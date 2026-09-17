package handlers

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/mail"
	"time"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/audit"
	"github.com/shridarpatil/whatomate/internal/crypto"
	"github.com/shridarpatil/whatomate/internal/database"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/internal/utils"
	"github.com/valyala/fasthttp"
	"github.com/zerodha/fastglue"
	"golang.org/x/crypto/bcrypt"
)

// generalSettingsSnapshot extracts the fields shown on the General tab into a
// map suitable for audit diffing. Reading from a nil JSONB map returns the
// zero value (nil), which is treated as "unset" by the audit comparator.
func generalSettingsSnapshot(name string, settings models.JSONB) map[string]any {
	hasSecret := false
	if settings != nil {
		if v, ok := settings["meta_app_secret_encrypted"].(string); ok && v != "" {
			hasSecret = true
		}
	}
	return map[string]any{
		"name":                name,
		"timezone":            settings["timezone"],
		"date_format":         settings["date_format"],
		"mask_phone_numbers":  settings["mask_phone_numbers"],
		"meta_app_id":         settings["meta_app_id"],
		"meta_config_id":      settings["meta_config_id"],
		"has_meta_app_secret": hasSecret,
		"has_logo":            settings["logo_data_url"] != nil && settings["logo_data_url"] != "",
	}
}

// callingSettingsSnapshot extracts the fields shown on the Calling tab into a
// map suitable for audit diffing.
func callingSettingsSnapshot(settings models.JSONB) map[string]any {
	return map[string]any{
		"calling_enabled":       settings["calling_enabled"],
		"max_call_duration":     settings["max_call_duration"],
		"transfer_timeout_secs": settings["transfer_timeout_secs"],
		"hold_music_file":       settings["hold_music_file"],
		"ringback_file":         settings["ringback_file"],
	}
}

// OrganizationSettings represents the settings structure
type OrganizationSettings struct {
	MaskPhoneNumbers    bool   `json:"mask_phone_numbers"`
	Timezone            string `json:"timezone"`
	DateFormat          string `json:"date_format"`
	CallingEnabled      bool   `json:"calling_enabled"`
	MaxCallDuration     int    `json:"max_call_duration"`
	TransferTimeoutSecs int    `json:"transfer_timeout_secs"`
	HoldMusicFile       string `json:"hold_music_file"`
	RingbackFile        string `json:"ringback_file"`
	MetaAppID           string `json:"meta_app_id"`
	MetaConfigID        string `json:"meta_config_id"`
	HasMetaAppSecret    bool   `json:"has_meta_app_secret"`
	// HasOpenAIEmbeddingsKey: knowledge-base document embeddings always use
	// OpenAI (text-embedding-3-small), independent of whichever provider the
	// chatbot's chat replies use — Anthropic has no first-party embeddings
	// API. Kept separate from ChatbotSettings.AI.APIKey so an Anthropic-only
	// org doesn't need a throwaway OpenAI ChatbotSettings row just for this.
	HasOpenAIEmbeddingsKey bool `json:"has_openai_embeddings_key"`
	// LogoDataURL is the org's logo as a data: URI (small image, capped at
	// upload time — see UploadOrgLogo), or empty if none is set. Not secret,
	// so it's returned directly rather than via a has_* flag.
	LogoDataURL string `json:"logo_data_url,omitempty"`
}

// GetOrganizationSettings returns the organization settings
func (a *App) GetOrganizationSettings(r *fastglue.Request) error {
	orgID, err := a.getOrgID(r)
	if err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusUnauthorized, "Unauthorized", nil, "")
	}

	var org models.Organization
	if err := a.DB.Where("id = ?", orgID).First(&org).Error; err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusNotFound, "Organization not found", nil, "")
	}

	// Parse settings from JSONB
	settings := OrganizationSettings{
		MaskPhoneNumbers:    false,
		Timezone:            "UTC",
		DateFormat:          "YYYY-MM-DD",
		CallingEnabled:      false,
		MaxCallDuration:     callingConfigDefault(a.Config.Calling.MaxCallDuration, 3600),
		TransferTimeoutSecs: callingConfigDefault(a.Config.Calling.TransferTimeoutSecs, 60),
		HoldMusicFile:       a.Config.Calling.HoldMusicFile,
		RingbackFile:        a.Config.Calling.RingbackFile,
	}

	if org.Settings != nil {
		if v, ok := org.Settings["mask_phone_numbers"].(bool); ok {
			settings.MaskPhoneNumbers = v
		}
		if v, ok := org.Settings["timezone"].(string); ok && v != "" {
			settings.Timezone = v
		}
		if v, ok := org.Settings["date_format"].(string); ok && v != "" {
			settings.DateFormat = v
		}
		if v, ok := org.Settings["calling_enabled"].(bool); ok {
			settings.CallingEnabled = v
		}
		if v, ok := org.Settings["max_call_duration"].(float64); ok && v > 0 {
			settings.MaxCallDuration = int(v)
		}
		if v, ok := org.Settings["transfer_timeout_secs"].(float64); ok && v > 0 {
			settings.TransferTimeoutSecs = int(v)
		}
		if v, ok := org.Settings["hold_music_file"].(string); ok && v != "" {
			settings.HoldMusicFile = v
		}
		if v, ok := org.Settings["ringback_file"].(string); ok && v != "" {
			settings.RingbackFile = v
		}
		if v, ok := org.Settings["meta_app_id"].(string); ok && v != "" {
			settings.MetaAppID = v
		}
		if v, ok := org.Settings["meta_config_id"].(string); ok && v != "" {
			settings.MetaConfigID = v
		}
		if v, ok := org.Settings["meta_app_secret_encrypted"].(string); ok && v != "" {
			settings.HasMetaAppSecret = true
		}
		if v, ok := org.Settings["openai_embeddings_key_encrypted"].(string); ok && v != "" {
			settings.HasOpenAIEmbeddingsKey = true
		}
		if v, ok := org.Settings["logo_data_url"].(string); ok && v != "" {
			settings.LogoDataURL = v
		}
	}

	return r.SendEnvelope(map[string]any{
		"settings": settings,
		"name":     org.Name,
	})
}

