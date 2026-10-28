package salesforce

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newFakeServer stands up a fake Salesforce: the token endpoint always
// succeeds, and the Lead endpoint returns 401 exactly until unauthorizedFor
// calls have been made (0 = always succeed), to exercise the retry path.
func newFakeServer(t *testing.T, unauthorizedFor int32) (*httptest.Server, *int32, *int32) {
	t.Helper()
	var tokenCalls, leadCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/services/oauth2/token":
			n := atomic.AddInt32(&tokenCalls, 1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"token-` + strconv.Itoa(int(n)) + `"}`))
		case "/lead":
			calls := atomic.AddInt32(&leadCalls, 1)
			if calls <= unauthorizedFor {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"expired"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success":true,"action":"CREATED","leadId":"00Q123","messagesSaved":1,"message":"ok","warnings":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &tokenCalls, &leadCalls
}

func TestClient_CreateOrUpdateLead_Success(t *testing.T) {
	srv, tokenCalls, leadCalls := newFakeServer(t, 0)
	c := New(Config{ClientID: "id", ClientSecret: "secret", AuthURL: srv.URL + "/services/oauth2/token", LeadAPIURL: srv.URL + "/lead"})

	resp, err := c.CreateOrUpdateLead(context.Background(), FirstContactPayload{Name: "Neha", Mobile: "9000000000"})
	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Equal(t, "00Q123", resp.LeadID)
	assert.Equal(t, int32(1), atomic.LoadInt32(tokenCalls), "token fetched once")
	assert.Equal(t, int32(1), atomic.LoadInt32(leadCalls))

	// Second call reuses the cached token — no second token fetch.
	_, err = c.AppendMessages(context.Background(), FollowUpPayload{Mobile: "9000000000", ConversationID: "c1"})
	require.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(tokenCalls), "cached token reused")
	assert.Equal(t, int32(2), atomic.LoadInt32(leadCalls))
}

func TestClient_RetriesOnceOn401(t *testing.T) {
	srv, tokenCalls, leadCalls := newFakeServer(t, 1) // first Lead call 401s, second succeeds
	c := New(Config{ClientID: "id", ClientSecret: "secret", AuthURL: srv.URL + "/services/oauth2/token", LeadAPIURL: srv.URL + "/lead"})

	resp, err := c.CreateOrUpdateLead(context.Background(), FirstContactPayload{Name: "Neha", Mobile: "9000000000"})
	require.NoError(t, err, "a 401 must trigger a token refresh + retry, not a hard failure")
	assert.True(t, resp.Success)
	assert.Equal(t, int32(2), atomic.LoadInt32(tokenCalls), "expired token forces a second fetch")
	assert.Equal(t, int32(2), atomic.LoadInt32(leadCalls), "one retry after the 401")
}
