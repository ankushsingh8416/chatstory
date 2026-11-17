package handlers

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/crypto"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/internal/salesforce"
	"github.com/valyala/fasthttp"
	"github.com/zerodha/fastglue"
	"gorm.io/gorm"
)

// SalesforceSyncLogResponse is one row of "did this lead/message reach
// Salesforce" — the contact's name/phone are joined in so the UI doesn't
// need a second round trip per row.
type SalesforceSyncLogResponse struct {
	ID           uuid.UUID `json:"id"`
	ContactID    uuid.UUID `json:"contact_id"`
	ContactName  string    `json:"contact_name"`
	ContactPhone string    `json:"contact_phone"`
	Kind         string    `json:"kind"`
	Success      bool      `json:"success"`
	StatusCode   int       `json:"status_code"`
	LeadID       string    `json:"lead_id"`
	Action       string    `json:"action"`
	ErrorMessage string    `json:"error_message"`
	CreatedAt    time.Time `json:"created_at"`
}

// ListSalesforceSyncLogs answers "did this submission reach Salesforce, how,
// and what did it come back with" for every lead/message sync attempt.
// Supported query params: contact_id, success ("true"/"false"), page, limit.
func (a *App) ListSalesforceSyncLogs(r *fastglue.Request) error {
	orgID, _, err := a.requireAuth(r, models.ResourceAccounts, models.ActionRead)
	if err != nil {
		return nil
	}

	baseQuery := a.DB.Where("organization_id = ?", orgID)
	if v := string(r.RequestCtx.QueryArgs().Peek("contact_id")); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			baseQuery = baseQuery.Where("contact_id = ?", id)
		}
	}
	if v := string(r.RequestCtx.QueryArgs().Peek("success")); v != "" {
		baseQuery = baseQuery.Where("success = ?", v == "true")
	}

	pg := parsePagination(r)

	var logs []models.SalesforceSyncLog
	var total int64
	countQuery := baseQuery.Session(&gorm.Session{})
	countQuery.Model(&models.SalesforceSyncLog{}).Count(&total)

	if err := pg.Apply(baseQuery.Order("created_at DESC")).Find(&logs).Error; err != nil {
		a.Log.Error("Failed to list Salesforce sync logs", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError,
			"Failed to list Salesforce sync logs", nil, "")
	}

	contactIDs := make([]uuid.UUID, 0, len(logs))
	for _, l := range logs {
		contactIDs = append(contactIDs, l.ContactID)
	}
	var contacts []models.Contact
	if len(contactIDs) > 0 {
		a.DB.Where("id IN ?", contactIDs).Find(&contacts)
	}
	contactByID := make(map[uuid.UUID]models.Contact, len(contacts))
	for _, c := range contacts {
		contactByID[c.ID] = c
	}

	response := make([]SalesforceSyncLogResponse, len(logs))
	for i, l := range logs {
		c := contactByID[l.ContactID]
		response[i] = SalesforceSyncLogResponse{
			ID:           l.ID,
			ContactID:    l.ContactID,
			ContactName:  c.ProfileName,
			ContactPhone: c.PhoneNumber,
			Kind:         l.Kind,
			Success:      l.Success,
			StatusCode:   l.StatusCode,
			LeadID:       l.LeadID,
			Action:       l.Action,
			ErrorMessage: l.ErrorMessage,
			CreatedAt:    l.CreatedAt,
		}
	}

	return r.SendEnvelope(listEnvelope("salesforce_sync_logs", response, total, pg))
}

// getSalesforceClient builds a client from the org's stored settings. Returns
// (nil, nil) when the integration isn't enabled/configured for this org —
// callers should treat that as "nothing to do", not an error.
func (a *App) getSalesforceClient(orgID uuid.UUID) (*salesforce.Client, error) {
	var org models.Organization
	if err := a.DB.Where("id = ?", orgID).First(&org).Error; err != nil {
		return nil, err
	}
	if org.Settings == nil {
		return nil, nil
	}
	enabled, _ := org.Settings["salesforce_enabled"].(bool)
	if !enabled {
		return nil, nil
	}
	clientID, _ := org.Settings["salesforce_client_id"].(string)
	secretEnc, _ := org.Settings["salesforce_client_secret_encrypted"].(string)
	authURL, _ := org.Settings["salesforce_auth_url"].(string)
	leadURL, _ := org.Settings["salesforce_lead_api_url"].(string)
	if clientID == "" || secretEnc == "" || authURL == "" || leadURL == "" {
		return nil, nil
	}
	secret, err := crypto.Decrypt(secretEnc, a.Config.App.EncryptionKey)
	if err != nil {
		return nil, err
	}
	return salesforce.New(salesforce.Config{
		ClientID:     clientID,
		ClientSecret: secret,
		AuthURL:      authURL,
		LeadAPIURL:   leadURL,
	}), nil
}

// salesforceMeta reads/writes the small tracking block this app keeps in
// Contact.Metadata once a Lead exists for that contact — no separate table
// needed, and it's what makes the lead visible on the contact in-panel too.
type salesforceMeta struct {
	LeadID         string `json:"lead_id"`
	ConversationID string `json:"conversation_id"`
	SyncedAt       string `json:"synced_at"`
}