// UpdateOrganizationSettings updates the organization settings
func (a *App) UpdateOrganizationSettings(r *fastglue.Request) error {
	orgID, userID, err := a.getOrgAndUserID(r)
	if err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusUnauthorized, "Unauthorized", nil, "")
	}

	var req struct {
		MaskPhoneNumbers    *bool   `json:"mask_phone_numbers"`
		Timezone            *string `json:"timezone"`
		DateFormat          *string `json:"date_format"`
		Name                *string `json:"name"`
		CallingEnabled      *bool   `json:"calling_enabled"`
		MaxCallDuration     *int    `json:"max_call_duration"`
		TransferTimeoutSecs *int    `json:"transfer_timeout_secs"`
		HoldMusicFile       *string `json:"hold_music_file"`
		RingbackFile        *string `json:"ringback_file"`
		MetaAppID           *string `json:"meta_app_id"`
		MetaConfigID        *string `json:"meta_config_id"`
		MetaAppSecret       *string `json:"meta_app_secret"`
		OpenAIEmbeddingsKey *string `json:"openai_embeddings_key"`
	}

	if err := json.Unmarshal(r.RequestCtx.PostBody(), &req); err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "Invalid request body", nil, "")
	}

	var org models.Organization
	if err := a.DB.Where("id = ?", orgID).First(&org).Error; err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusNotFound, "Organization not found", nil, "")
	}

	// Gating Meta App credentials update on accounts:write permission
	metaAppCredsTouched := req.MetaAppID != nil || req.MetaConfigID != nil || req.MetaAppSecret != nil
	if metaAppCredsTouched {
		if err := a.requirePermission(r, userID, models.ResourceAccounts, models.ActionWrite); err != nil {
			return nil
		}
	}
	// Gating the embeddings key on knowledge_base:write — it's KB-specific, not a general secret.
	if req.OpenAIEmbeddingsKey != nil {
		if err := a.requirePermission(r, userID, models.ResourceKnowledgeBase, models.ActionWrite); err != nil {
			return nil
		}
	}

	// Snapshot before mutation so we can compute per-tab diffs.
	oldGeneral := generalSettingsSnapshot(org.Name, org.Settings)
	oldCalling := callingSettingsSnapshot(org.Settings)

	// Track which tabs received updates so we only audit the relevant ones.
	generalTouched := req.MaskPhoneNumbers != nil || req.Timezone != nil || req.DateFormat != nil || (req.Name != nil && *req.Name != "") || metaAppCredsTouched
	callingTouched := req.CallingEnabled != nil || req.MaxCallDuration != nil || req.TransferTimeoutSecs != nil || req.HoldMusicFile != nil || req.RingbackFile != nil

	// Update settings
	if org.Settings == nil {
		org.Settings = models.JSONB{}
	}

	if req.MaskPhoneNumbers != nil {
		org.Settings["mask_phone_numbers"] = *req.MaskPhoneNumbers
	}
	if req.Timezone != nil {
		org.Settings["timezone"] = *req.Timezone
	}
	if req.DateFormat != nil {
		org.Settings["date_format"] = *req.DateFormat
	}
	if req.CallingEnabled != nil {
		org.Settings["calling_enabled"] = *req.CallingEnabled
	}
	if req.MaxCallDuration != nil && *req.MaxCallDuration > 0 {
		org.Settings["max_call_duration"] = *req.MaxCallDuration
	}
	if req.TransferTimeoutSecs != nil && *req.TransferTimeoutSecs > 0 {
		org.Settings["transfer_timeout_secs"] = *req.TransferTimeoutSecs
	}
	if req.HoldMusicFile != nil {
		org.Settings["hold_music_file"] = *req.HoldMusicFile
	}
	if req.RingbackFile != nil {
		org.Settings["ringback_file"] = *req.RingbackFile
	}
	if req.MetaAppID != nil {
		org.Settings["meta_app_id"] = *req.MetaAppID
	}
	if req.MetaConfigID != nil {
		org.Settings["meta_config_id"] = *req.MetaConfigID
	}
	if req.MetaAppSecret != nil && *req.MetaAppSecret != "" {
		encSecret, errEnc := crypto.Encrypt(*req.MetaAppSecret, a.Config.App.EncryptionKey)
		if errEnc != nil {
			a.Log.Error("Failed to encrypt meta app secret", "error", errEnc)
			return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to update settings", nil, "")
		}
		org.Settings["meta_app_secret_encrypted"] = encSecret
	}
	if req.OpenAIEmbeddingsKey != nil && *req.OpenAIEmbeddingsKey != "" {
		encKey, errEnc := crypto.Encrypt(*req.OpenAIEmbeddingsKey, a.Config.App.EncryptionKey)
		if errEnc != nil {
			a.Log.Error("Failed to encrypt OpenAI embeddings key", "error", errEnc)
			return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to update settings", nil, "")
		}
		org.Settings["openai_embeddings_key_encrypted"] = encKey
	}
	if req.Name != nil && *req.Name != "" {
		org.Name = *req.Name
	}

	if err := a.DB.Save(&org).Error; err != nil {
		a.Log.Error("Failed to update settings", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to update settings", nil, "")
	}

	if a.CallManager != nil {
		a.CallManager.InvalidateOrgCallingSettingsCache(orgID)
	}

	// Emit per-tab audit entries. LogAudit is a no-op when there are zero changes.
	userName := audit.GetUserName(a.DB, userID)
	if generalTouched {
		newGeneral := generalSettingsSnapshot(org.Name, org.Settings)
		audit.LogAudit(a.DB, orgID, userID, userName,
			models.ResourceSettingsGeneral, orgID, models.AuditActionUpdated, oldGeneral, newGeneral)
	}
	if callingTouched {
		newCalling := callingSettingsSnapshot(org.Settings)
		audit.LogAudit(a.DB, orgID, userID, userName,
			models.ResourceSettingsCalling, orgID, models.AuditActionUpdated, oldCalling, newCalling)
	}

	return r.SendEnvelope(map[string]any{
		"message": "Settings updated successfully",
	})
}

