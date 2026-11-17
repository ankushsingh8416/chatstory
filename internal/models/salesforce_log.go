package models

import (
	"time"

	"github.com/google/uuid"
)

// SalesforceSyncLog records one attempt to push a lead or message to
// Salesforce, so a submission's fate (did it go, how, what came back) is
// visible without digging through container logs.
type SalesforceSyncLog struct {
	ID             uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	OrganizationID uuid.UUID `gorm:"type:uuid;index;not null" json:"organization_id"`
	ContactID      uuid.UUID `gorm:"type:uuid;index;not null" json:"contact_id"`
	Kind           string    `gorm:"size:20;not null" json:"kind"` // "lead_capture" | "message"
	Success        bool      `gorm:"not null" json:"success"`
	StatusCode     int       `json:"status_code"`
	LeadID         string    `gorm:"size:50" json:"lead_id"`
	Action         string    `gorm:"size:20" json:"action"` // CREATED / UPDATED, from Salesforce's response
	ErrorMessage   string    `json:"error_message"`
	CreatedAt      time.Time `gorm:"autoCreateTime;index" json:"created_at"`
}

func (SalesforceSyncLog) TableName() string {
	return "salesforce_sync_logs"
}
