package models

import (
	"time"

	"github.com/google/uuid"
)

// Lead records one captured lead — a chatbot form/Transfer/Message node
// submission, or a free-text phone number pickup — regardless of whether
// the Salesforce push for it succeeded. This is the panel's own copy, so
// submissions are visible/exportable even for orgs without Salesforce.
type Lead struct {
	ID             uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	OrganizationID uuid.UUID `gorm:"type:uuid;index;not null" json:"organization_id"`
	ContactID      uuid.UUID `gorm:"type:uuid;index;not null" json:"contact_id"`
	Name           string    `gorm:"size:255" json:"name"`
	Mobile         string    `gorm:"size:50" json:"mobile"`
	Email          string    `gorm:"size:255" json:"email"`
	Project        string    `gorm:"size:255" json:"project"`
	Message        string    `json:"message"`
	Source         string    `gorm:"size:100" json:"source"` // chatbot flow name this lead came from
	CreatedAt      time.Time `gorm:"autoCreateTime;index" json:"created_at"`
}

func (Lead) TableName() string {
	return "leads"
}