// IsCallingEnabledForOrg checks if calling is enabled for an organization.
// Both the global CallManager and the per-org setting must be active.
func (a *App) IsCallingEnabledForOrg(orgID any) bool {
	if a.CallManager == nil {
		return false
	}
	var org models.Organization
	if err := a.DB.Where("id = ?", orgID).First(&org).Error; err != nil {
		return false
	}
	if org.Settings != nil {
		if v, ok := org.Settings["calling_enabled"].(bool); ok {
			return v
		}
	}
	return false
}

// requireCallingEnabled checks if calling is enabled for the org and returns an error
// envelope if not. Returns nil when calling is enabled and the handler can proceed.
func (a *App) requireCallingEnabled(r *fastglue.Request, orgID uuid.UUID) error {
	if !a.IsCallingEnabledForOrg(orgID) {
		return r.SendErrorEnvelope(fasthttp.StatusServiceUnavailable, "Calling is not enabled for this organization", nil, "")
	}
	return nil
}

// GetOrgCallingConfig returns org-level calling config values, falling back to global defaults.
func (a *App) GetOrgCallingConfig(orgID any) (maxDuration, transferTimeout int) {
	maxDuration = callingConfigDefault(a.Config.Calling.MaxCallDuration, 3600)
	transferTimeout = callingConfigDefault(a.Config.Calling.TransferTimeoutSecs, 60)

	var org models.Organization
	if err := a.DB.Where("id = ?", orgID).First(&org).Error; err != nil {
		return
	}
	if org.Settings != nil {
		if v, ok := org.Settings["max_call_duration"].(float64); ok && v > 0 {
			maxDuration = int(v)
		}
		if v, ok := org.Settings["transfer_timeout_secs"].(float64); ok && v > 0 {
			transferTimeout = int(v)
		}
	}
	return
}

// callingConfigDefault returns val if positive, otherwise fallback.
func callingConfigDefault(val, fallback int) int {
	if val > 0 {
		return val
	}
	return fallback
}

// MaskContactFields conditionally masks a profile name and phone number
// if phone masking is enabled for the given organization.
func (a *App) MaskContactFields(orgID any, profileName, phoneNumber string) (string, string) {
	if a.ShouldMaskPhoneNumbers(orgID) {
		return utils.MaskIfPhoneNumber(profileName), utils.MaskPhoneNumber(phoneNumber)
	}
	return profileName, phoneNumber
}

// ShouldMaskPhoneNumbers checks if phone masking is enabled for the organization
func (a *App) ShouldMaskPhoneNumbers(orgID any) bool {
	var org models.Organization
	if err := a.DB.Where("id = ?", orgID).First(&org).Error; err != nil {
		return false
	}

	if org.Settings != nil {
		if v, ok := org.Settings["mask_phone_numbers"].(bool); ok {
			return v
		}
	}
	return false
}

// OrganizationResponse represents an organization in API responses
type OrganizationResponse struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug,omitempty"`
	Suspended   bool      `json:"suspended"`
	MemberCount int64     `json:"member_count,omitempty"`
	LogoDataURL string    `json:"logo_data_url,omitempty"`
	CreatedAt   string    `json:"created_at"`
}

// organizationToResponse converts an Organization to its API response shape,
// reading the suspended flag and logo out of the Settings JSONB bag.
func organizationToResponse(org models.Organization) OrganizationResponse {
	suspended, _ := org.Settings["suspended"].(bool)
	logo, _ := org.Settings["logo_data_url"].(string)
	return OrganizationResponse{
		ID:          org.ID,
		Name:        org.Name,
		Slug:        org.Slug,
		Suspended:   suspended,
		LogoDataURL: logo,
		CreatedAt:   org.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}
}

