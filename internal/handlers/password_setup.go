package handlers

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/valyala/fasthttp"
	"github.com/zerodha/fastglue"
	"golang.org/x/crypto/bcrypt"
)

const passwordSetupTokenPurpose = "password_setup"
const passwordSetupTokenTTL = 48 * time.Hour

// passwordSetupClaims is a stateless, self-contained one-time-setup token —
// no DB row needed. PasswordHashFingerprint pins the token to the user's
// password hash AT ISSUE TIME: once they set a real password (changing the
// hash), the same token stops validating, which gives single-use semantics
// for free without a token table/migration.
type passwordSetupClaims struct {
	UserID                  uuid.UUID `json:"user_id"`
	Purpose                 string    `json:"purpose"`
	PasswordHashFingerprint string    `json:"phf"`
	jwt.RegisteredClaims
}

func passwordHashFingerprint(hash string) string {
	if len(hash) > 20 {
		return hash[:20]
	}
	return hash
}

// generatePasswordSetupToken issues a 48-hour setup token for user and
// returns the full link an admin can share, pointing at the frontend's
// /set-password page.
func (a *App) generatePasswordSetupToken(r *fastglue.Request, user *models.User) (string, error) {
	claims := passwordSetupClaims{
		UserID:                  user.ID,
		Purpose:                 passwordSetupTokenPurpose,
		PasswordHashFingerprint: passwordHashFingerprint(user.PasswordHash),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(passwordSetupTokenTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "whatomate",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(a.Config.JWT.Secret))
	if err != nil {
		return "", err
	}

	scheme := "https"
	if !r.RequestCtx.IsTLS() && a.Config.App.Environment == "development" {
		scheme = "http"
	}
	host := string(r.RequestCtx.Host())
	basePath := sanitizeRedirectPath(a.Config.Server.BasePath)
	return fmt.Sprintf("%s://%s%s/set-password?token=%s", scheme, host, basePath, signed), nil
}

func (a *App) parsePasswordSetupToken(tokenStr string) (uuid.UUID, error) {
	var claims passwordSetupClaims
	token, err := jwt.ParseWithClaims(tokenStr, &claims, func(t *jwt.Token) (any, error) {
		return []byte(a.Config.JWT.Secret), nil
	})
	if err != nil || !token.Valid {
		return uuid.Nil, errors.New("invalid or expired setup link")
	}
	if claims.Purpose != passwordSetupTokenPurpose {
		return uuid.Nil, errors.New("invalid setup link")
	}
	return claims.UserID, nil
}

// SetPassword is the public endpoint a user's /set-password page submits to
// — either completing a super-admin-provisioned org's first login, or a
// password reset. Public because the user has no session yet; the setup
// token itself is the credential.
func (a *App) SetPassword(r *fastglue.Request) error {
	var req struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := a.decodeRequest(r, &req); err != nil {
		return nil
	}
	if req.Token == "" || len(req.Password) < 8 {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "A valid token and a password of at least 8 characters are required", nil, "")
	}

	userID, err := a.parsePasswordSetupToken(req.Token)
	if err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusUnauthorized, err.Error(), nil, "")
	}

	var user models.User
	if err := a.DB.Where("id = ? AND deleted_at IS NULL", userID).First(&user).Error; err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusNotFound, "User not found", nil, "")
	}

	// Re-check the fingerprint against the CURRENT hash (decoded again here,
	// not trusted from the token alone) so a token can't be replayed after
	// the password it was issued for has already been set.
	var claims passwordSetupClaims
	if _, err := jwt.ParseWithClaims(req.Token, &claims, func(t *jwt.Token) (any, error) {
		return []byte(a.Config.JWT.Secret), nil
	}); err != nil || claims.PasswordHashFingerprint != passwordHashFingerprint(user.PasswordHash) {
		return r.SendErrorEnvelope(fasthttp.StatusUnauthorized, "This setup link has already been used. Ask an admin to send a new one.", nil, "")
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		a.Log.Error("Failed to hash password", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to set password", nil, "")
	}
	if err := a.DB.Model(&models.User{}).Where("id = ?", user.ID).Update("password_hash", string(hashedPassword)).Error; err != nil {
		a.Log.Error("Failed to set password", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to set password", nil, "")
	}

	a.logAudit(user.OrganizationID, user.ID, "user", user.ID, models.AuditActionUpdated,
		map[string]any{"action": "password_set_via_link"},
		map[string]any{"action": "password_set_via_link", "target_email": user.Email})

	return r.SendEnvelope(map[string]string{"message": "Password set successfully"})
}
