package handlers

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/crypto"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/internal/salesforce"
)

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
	_, err = client.AppendMessages(ctx, salesforce.FollowUpPayload{
		Mobile:         contact.PhoneNumber,
		ConversationID: meta.ConversationID,
		Messages: []salesforce.MessageItem{{
			MessageID: wamid,
			Direction: direction,
			Sender:    sender,
			Body:      body,
		}},
	})
	if err != nil {
		a.Log.Error("Salesforce message sync failed", "error", err, "contact_id", contact.ID)
	}
}