// ListOrganizations returns all organizations (super admin or users with organizations:read)
func (a *App) ListOrganizations(r *fastglue.Request) error {
	userID, ok := r.RequestCtx.UserValue("user_id").(uuid.UUID)
	if !ok {
		return r.SendErrorEnvelope(fasthttp.StatusUnauthorized, "Unauthorized", nil, "")
	}

	// Super admins or users with organizations:read permission
	if !a.IsSuperAdmin(userID) && !a.HasPermission(userID, models.ResourceOrganizations, models.ActionRead) {
		return r.SendErrorEnvelope(fasthttp.StatusForbidden, "Insufficient permissions", nil, "")
	}

	var orgs []models.Organization
	if err := a.DB.Order("name ASC").Find(&orgs).Error; err != nil {
		a.Log.Error("Failed to list organizations", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to list organizations", nil, "")
	}

	// Member counts, fetched in one grouped query rather than N+1.
	var counts []struct {
		OrganizationID uuid.UUID
		Count          int64
	}
	a.DB.Table("user_organizations").
		Select("organization_id, COUNT(*) as count").
		Where("deleted_at IS NULL").
		Group("organization_id").
		Scan(&counts)
	countMap := make(map[uuid.UUID]int64, len(counts))
	for _, c := range counts {
		countMap[c.OrganizationID] = c.Count
	}

	response := make([]OrganizationResponse, len(orgs))
	for i, org := range orgs {
		resp := organizationToResponse(org)
		resp.MemberCount = countMap[org.ID]
		response[i] = resp
	}

	return r.SendEnvelope(map[string]any{
		"organizations": response,
	})
}

// GetCurrentOrganization returns the current user's organization details
func (a *App) GetCurrentOrganization(r *fastglue.Request) error {
	orgID, err := a.getOrgID(r)
	if err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusUnauthorized, "Unauthorized", nil, "")
	}

	var org models.Organization
	if err := a.DB.Where("id = ?", orgID).First(&org).Error; err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusNotFound, "Organization not found", nil, "")
	}

	return r.SendEnvelope(organizationToResponse(org))
}

// CreateOrganizationRequest represents the request body for creating an organization
type CreateOrganizationRequest struct {
	Name string `json:"name"`
}

