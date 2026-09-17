package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/crypto"
)

// DataConnectionType identifies which external system a DataConnection points at.
type DataConnectionType string

const (
	DataConnectionTypePostgres DataConnectionType = "postgres" // also covers Supabase (same wire protocol)
	DataConnectionTypeQdrant   DataConnectionType = "qdrant"
	// MongoDB is intentionally not implemented yet — add a new type + a new
	// internal/vectorstore implementation when needed, nothing else changes.
)

// DataConnectionStatus reflects the result of the most recent connection test.
type DataConnectionStatus string

const (
	DataConnectionStatusUntested DataConnectionStatus = "untested"
	DataConnectionStatusOK       DataConnectionStatus = "ok"
	DataConnectionStatusFailed   DataConnectionStatus = "failed"
)

// DataConnection stores an org's own external database/vector-store
// credentials — e.g. their Supabase/Postgres instance or Qdrant cluster —
// so the org can bring their own data store for chatbot knowledge bases.
// One table, type-discriminated, rather than separate tables per type:
// only the fields relevant to Type are populated, matching how other
// variant-config rows in this codebase (e.g. Template header fields) work.
type DataConnection struct {
	BaseModel
	OrganizationID uuid.UUID          `gorm:"type:uuid;index;not null" json:"organization_id"`
	Name           string             `gorm:"size:255;not null" json:"name"`
	Type           DataConnectionType `gorm:"size:20;not null" json:"type"`

	// Postgres/Supabase fields (Type=postgres)
	Host     string `gorm:"size:255" json:"host,omitempty"`
	Port     int    `gorm:"default:5432" json:"port,omitempty"`
	Database string `gorm:"size:255" json:"database,omitempty"`
	Username string `gorm:"size:255" json:"username,omitempty"`
	Password string `gorm:"type:text" json:"-"` // encrypted
	SSLMode  string `gorm:"size:20;default:'require'" json:"ssl_mode,omitempty"`

	// Qdrant fields (Type=qdrant)
	QdrantURL    string `gorm:"type:text" json:"qdrant_url,omitempty"`
	QdrantAPIKey string `gorm:"type:text" json:"-"` // encrypted

	// Connection test status, surfaced in the UI so an org knows a
	// connection works before pointing a knowledge base at it.
	Status        DataConnectionStatus `gorm:"size:20;default:'untested'" json:"status"`
	LastTestedAt  *time.Time           `json:"last_tested_at,omitempty"`
	LastTestError string               `gorm:"type:text" json:"last_test_error,omitempty"`

	CreatedByID *uuid.UUID `gorm:"type:uuid" json:"created_by_id,omitempty"`
	UpdatedByID *uuid.UUID `gorm:"type:uuid" json:"updated_by_id,omitempty"`

	// Relations
	Organization *Organization `gorm:"foreignKey:OrganizationID" json:"organization,omitempty"`
	CreatedBy    *User         `gorm:"foreignKey:CreatedByID" json:"created_by,omitempty"`
	UpdatedBy    *User         `gorm:"foreignKey:UpdatedByID" json:"updated_by,omitempty"`
}

func (DataConnection) TableName() string {
	return "data_connections"
}

// DecryptSecrets decrypts the encrypted credential fields, mirroring
// WhatsAppAccount.DecryptSecrets. Call this only right before using the
// credentials (e.g. to dial the external store) — never persist or log the
// decrypted struct.
func (d *DataConnection) DecryptSecrets(encryptionKey string) {
	crypto.DecryptFields(encryptionKey, &d.Password, &d.QdrantAPIKey)
}
