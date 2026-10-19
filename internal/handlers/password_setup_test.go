package handlers

import (
	"net/http"
	"strings"
	"testing"

	"github.com/shridarpatil/whatomate/internal/config"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/test/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func newPasswordSetupTestApp(t *testing.T) *App {
	t.Helper()
	app := newProcessorTestApp(t)
	app.Config = &config.Config{
		JWT: config.JWTConfig{Secret: "test-secret-for-password-setup-tokens"},
		App: config.AppConfig{Environment: "production"},
	}
	return app
}

func tokenFromLink(t *testing.T, link string) string {
	t.Helper()
	idx := strings.Index(link, "token=")
	require.Greater(t, idx, -1, "link missing token query param: %s", link)
	return link[idx+len("token="):]
}

func TestPasswordSetupToken_RoundTrip(t *testing.T) {
	app := newPasswordSetupTestApp(t)
	org, _ := createProcessorTestOrg(t, app)

	hashed, err := bcrypt.GenerateFromPassword([]byte("original-throwaway-pw"), bcrypt.DefaultCost)
	require.NoError(t, err)
	user := models.User{OrganizationID: org.ID, Email: "newadmin@example.com", PasswordHash: string(hashed), FullName: "New Admin", IsActive: true}
	require.NoError(t, app.DB.Create(&user).Error)

	req := testutil.NewGETRequest(t)
	link, err := app.generatePasswordSetupToken(req, &user)
	require.NoError(t, err)
	assert.Contains(t, link, "/set-password?token=")

	userID, err := app.parsePasswordSetupToken(tokenFromLink(t, link))
	require.NoError(t, err)
	assert.Equal(t, user.ID, userID)
}

func TestSetPassword_Success(t *testing.T) {
	app := newPasswordSetupTestApp(t)
	org, _ := createProcessorTestOrg(t, app)

	hashed, err := bcrypt.GenerateFromPassword([]byte("original-throwaway-pw"), bcrypt.DefaultCost)
	require.NoError(t, err)
	user := models.User{OrganizationID: org.ID, Email: "newadmin2@example.com", PasswordHash: string(hashed), FullName: "New Admin", IsActive: true}
	require.NoError(t, app.DB.Create(&user).Error)

	req := testutil.NewGETRequest(t)
	link, err := app.generatePasswordSetupToken(req, &user)
	require.NoError(t, err)
	tokenStr := tokenFromLink(t, link)

	setReq := testutil.NewJSONRequest(t, map[string]any{"token": tokenStr, "password": "myNewPassword123"})
	require.NoError(t, app.SetPassword(setReq))
	assert.Equal(t, http.StatusOK, testutil.GetResponseStatusCode(setReq))

	var reloaded models.User
	require.NoError(t, app.DB.Where("id = ?", user.ID).First(&reloaded).Error)
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(reloaded.PasswordHash), []byte("myNewPassword123")))

	// The same token must not work a second time (fingerprint no longer matches).
	replayReq := testutil.NewJSONRequest(t, map[string]any{"token": tokenStr, "password": "anotherPassword456"})
	require.NoError(t, app.SetPassword(replayReq))
	assert.Equal(t, http.StatusUnauthorized, testutil.GetResponseStatusCode(replayReq))
}

func TestSetPassword_RejectsShortPassword(t *testing.T) {
	app := newPasswordSetupTestApp(t)
	org, _ := createProcessorTestOrg(t, app)

	hashed, _ := bcrypt.GenerateFromPassword([]byte("x"), bcrypt.DefaultCost)
	user := models.User{OrganizationID: org.ID, Email: "newadmin3@example.com", PasswordHash: string(hashed), FullName: "New Admin", IsActive: true}
	require.NoError(t, app.DB.Create(&user).Error)

	req := testutil.NewGETRequest(t)
	link, err := app.generatePasswordSetupToken(req, &user)
	require.NoError(t, err)

	setReq := testutil.NewJSONRequest(t, map[string]any{"token": tokenFromLink(t, link), "password": "short"})
	require.NoError(t, app.SetPassword(setReq))
	assert.Equal(t, http.StatusBadRequest, testutil.GetResponseStatusCode(setReq))
}

func TestSetPassword_RejectsExpiredOrGarbageToken(t *testing.T) {
	app := newPasswordSetupTestApp(t)

	setReq := testutil.NewJSONRequest(t, map[string]any{"token": "not-a-real-token", "password": "validPassword123"})
	require.NoError(t, app.SetPassword(setReq))
	assert.Equal(t, http.StatusUnauthorized, testutil.GetResponseStatusCode(setReq))
}