// CreateOrganization creates a new organization
func (a *App) CreateOrganization(r *fastglue.Request) error {
	_, userID, err := a.requireAuth(r, models.ResourceOrganizations, models.ActionWrite)
	if err != nil {
		return nil
	}

	var req CreateOrganizationRequest
	if err := a.decodeRequest(r, &req); err != nil {
		return nil
	}

	if req.Name == "" {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "Organization name is required", nil, "")
	}

	// Start transaction
	tx := a.DB.Begin()
	if tx.Error != nil {
		a.Log.Error("Failed to begin transaction", "error", tx.Error)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to create organization", nil, "")
	}

	org := models.Organization{
		Name:     req.Name,
		Slug:     generateSlug(req.Name),
		Settings: models.JSONB{},
	}

	if err := tx.Create(&org).Error; err != nil {
		tx.Rollback()
		a.Log.Error("Failed to create organization", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to create organization", nil, "")
	}

	// Seed system roles for the new organization
	if err := database.SeedSystemRolesForOrg(tx, org.ID); err != nil {
		tx.Rollback()
		a.Log.Error("Failed to seed system roles", "error", err, "org_id", org.ID)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to create organization", nil, "")
	}

	// Create default chatbot settings
	chatbotSettings := models.ChatbotSettings{
		OrganizationID:     org.ID,
		IsEnabled:          false,
		SessionTimeoutMins: 30,
	}
	if err := tx.Create(&chatbotSettings).Error; err != nil {
		tx.Rollback()
		a.Log.Error("Failed to create chatbot settings", "error", err, "org_id", org.ID)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to create organization", nil, "")
	}

	// Get admin role for this org and add the creator as admin
	var adminRole models.CustomRole
	if err := tx.Where("organization_id = ? AND name = ? AND is_system = ?", org.ID, "admin", true).First(&adminRole).Error; err != nil {
		tx.Rollback()
		a.Log.Error("Failed to find admin role", "error", err, "org_id", org.ID)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to create organization", nil, "")
	}

	userOrg := models.UserOrganization{
		UserID:         userID,
		OrganizationID: org.ID,
		RoleID:         &adminRole.ID,
		IsDefault:      false,
	}
	if err := tx.Create(&userOrg).Error; err != nil {
		tx.Rollback()
		a.Log.Error("Failed to add creator to organization", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to create organization", nil, "")
	}

	// Seed default dashboard widgets for the new organization
	if err := database.SeedDefaultWidgetsForOrg(tx, org.ID, userID); err != nil {
		tx.Rollback()
		a.Log.Error("Failed to seed default widgets", "error", err, "org_id", org.ID)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to create organization", nil, "")
	}

	if err := tx.Commit().Error; err != nil {
		a.Log.Error("Failed to commit transaction", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to create organization", nil, "")
	}

	a.Log.Info("Created organization", "org_id", org.ID, "org_name", org.Name, "created_by", userID)

	return r.SendEnvelope(organizationToResponse(org))
}

// MemberResponse represents an organization member in API responses
type MemberResponse struct {
	ID             uuid.UUID  `json:"id"`
	UserID         uuid.UUID  `json:"user_id"`
	OrganizationID uuid.UUID  `json:"organization_id"`
	RoleID         *uuid.UUID `json:"role_id,omitempty"`
	RoleName       string     `json:"role_name,omitempty"`
	IsDefault      bool       `json:"is_default"`
	Email          string     `json:"email"`
	FullName       string     `json:"full_name"`
	IsActive       bool       `json:"is_active"`
	CreatedAt      time.Time  `json:"created_at"`
}

// ListOrganizationMembers returns all members of the current organization
func (a *App) ListOrganizationMembers(r *fastglue.Request) error {
	orgID, _, err := a.requireAuth(r, models.ResourceOrganizations, models.ActionRead)
	if err != nil {
		return nil
	}

	pg := parsePagination(r)
	search := string(r.RequestCtx.QueryArgs().Peek("search"))

	baseQuery := a.DB.Table("user_organizations").
		Joins("LEFT JOIN users ON users.id = user_organizations.user_id AND users.deleted_at IS NULL").
		Joins("LEFT JOIN custom_roles ON custom_roles.id = user_organizations.role_id AND custom_roles.deleted_at IS NULL").
		Where("user_organizations.organization_id = ? AND user_organizations.deleted_at IS NULL", orgID)

	if search != "" {
		baseQuery = baseQuery.Where("users.full_name ILIKE ? OR users.email ILIKE ?", "%"+search+"%", "%"+search+"%")
	}

	var total int64
	baseQuery.Count(&total)

	var response []MemberResponse
	if err := pg.Apply(baseQuery.
		Select(`user_organizations.id, user_organizations.user_id, user_organizations.organization_id,
			user_organizations.role_id, user_organizations.is_default, user_organizations.created_at,
			users.email, users.full_name, users.is_active,
			custom_roles.name AS role_name`).
		Order("user_organizations.created_at DESC")).
		Scan(&response).Error; err != nil {
		a.Log.Error("Failed to list organization members", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to list members", nil, "")
	}

	return r.SendEnvelope(listEnvelope("members", response, total, pg))
}

// AddMemberRequest represents the request body for adding a member to an organization
type AddMemberRequest struct {
	UserID uuid.UUID  `json:"user_id"`
	Email  string     `json:"email"`
	RoleID *uuid.UUID `json:"role_id"`
}

// AddOrganizationMember adds an existing user to the current organization
func (a *App) AddOrganizationMember(r *fastglue.Request) error {
	orgID, _, err := a.requireAuth(r, models.ResourceOrganizations, models.ActionAssign)
	if err != nil {
		return nil
	}

	var req AddMemberRequest
	if err := a.decodeRequest(r, &req); err != nil {
		return nil
	}

	// Resolve target user by user_id or email
	var targetUser models.User
	if req.UserID != uuid.Nil {
		if err := a.DB.Where("id = ?", req.UserID).First(&targetUser).Error; err != nil {
			return r.SendErrorEnvelope(fasthttp.StatusNotFound, "User not found", nil, "")
		}
	} else if req.Email != "" {
		if err := a.DB.Where("email = ?", req.Email).First(&targetUser).Error; err != nil {
			return r.SendErrorEnvelope(fasthttp.StatusNotFound, "No user found with this email", nil, "")
		}
	} else {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "user_id or email is required", nil, "")
	}

	// Check if already a member
	var existingCount int64
	a.DB.Model(&models.UserOrganization{}).
		Where("user_id = ? AND organization_id = ?", targetUser.ID, orgID).
		Count(&existingCount)
	if existingCount > 0 {
		return r.SendErrorEnvelope(fasthttp.StatusConflict, "User is already a member of this organization", nil, "")
	}

	// Determine role
	var roleID *uuid.UUID
	if req.RoleID != nil {
		// Validate role exists and belongs to org
		var role models.CustomRole
		if err := a.DB.Where("id = ? AND organization_id = ?", req.RoleID, orgID).First(&role).Error; err != nil {
			return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "Invalid role", nil, "")
		}
		roleID = req.RoleID
	} else {
		// Use org's default role
		var defaultRole models.CustomRole
		if err := a.DB.Where("organization_id = ? AND is_default = ?", orgID, true).First(&defaultRole).Error; err == nil {
			roleID = &defaultRole.ID
		}
	}

	userOrg := models.UserOrganization{
		UserID:         targetUser.ID,
		OrganizationID: orgID,
		RoleID:         roleID,
		IsDefault:      false,
	}

	if err := a.DB.Create(&userOrg).Error; err != nil {
		a.Log.Error("Failed to add organization member", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to add member", nil, "")
	}

	return r.SendEnvelope(map[string]string{"message": "Member added successfully"})
}

// RemoveOrganizationMember removes a user from the current organization
func (a *App) RemoveOrganizationMember(r *fastglue.Request) error {
	orgID, userID, err := a.requireAuth(r, models.ResourceOrganizations, models.ActionAssign)
	if err != nil {
		return nil
	}

	targetUserID, err := parsePathUUID(r, "member_id", "member")
	if err != nil {
		return nil
	}

	// Cannot remove self
	if targetUserID == userID {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "Cannot remove yourself from the organization", nil, "")
	}

	result := a.DB.Where("user_id = ? AND organization_id = ?", targetUserID, orgID).
		Delete(&models.UserOrganization{})
	if result.Error != nil {
		a.Log.Error("Failed to remove organization member", "error", result.Error)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to remove member", nil, "")
	}
	if result.RowsAffected == 0 {
		return r.SendErrorEnvelope(fasthttp.StatusNotFound, "Member not found in this organization", nil, "")
	}

	// Invalidate removed user's permission cache
	a.InvalidateUserPermissionsCache(targetUserID)

	return r.SendEnvelope(map[string]string{"message": "Member removed successfully"})
}

// UpdateMemberRoleRequest represents the request body for updating a member's role
type UpdateMemberRoleRequest struct {
	RoleID uuid.UUID `json:"role_id"`
}

// UpdateOrganizationMemberRole updates a member's role in the current organization
func (a *App) UpdateOrganizationMemberRole(r *fastglue.Request) error {
	orgID, _, err := a.requireAuth(r, models.ResourceOrganizations, models.ActionAssign)
	if err != nil {
		return nil
	}

	targetUserID, err := parsePathUUID(r, "member_id", "member")
	if err != nil {
		return nil
	}

	var req UpdateMemberRoleRequest
	if err := a.decodeRequest(r, &req); err != nil {
		return nil
	}

	if req.RoleID == uuid.Nil {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "role_id is required", nil, "")
	}

	// Validate role exists and belongs to org
	var role models.CustomRole
	if err := a.DB.Where("id = ? AND organization_id = ?", req.RoleID, orgID).First(&role).Error; err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "Invalid role", nil, "")
	}

	// Update the user's role in this org
	result := a.DB.Model(&models.UserOrganization{}).
		Where("user_id = ? AND organization_id = ?", targetUserID, orgID).
		Update("role_id", req.RoleID)
	if result.Error != nil {
		a.Log.Error("Failed to update member role", "error", result.Error)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to update member role", nil, "")
	}
	if result.RowsAffected == 0 {
		return r.SendErrorEnvelope(fasthttp.StatusNotFound, "Member not found in this organization", nil, "")
	}

	// Invalidate permission cache
	a.InvalidateUserPermissionsCache(targetUserID)

	return r.SendEnvelope(map[string]string{"message": "Member role updated successfully"})
}