func readSalesforceMeta(contact *models.Contact) *salesforceMeta {
	raw, ok := contact.Metadata["salesforce"].(map[string]any)
	if !ok {
		return nil
	}
	m := &salesforceMeta{}
	m.LeadID, _ = raw["lead_id"].(string)
	m.ConversationID, _ = raw["conversation_id"].(string)
	m.SyncedAt, _ = raw["synced_at"].(string)
	if m.LeadID == "" {
		return nil
	}
	return m
}

// logSalesforceSync records one sync attempt so "did this submission reach
// Salesforce, how, and with what status" can be answered from the DB
// instead of grepping container logs. resp may be nil (e.g. the client
// itself failed to build); err nil means success.
func (a *App) logSalesforceSync(orgID, contactID uuid.UUID, kind string, resp *salesforce.LeadResponse, err error) {
	entry := models.SalesforceSyncLog{OrganizationID: orgID, ContactID: contactID, Kind: kind}
	if resp != nil {
		entry.StatusCode = resp.StatusCode
		entry.LeadID = resp.LeadID
		entry.Action = resp.Action
	}
	if err != nil {
		entry.ErrorMessage = err.Error()
	} else if resp != nil {
		entry.Success = resp.Success
	}
	if dbErr := a.DB.Create(&entry).Error; dbErr != nil {
		a.Log.Error("Failed to persist Salesforce sync log", "error", dbErr, "contact_id", contactID)
	}
}

// syncLeadFirstContact fires once, when a WhatsApp Flow submission (or any
// future capture point) has collected name + mobile. It creates/updates the
// Salesforce Lead and records the result on the contact so every later
// message can be appended to the same Lead (see syncMessageToSalesforce).
func (a *App) syncLeadFirstContact(orgID uuid.UUID, contact *models.Contact, name, mobile, email, project, initialMessage string) {
	if name == "" || mobile == "" {
		return
	}
	client, err := a.getSalesforceClient(orgID)
	if err != nil {
		a.Log.Error("Failed to build Salesforce client", "error", err, "org_id", orgID)
		return
	}
	if client == nil {
		return
	}

	conversationID := contact.ID.String()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	resp, err := client.CreateOrUpdateLead(ctx, salesforce.FirstContactPayload{
		Name:           name,
		Mobile:         mobile,
		Email:          email,
		Project:        project,
		Message:        initialMessage,
		ConversationID: conversationID,
		AgentName:      "Whatomate",
		Messages: []salesforce.MessageItem{{
			MessageID: "capture_" + conversationID,
			Direction: "Inbound",
			Sender:    name,
			Body:      initialMessage,
		}},
	})
	a.logSalesforceSync(orgID, contact.ID, "lead_capture", resp, err)
	if err != nil {
		a.Log.Error("Salesforce lead capture failed", "error", err, "contact_id", contact.ID)
		return
	}
	if !resp.Success || resp.LeadID == "" {
		a.Log.Warn("Salesforce lead capture returned no lead id", "response", resp.Message, "contact_id", contact.ID)
		return
	}

	if contact.Metadata == nil {
		contact.Metadata = models.JSONB{}
	}
	contact.Metadata["salesforce"] = map[string]any{
		"lead_id":         resp.LeadID,
		"conversation_id": conversationID,
		"synced_at":       time.Now().UTC().Format(time.RFC3339),
	}
	if err := a.DB.Model(contact).Update("metadata", contact.Metadata).Error; err != nil {
		a.Log.Error("Failed to persist Salesforce lead metadata", "error", err, "contact_id", contact.ID)
		return
	}
	a.Log.Info("Salesforce lead synced", "lead_id", resp.LeadID, "action", resp.Action, "contact_id", contact.ID)
}

// syncMessageToSalesforce appends a single message to an existing Lead's
// history. No-op when this contact has no tracked Lead yet — scoped this
// way so ordinary chats that never became a lead don't hit Salesforce at
// all. Fire-and-forget: a Salesforce outage must never block message
// delivery, so callers run this in a goroutine.
func (a *App) syncMessageToSalesforce(orgID uuid.UUID, contact *models.Contact, direction, sender, body, wamid string) {
	meta := readSalesforceMeta(contact)
	if meta == nil {
		return
	}
	client, err := a.getSalesforceClient(orgID)
	if err != nil {
		a.Log.Error("Failed to build Salesforce client", "error", err, "org_id", orgID)
		return
	}
	if client == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if wamid == "" {
		wamid = uuid.New().String()
	}
	resp, err := client.AppendMessages(ctx, salesforce.FollowUpPayload{
		Mobile:         contact.PhoneNumber,
		ConversationID: meta.ConversationID,
		Messages: []salesforce.MessageItem{{
			MessageID: wamid,
			Direction: direction,
			Sender:    sender,
			Body:      body,
		}},
	})
	a.logSalesforceSync(orgID, contact.ID, "message", resp, err)
	if err != nil {
		a.Log.Error("Salesforce message sync failed", "error", err, "contact_id", contact.ID)
	}
}
