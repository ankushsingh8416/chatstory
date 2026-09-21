package models

import (
	"time"

	"github.com/google/uuid"
)

// DocumentSourceType identifies how a Document's text was provided.
type DocumentSourceType string

const (
	DocumentSourceUpload DocumentSourceType = "upload" // text file uploaded
	DocumentSourceText   DocumentSourceType = "text"   // pasted text
)

// DocumentStatus tracks a Document through the async ingestion pipeline.
type DocumentStatus string

const (
	DocumentStatusPending    DocumentStatus = "pending"
	DocumentStatusProcessing DocumentStatus = "processing"
	DocumentStatusCompleted  DocumentStatus = "completed"
	DocumentStatusFailed     DocumentStatus = "failed"
)

// KnowledgeBase is a named collection of documents bound to one
// DataConnection — the org's own vector store where chunks get embedded and
// upserted, and later (chatbot retrieval) searched.
type KnowledgeBase struct {
	BaseModel
	OrganizationID   uuid.UUID `gorm:"type:uuid;index;not null" json:"organization_id"`
	Name             string    `gorm:"size:255;not null" json:"name"`
	Description      string    `gorm:"type:text" json:"description"`
	DataConnectionID uuid.UUID `gorm:"type:uuid;index;not null" json:"data_connection_id"`
	// CollectionName is the Qdrant collection name, or (for postgres/pgvector)
	// the target table name — created on first ingest if it doesn't exist,
	// unless it already exists (an org's own pre-existing vector table —
	// see ContentColumn/EmbeddingColumn below), in which case it's used
	// read-only and no document is ever ingested into it through this app.
	CollectionName string `gorm:"size:255;not null" json:"collection_name"`
	// ContentColumn/EmbeddingColumn name the text and vector(N) columns to
	// use for Postgres/pgvector search. Default to "content"/"embedding" -
	// the fixed shape this app's own ingestion pipeline creates - but are
	// overridable when CollectionName points at an org's pre-existing table
	// with different column names (every org's own schema differs).
	ContentColumn   string `gorm:"size:100;default:'content'" json:"content_column"`
	EmbeddingColumn string `gorm:"size:100;default:'embedding'" json:"embedding_column"`
	// ExtraTables lists additional existing tables (beyond CollectionName)
	// to search alongside the primary one, for orgs whose data is spread
	// across several vector tables (e.g. blog posts in one, products in
	// another). Each entry: {"table", "content_column", "embedding_column"}.
	// Only meaningful when CollectionName points at a pre-existing table
	// (the "use existing table(s)" flow) — self-managed/ingested knowledge
	// bases always have exactly one table (CollectionName) and this is empty.
	ExtraTables    JSONBArray `gorm:"type:jsonb;default:'[]'" json:"extra_tables"`
	EmbeddingModel string     `gorm:"size:100;default:'text-embedding-3-small'" json:"embedding_model"`
	EmbeddingDims  int        `gorm:"default:1536" json:"embedding_dims"`
	ChunkSize      int        `gorm:"default:1000" json:"chunk_size"`
	ChunkOverlap   int        `gorm:"default:150" json:"chunk_overlap"`
	IsActive       bool       `gorm:"default:true" json:"is_active"`

	CreatedByID *uuid.UUID `gorm:"type:uuid" json:"created_by_id,omitempty"`

	// Relations
	Organization   *Organization   `gorm:"foreignKey:OrganizationID" json:"organization,omitempty"`
	DataConnection *DataConnection `gorm:"foreignKey:DataConnectionID" json:"data_connection,omitempty"`
}

func (KnowledgeBase) TableName() string {
	return "knowledge_bases"
}

// Document is one ingested source (an uploaded text file, or pasted text)
// within a KnowledgeBase. RawText and per-chunk content are kept in
// Whatomate's own database (not just the external vector store) so the KB
// UI can show what's actually indexed, and so re-ingestion doesn't require
// re-uploading — only the embedding vectors live externally.
type Document struct {
	BaseModel
	OrganizationID  uuid.UUID          `gorm:"type:uuid;index;not null" json:"organization_id"`
	KnowledgeBaseID uuid.UUID          `gorm:"type:uuid;index;not null" json:"knowledge_base_id"`
	Name            string             `gorm:"size:255;not null" json:"name"`
	SourceType      DocumentSourceType `gorm:"size:20;not null" json:"source_type"`
	RawText         string             `gorm:"type:text" json:"-"`
	Status          DocumentStatus     `gorm:"size:20;default:'pending'" json:"status"`
	ErrorMessage    string             `gorm:"type:text" json:"error_message,omitempty"`
	ChunkCount      int                `gorm:"default:0" json:"chunk_count"`
	ProcessedAt     *time.Time         `json:"processed_at,omitempty"`

	CreatedByID *uuid.UUID `gorm:"type:uuid" json:"created_by_id,omitempty"`

	// Relations
	KnowledgeBase *KnowledgeBase `gorm:"foreignKey:KnowledgeBaseID" json:"knowledge_base,omitempty"`
}

func (Document) TableName() string {
	return "documents"
}

// DocumentChunk is one chunk of a Document's text, plus VectorRef — the ID
// used to identify this chunk's vector in the external store, so a specific
// chunk's vector can be deleted/re-upserted without touching the rest.
type DocumentChunk struct {
	BaseModel
	DocumentID      uuid.UUID `gorm:"type:uuid;index;not null" json:"document_id"`
	KnowledgeBaseID uuid.UUID `gorm:"type:uuid;index;not null" json:"knowledge_base_id"` // denormalized for direct KB-scoped queries
	ChunkIndex      int       `gorm:"not null" json:"chunk_index"`
	Content         string    `gorm:"type:text;not null" json:"content"`
	VectorRef       string    `gorm:"size:100" json:"vector_ref"`
	TokenCount      int       `gorm:"default:0" json:"token_count"`

	// Relations
	Document *Document `gorm:"foreignKey:DocumentID" json:"document,omitempty"`
}

func (DocumentChunk) TableName() string {
	return "document_chunks"
}