// UpdateOrganizationRequest represents the request body for a super admin
// editing any organization (as opposed to UpdateOrganizationSettings, which
// only edits the caller's own current org).
type UpdateOrganizationRequest struct {
	Name *string `json:"name"`
}

// UpdateOrganization lets a super admin rename any organization.
func (a *App) UpdateOrganization(r *fastglue.Request) error {
	superAdminID, err := a.requireSuperAdmin(r)
	if err != nil {
		return nil
	}

	orgID, err := parsePathUUID(r, "id", "organization")
	if err != nil {
		return nil
	}

	var org models.Organization
	if err := a.DB.Where("id = ?", orgID).First(&org).Error; err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusNotFound, "Organization not found", nil, "")
	}

	var req UpdateOrganizationRequest
	if err := a.decodeRequest(r, &req); err != nil {
		return nil
	}

	oldName := org.Name
	if req.Name != nil && *req.Name != "" {
		org.Name = *req.Name
	}

	if err := a.DB.Save(&org).Error; err != nil {
		a.Log.Error("Failed to update organization", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to update organization", nil, "")
	}

	a.logAudit(org.ID, superAdminID, models.ResourceOrganizations, org.ID, models.AuditActionUpdated,
		map[string]any{"name": oldName}, map[string]any{"name": org.Name})

	return r.SendEnvelope(organizationToResponse(org))
}

// setOrganizationSuspended is the shared implementation behind
// SuspendOrganization/ActivateOrganization.
func (a *App) setOrganizationSuspended(r *fastglue.Request, suspended bool) error {
	superAdminID, err := a.requireSuperAdmin(r)
	if err != nil {
		return nil
	}

	orgID, err := parsePathUUID(r, "id", "organization")
	if err != nil {
		return nil
	}

	var org models.Organization
	if err := a.DB.Where("id = ?", orgID).First(&org).Error; err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusNotFound, "Organization not found", nil, "")
	}

	if org.Settings == nil {
		org.Settings = models.JSONB{}
	}
	wasSuspended, _ := org.Settings["suspended"].(bool)
	org.Settings["suspended"] = suspended
	if suspended {
		org.Settings["suspended_at"] = time.Now().Format(time.RFC3339)
		org.Settings["suspended_by"] = superAdminID.String()
	} else {
		delete(org.Settings, "suspended_at")
		delete(org.Settings, "suspended_by")
	}

	if err := a.DB.Save(&org).Error; err != nil {
		a.Log.Error("Failed to update organization suspension", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to update organization", nil, "")
	}

	action := "activated"
	if suspended {
		action = "suspended"
	}
	a.logAudit(org.ID, superAdminID, models.ResourceOrganizations, org.ID, models.AuditActionUpdated,
		map[string]any{"suspended": wasSuspended}, map[string]any{"suspended": suspended})

	return r.SendEnvelope(map[string]any{"message": "Organization " + action + " successfully", "suspended": suspended})
}

// SuspendOrganization blocks sign-in for every user in the organization
// (except super admins, who remain exempt — see Login in auth.go).
func (a *App) SuspendOrganization(r *fastglue.Request) error {
	return a.setOrganizationSuspended(r, true)
}

