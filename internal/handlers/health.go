package handlers

import (
	"time"

	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/valyala/fasthttp"
	"github.com/zerodha/fastglue"
)

// AccountHealthStatus is a single WhatsApp account's live health snapshot,
// surfaced on the dashboard so issues (dead token, chatbot off, send
// failures) are visible without having to dig through logs per-account.
type AccountHealthStatus struct {
	AccountID         string `json:"account_id"`
	AccountName       string `json:"account_name"`
	TokenValid        bool   `json:"token_valid"`
	TokenError        string `json:"token_error,omitempty"`
	ChatbotEnabled    bool   `json:"chatbot_enabled"`
	AIEnabled         bool   `json:"ai_enabled"`
	FailedMessages24h int64  `json:"failed_messages_24h"`
	StuckContacts     int64  `json:"stuck_contacts"`
}

// HealthOverviewResponse is the dashboard health-check payload.
type HealthOverviewResponse struct {
	Accounts  []AccountHealthStatus `json:"accounts"`
	CheckedAt string                `json:"checked_at"`
}

// GetHealthOverview checks every WhatsApp account's token validity (live
// call to Meta), chatbot/AI enablement, recent outgoing send failures, and
// contacts stuck in a chatbot-disabled agent queue — so a broken account
// shows up the moment someone opens the dashboard, not after a customer
// complains.
func (a *App) GetHealthOverview(r *fastglue.Request) error {
	orgID, _, err := a.requireAuth(r, models.ResourceAccounts, models.ActionRead)
	if err != nil {
		return nil
	}

	var accounts []models.WhatsAppAccount
	if err := a.DB.Where("organization_id = ?", orgID).Order("created_at ASC").Find(&accounts).Error; err != nil {
		a.Log.Error("Failed to list accounts for health check", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to check account health", nil, "")
	}

	since := time.Now().Add(-24 * time.Hour)
	results := make([]AccountHealthStatus, 0, len(accounts))

	for _, acc := range accounts {
		status := AccountHealthStatus{
			AccountID:   acc.ID.String(),
			AccountName: acc.Name,
		}

		a.decryptAccountSecrets(&acc)
		if err := a.validateAccountCredentials(acc.PhoneID, acc.BusinessID, acc.AccessToken, acc.APIVersion); err != nil {
			status.TokenError = err.Error()
		} else {
			status.TokenValid = true
		}

		if settings, err := a.getChatbotSettingsCached(orgID, acc.Name); err == nil && settings != nil {
			status.ChatbotEnabled = settings.IsEnabled
			status.AIEnabled = settings.AI.Enabled
		}

		a.DB.Model(&models.Message{}).
			Where("organization_id = ? AND whats_app_account = ? AND direction = ? AND status = ? AND created_at >= ?",
				orgID, acc.Name, models.DirectionOutgoing, models.MessageStatusFailed, since).
			Count(&status.FailedMessages24h)

		a.DB.Model(&models.AgentTransfer{}).
			Where("organization_id = ? AND whats_app_account = ? AND status = ? AND source = ?",
				orgID, acc.Name, models.TransferStatusActive, models.TransferSourceChatbotDisabled).
			Count(&status.StuckContacts)

		results = append(results, status)
	}

	return r.SendEnvelope(HealthOverviewResponse{
		Accounts:  results,
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
	})
}
