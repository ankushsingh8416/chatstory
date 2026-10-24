package handlers_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/test/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
)

// TestApp_GetHealthOverview_FlagsIssues exercises the dashboard health
// check against two accounts: one whose token Meta rejects, and one that's
// healthy but has a chatbot-disabled contact stuck in the queue and a
// recent failed send — the three signals the dashboard surfaces.
func TestApp_GetHealthOverview_FlagsIssues(t *testing.T) {
	meta := newFakeMetaServer(t)
	app := newAppWithMeta(t, meta)
	org := testutil.CreateTestOrganization(t, app.DB)

	badAcc := createTestAccountForValidation(t, app.DB, org.ID, "phone-bad", "biz-bad")
	goodAcc := createTestAccountForValidation(t, app.DB, org.ID, "phone-good", "biz-good")

	meta.phoneFn = func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "phone-bad") {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"Invalid OAuth access token","code":190}}`))
			return
		}
		_, _ = w.Write([]byte(`{"display_phone_number":"+1","verified_name":"Good","code_verification_status":"VERIFIED","account_mode":"LIVE"}`))
	}

	require.NoError(t, app.DB.Create(&models.ChatbotSettings{
		BaseModel:       models.BaseModel{ID: uuid.New()},
		OrganizationID:  org.ID,
		WhatsAppAccount: goodAcc.Name,
		IsEnabled:       true,
	}).Error)
	require.NoError(t, app.DB.Create(&models.ChatbotSettings{
		BaseModel:       models.BaseModel{ID: uuid.New()},
		OrganizationID:  org.ID,
		WhatsAppAccount: badAcc.Name,
		IsEnabled:       false,
	}).Error)

	contact := &models.Contact{
		BaseModel:      models.BaseModel{ID: uuid.New()},
		OrganizationID: org.ID,
		PhoneNumber:    "15550000000",
	}
	require.NoError(t, app.DB.Create(contact).Error)

	require.NoError(t, app.DB.Create(&models.AgentTransfer{
		BaseModel:       models.BaseModel{ID: uuid.New()},
		OrganizationID:  org.ID,
		ContactID:       contact.ID,
		WhatsAppAccount: goodAcc.Name,
		PhoneNumber:     contact.PhoneNumber,
		Status:          models.TransferStatusActive,
		Source:          models.TransferSourceChatbotDisabled,
	}).Error)

	require.NoError(t, app.DB.Create(&models.Message{
		BaseModel:       models.BaseModel{ID: uuid.New()},
		OrganizationID:  org.ID,
		WhatsAppAccount: goodAcc.Name,
		ContactID:       contact.ID,
		Direction:       models.DirectionOutgoing,
		MessageType:     models.MessageTypeText,
		Status:          models.MessageStatusFailed,
	}).Error)

	req := testutil.NewGETRequest(t)
	testutil.SetAuthContext(req, org.ID, uuid.New())

	require.NoError(t, app.GetHealthOverview(req))
	require.Equal(t, fasthttp.StatusOK, testutil.GetResponseStatusCode(req))

	var resp struct {
		Data struct {
			Accounts []struct {
				AccountName       string `json:"account_name"`
				TokenValid        bool   `json:"token_valid"`
				ChatbotEnabled    bool   `json:"chatbot_enabled"`
				FailedMessages24h int64  `json:"failed_messages_24h"`
				StuckContacts     int64  `json:"stuck_contacts"`
			} `json:"accounts"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(testutil.GetResponseBody(req), &resp))
	require.Len(t, resp.Data.Accounts, 2)

	byName := map[string]bool{}
	for _, acc := range resp.Data.Accounts {
		byName[acc.AccountName] = true
		switch acc.AccountName {
		case badAcc.Name:
			assert.False(t, acc.TokenValid, "bad account's token must be flagged invalid")
			assert.False(t, acc.ChatbotEnabled)
		case goodAcc.Name:
			assert.True(t, acc.TokenValid, "good account's token must validate")
			assert.True(t, acc.ChatbotEnabled)
			assert.Equal(t, int64(1), acc.FailedMessages24h)
			assert.Equal(t, int64(1), acc.StuckContacts)
		}
	}
	assert.True(t, byName[badAcc.Name])
	assert.True(t, byName[goodAcc.Name])
}