// ActivateOrganization reverses SuspendOrganization.
func (a *App) ActivateOrganization(r *fastglue.Request) error {
	return a.setOrganizationSuspended(r, false)
}

// DeleteOrganization soft-deletes an organization. Super admin only.
func (a *App) DeleteOrganization(r *fastglue.Request) error {
	superAdminID, err := a.requireSuperAdmin(r)
	if err != nil {
		return nil
	}

	orgID, err := parsePathUUID(r, "id", "organization")
	if err != nil {
		return nil
	}

	var org models.Organization
	if err := a.DB.Where("id = ?", orgID).First(&org).Error; err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusNotFound, "Organization not found", nil, "")
	}

	if err := a.DB.Delete(&org).Error; err != nil {
		a.Log.Error("Failed to delete organization", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to delete organization", nil, "")
	}

	a.logAudit(org.ID, superAdminID, models.ResourceOrganizations, org.ID, models.AuditActionDeleted,
		map[string]any{"name": org.Name}, nil)

	return r.SendEnvelope(map[string]string{"message": "Organization deleted successfully"})
}

// CreateOrganizationWithAdminRequest represents the request body for a super
// admin provisioning a brand-new organization together with its first admin
// login, in one step.
type CreateOrganizationWithAdminRequest struct {
	OrgName       string `json:"org_name"`
	AdminEmail    string `json:"admin_email"`
	AdminFullName string `json:"admin_full_name"`
	AdminPassword string `json:"admin_password"` // optional; auto-generated when empty
}

// CreateOrganizationWithAdminResponse extends OrganizationResponse with the
// newly created admin login. GeneratedPassword is populated exactly once,
// only when the caller didn't supply admin_password themselves.
type CreateOrganizationWithAdminResponse struct {
	OrganizationResponse
	AdminEmail        string `json:"admin_email"`
	GeneratedPassword string `json:"generated_password,omitempty"`
}

// CreateOrganizationWithAdmin lets a super admin provision a new organization
// and its first admin login together. Unlike CreateOrganization (which
// attaches the calling user as the org's admin, for self-serve org creation),
// this creates a brand-new User for admin_email — the super admin is
// provisioning access for someone else, not joining the org themselves.
func (a *App) CreateOrganizationWithAdmin(r *fastglue.Request) error {
	superAdminID, err := a.requireSuperAdmin(r)
	if err != nil {
		return nil
	}

	var req CreateOrganizationWithAdminRequest
	if err := a.decodeRequest(r, &req); err != nil {
		return nil
	}

	if req.OrgName == "" || req.AdminEmail == "" || req.AdminFullName == "" {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "org_name, admin_email, and admin_full_name are required", nil, "")
	}
	if _, err := mail.ParseAddress(req.AdminEmail); err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "Invalid admin email format", nil, "")
	}

	var existingUser models.User
	if err := a.DB.Where("email = ?", req.AdminEmail).First(&existingUser).Error; err == nil {
		return r.SendErrorEnvelope(fasthttp.StatusConflict, "A user with this email already exists", nil, "")
	}

	generatedPassword := ""
	password := req.AdminPassword
	if password == "" {
		generatedPassword = generateRandomString(16)
		password = generatedPassword
	} else if len(password) < 8 {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "admin_password must be at least 8 characters", nil, "")
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		a.Log.Error("Failed to hash password", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to create organization", nil, "")
	}

	tx := a.DB.Begin()
	if tx.Error != nil {
		a.Log.Error("Failed to begin transaction", "error", tx.Error)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to create organization", nil, "")
	}

	org := models.Organization{
		Name:     req.OrgName,
		Slug:     generateSlug(req.OrgName),
		Settings: models.JSONB{},
	}
	if err := tx.Create(&org).Error; err != nil {
		tx.Rollback()
		a.Log.Error("Failed to create organization", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to create organization", nil, "")
	}

	if err := database.SeedSystemRolesForOrg(tx, org.ID); err != nil {
		tx.Rollback()
		a.Log.Error("Failed to seed system roles", "error", err, "org_id", org.ID)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to create organization", nil, "")
	}

	chatbotSettings := models.ChatbotSettings{
		OrganizationID:     org.ID,
		IsEnabled:          false,
		SessionTimeoutMins: 30,
	}
	if err := tx.Create(&chatbotSettings).Error; err != nil {
		tx.Rollback()
		a.Log.Error("Failed to create chatbot settings", "error", err, "org_id", org.ID)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to create organization", nil, "")
	}

	var adminRole models.CustomRole
	if err := tx.Where("organization_id = ? AND name = ? AND is_system = ?", org.ID, "admin", true).First(&adminRole).Error; err != nil {
		tx.Rollback()
		a.Log.Error("Failed to find admin role", "error", err, "org_id", org.ID)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to create organization", nil, "")
	}

	adminUser := models.User{
		OrganizationID: org.ID,
		Email:          req.AdminEmail,
		PasswordHash:   string(hashedPassword),
		FullName:       req.AdminFullName,
		RoleID:         &adminRole.ID,
		IsActive:       true,
	}
	if err := tx.Create(&adminUser).Error; err != nil {
		tx.Rollback()
		a.Log.Error("Failed to create admin user", "error", err, "org_id", org.ID)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to create organization", nil, "")
	}

	userOrg := models.UserOrganization{
		UserID:         adminUser.ID,
		OrganizationID: org.ID,
		RoleID:         &adminRole.ID,
		IsDefault:      true,
	}
	if err := tx.Create(&userOrg).Error; err != nil {
		tx.Rollback()
		a.Log.Error("Failed to link admin user to organization", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to create organization", nil, "")
	}

	if err := database.SeedDefaultWidgetsForOrg(tx, org.ID, adminUser.ID); err != nil {
		tx.Rollback()
		a.Log.Error("Failed to seed default widgets", "error", err, "org_id", org.ID)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to create organization", nil, "")
	}

	if err := tx.Commit().Error; err != nil {
		a.Log.Error("Failed to commit transaction", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to create organization", nil, "")
	}

	a.logAudit(org.ID, superAdminID, models.ResourceOrganizations, org.ID, models.AuditActionCreated,
		nil, map[string]any{"name": org.Name, "admin_email": req.AdminEmail})

	a.Log.Info("Super admin created organization with admin login",
		"org_id", org.ID, "org_name", org.Name, "admin_email", req.AdminEmail, "created_by", superAdminID)

	resp := CreateOrganizationWithAdminResponse{
		OrganizationResponse: organizationToResponse(org),
		AdminEmail:           req.AdminEmail,
		GeneratedPassword:    generatedPassword,
	}
	return r.SendEnvelope(resp)
}

// maxOrgLogoBytes caps the raw uploaded logo file size. Logos are stored
// inline as a base64 data: URI in Organization.Settings (no separate file
// storage/serving infra needed for a small image), and are included in
// organization list/detail responses — kept deliberately small so that
// doesn't bloat those payloads.
const maxOrgLogoBytes = 150 * 1024 // 150KB

var allowedOrgLogoMimeTypes = map[string]bool{
	"image/png":     true,
	"image/jpeg":    true,
	"image/jpg":     true,
	"image/webp":    true,
	"image/svg+xml": true,
}

// UploadOrgLogo uploads (or replaces) the organization's logo. Works for
// both the org's own admin (editing their own org, the common case) and a
// super admin managing a different org via the X-Organization-ID header
// override — both resolve through the same getOrgAndUserID/getOrgID path
// used throughout this file, so no separate code path is needed for either.
func (a *App) UploadOrgLogo(r *fastglue.Request) error {
	orgID, userID, err := a.requireAuth(r, models.ResourceSettingsGeneral, models.ActionWrite)
	if err != nil {
		return nil
	}

	fileHeader, ferr := r.RequestCtx.FormFile("file")
	if ferr != nil {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "No file provided", nil, "")
	}

	mimeType := fileHeader.Header.Get("Content-Type")
	if !allowedOrgLogoMimeTypes[mimeType] {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "Unsupported image type. Use PNG, JPEG, WEBP, or SVG.", nil, "")
	}

	file, ferr := fileHeader.Open()
	if ferr != nil {
		a.Log.Error("Failed to open uploaded logo", "error", ferr)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to open uploaded file", nil, "")
	}
	defer func() { _ = file.Close() }()

	data, rerr := io.ReadAll(io.LimitReader(file, maxOrgLogoBytes+1))
	if rerr != nil {
		a.Log.Error("Failed to read uploaded logo", "error", rerr)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to read uploaded file", nil, "")
	}
	if len(data) > maxOrgLogoBytes {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "Logo is too large (max 150KB) — please use a smaller or more compressed image", nil, "")
	}

	var org models.Organization
	if err := a.DB.Where("id = ?", orgID).First(&org).Error; err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusNotFound, "Organization not found", nil, "")
	}
	if org.Settings == nil {
		org.Settings = models.JSONB{}
	}

	dataURL := "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data)
	org.Settings["logo_data_url"] = dataURL

	if err := a.DB.Save(&org).Error; err != nil {
		a.Log.Error("Failed to save organization logo", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to save logo", nil, "")
	}

	a.logAudit(orgID, userID, models.ResourceSettingsGeneral, orgID, models.AuditActionUpdated,
		map[string]any{"has_logo": false}, map[string]any{"has_logo": true})

	return r.SendEnvelope(map[string]any{
		"message":       "Logo updated successfully",
		"logo_data_url": dataURL,
	})
}

// DeleteOrgLogo removes the organization's logo.
func (a *App) DeleteOrgLogo(r *fastglue.Request) error {
	orgID, userID, err := a.requireAuth(r, models.ResourceSettingsGeneral, models.ActionWrite)
	if err != nil {
		return nil
	}

	var org models.Organization
	if err := a.DB.Where("id = ?", orgID).First(&org).Error; err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusNotFound, "Organization not found", nil, "")
	}
	if org.Settings != nil {
		delete(org.Settings, "logo_data_url")
		if err := a.DB.Save(&org).Error; err != nil {
			a.Log.Error("Failed to remove organization logo", "error", err)
			return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to remove logo", nil, "")
		}
	}

	a.logAudit(orgID, userID, models.ResourceSettingsGeneral, orgID, models.AuditActionUpdated,
		map[string]any{"has_logo": true}, map[string]any{"has_logo": false})

	return r.SendEnvelope(map[string]string{"message": "Logo removed successfully"})
}
