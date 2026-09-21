package handlers

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/crypto"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/internal/vectorstore"
	"github.com/valyala/fasthttp"
	"github.com/zerodha/fastglue"
)

// maxDocumentTextBytes caps how much text a single document can contain —
// generous for a knowledge-base article/FAQ, small enough to keep one
// ingestion job (embedding + upsert) bounded.
const maxDocumentTextBytes = 2 * 1024 * 1024 // 2MB

var validCollectionNameRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]{0,62}$`)

func validCollectionName(name string) bool {
	return validCollectionNameRe.MatchString(name)
}

// KBTableRef names one existing vector table and its column mapping — used
// both for the primary table (CollectionName/ContentColumn/EmbeddingColumn)
// and for each entry of ExtraTables, when a knowledge base searches across
// several of an org's own pre-existing tables at once.
type KBTableRef struct {
	Table           string `json:"table"`
	ContentColumn   string `json:"content_column"`
	EmbeddingColumn string `json:"embedding_column"`
}

// KnowledgeBaseRequest represents the request body for creating/editing a knowledge base.
type KnowledgeBaseRequest struct {
	Name             string    `json:"name"`
	Description      string    `json:"description"`
	DataConnectionID uuid.UUID `json:"data_connection_id"`
	CollectionName   string    `json:"collection_name"`
	// ContentColumn/EmbeddingColumn are only meaningful for Postgres
	// connections and only need to be set when CollectionName points at an
	// org's own pre-existing table (empty defaults to "content"/"embedding",
	// this app's own ingestion pipeline shape).
	ContentColumn   string `json:"content_column"`
	EmbeddingColumn string `json:"embedding_column"`
	// ExtraTables lists additional existing tables to search alongside
	// CollectionName — see models.KnowledgeBase.ExtraTables.
	ExtraTables   []KBTableRef `json:"extra_tables"`
	EmbeddingDims int          `json:"embedding_dims"`
	ChunkSize     int          `json:"chunk_size"`
	ChunkOverlap  int          `json:"chunk_overlap"`
	IsActive      *bool        `json:"is_active"`
}

// extraTablesFromRequest validates and converts the request's ExtraTables
// into the JSONB shape stored on the model, defaulting column names and
// skipping a table that duplicates the primary collection (already searched
// via CollectionName, so listing it again would just double-count its hits).
func extraTablesFromRequest(refs []KBTableRef, primaryCollection string) (models.JSONBArray, error) {
	arr := make(models.JSONBArray, 0, len(refs))
	seen := map[string]bool{primaryCollection: true}
	for _, ref := range refs {
		table := strings.TrimSpace(ref.Table)
		if table == "" || seen[table] {
			continue
		}
		if !validCollectionName(table) {
			return nil, fmt.Errorf("invalid table name %q", table)
		}
		seen[table] = true

		contentColumn := ref.ContentColumn
		if contentColumn == "" {
			contentColumn = "content"
		}
		embeddingColumn := ref.EmbeddingColumn
		if embeddingColumn == "" {
			embeddingColumn = "embedding"
		}
		arr = append(arr, map[string]any{
			"table":            table,
			"content_column":   contentColumn,
			"embedding_column": embeddingColumn,
		})
	}
	return arr, nil
}

// extraTablesToResponse converts the model's stored JSONB shape back into
// the typed API response shape.
func extraTablesToResponse(arr models.JSONBArray) []KBTableRef {
	refs := make([]KBTableRef, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		table, _ := m["table"].(string)
		if table == "" {
			continue
		}
		content, _ := m["content_column"].(string)
		embedding, _ := m["embedding_column"].(string)
		refs = append(refs, KBTableRef{Table: table, ContentColumn: content, EmbeddingColumn: embedding})
	}
	return refs
}

// KnowledgeBaseResponse represents a knowledge base in API responses.
type KnowledgeBaseResponse struct {
	ID                 uuid.UUID    `json:"id"`
	Name               string       `json:"name"`
	Description        string       `json:"description"`
	DataConnectionID   uuid.UUID    `json:"data_connection_id"`
	DataConnectionName string       `json:"data_connection_name,omitempty"`
	CollectionName     string       `json:"collection_name"`
	ContentColumn      string       `json:"content_column"`
	EmbeddingColumn    string       `json:"embedding_column"`
	ExtraTables        []KBTableRef `json:"extra_tables"`
	EmbeddingModel     string       `json:"embedding_model"`
	EmbeddingDims      int          `json:"embedding_dims"`
	ChunkSize          int          `json:"chunk_size"`
	ChunkOverlap       int          `json:"chunk_overlap"`
	IsActive           bool         `json:"is_active"`
	DocumentCount      int64        `json:"document_count"`
	CreatedAt          string       `json:"created_at"`
	UpdatedAt          string       `json:"updated_at"`
}

func knowledgeBaseToResponse(kb models.KnowledgeBase) KnowledgeBaseResponse {
	resp := KnowledgeBaseResponse{
		ID:               kb.ID,
		Name:             kb.Name,
		Description:      kb.Description,
		DataConnectionID: kb.DataConnectionID,
		CollectionName:   kb.CollectionName,
		ContentColumn:    kb.ContentColumn,
		EmbeddingColumn:  kb.EmbeddingColumn,
		ExtraTables:      extraTablesToResponse(kb.ExtraTables),
		EmbeddingModel:   kb.EmbeddingModel,
		EmbeddingDims:    kb.EmbeddingDims,
		ChunkSize:        kb.ChunkSize,
		ChunkOverlap:     kb.ChunkOverlap,
		IsActive:         kb.IsActive,
		CreatedAt:        kb.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:        kb.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
	if kb.DataConnection != nil {
		resp.DataConnectionName = kb.DataConnection.Name
	}
	return resp
}

// DocumentResponse represents a document in API responses.
type DocumentResponse struct {
	ID              uuid.UUID                 `json:"id"`
	KnowledgeBaseID uuid.UUID                 `json:"knowledge_base_id"`
	Name            string                    `json:"name"`
	SourceType      models.DocumentSourceType `json:"source_type"`
	Status          models.DocumentStatus     `json:"status"`
	ErrorMessage    string                    `json:"error_message,omitempty"`
	ChunkCount      int                       `json:"chunk_count"`
	ProcessedAt     *time.Time                `json:"processed_at,omitempty"`
	CreatedAt       string                    `json:"created_at"`
	UpdatedAt       string                    `json:"updated_at"`
}

func documentToResponse(d models.Document) DocumentResponse {
	return DocumentResponse{
		ID:              d.ID,
		KnowledgeBaseID: d.KnowledgeBaseID,
		Name:            d.Name,
		SourceType:      d.SourceType,
		Status:          d.Status,
		ErrorMessage:    d.ErrorMessage,
		ChunkCount:      d.ChunkCount,
		ProcessedAt:     d.ProcessedAt,
		CreatedAt:       d.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:       d.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
}

// orgHasEmbeddingsKey checks whether the org has an OpenAI embeddings key
// configured (see Organization.Settings["openai_embeddings_key_encrypted"]).
func (a *App) orgHasEmbeddingsKey(orgID uuid.UUID) bool {
	var org models.Organization
	if err := a.DB.Where("id = ?", orgID).First(&org).Error; err != nil {
		return false
	}
	key, _ := org.Settings["openai_embeddings_key_encrypted"].(string)
	return key != ""
}

// ListKnowledgeBases returns all knowledge bases for the organization.
func (a *App) ListKnowledgeBases(r *fastglue.Request) error {
	orgID, _, err := a.requireAuth(r, models.ResourceKnowledgeBase, models.ActionRead)
	if err != nil {
		return nil
	}

	var kbs []models.KnowledgeBase
	if err := a.DB.Where("organization_id = ?", orgID).Preload("DataConnection").Order("name ASC").Find(&kbs).Error; err != nil {
		a.Log.Error("Failed to list knowledge bases", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to list knowledge bases", nil, "")
	}

	var counts []struct {
		KnowledgeBaseID uuid.UUID
		Count           int64
	}
	a.DB.Model(&models.Document{}).
		Select("knowledge_base_id, COUNT(*) as count").
		Where("organization_id = ?", orgID).
		Group("knowledge_base_id").
		Scan(&counts)
	countMap := make(map[uuid.UUID]int64, len(counts))
	for _, c := range counts {
		countMap[c.KnowledgeBaseID] = c.Count
	}

	response := make([]KnowledgeBaseResponse, len(kbs))
	for i, kb := range kbs {
		resp := knowledgeBaseToResponse(kb)
		resp.DocumentCount = countMap[kb.ID]
		response[i] = resp
	}

	return r.SendEnvelope(map[string]any{"knowledge_bases": response})
}

// CreateKnowledgeBase creates a new knowledge base bound to a data connection.
func (a *App) CreateKnowledgeBase(r *fastglue.Request) error {
	orgID, userID, err := a.requireAuth(r, models.ResourceKnowledgeBase, models.ActionWrite)
	if err != nil {
		return nil
	}

	var req KnowledgeBaseRequest
	if err := a.decodeRequest(r, &req); err != nil {
		return nil
	}

	if req.Name == "" {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "name is required", nil, "")
	}
	if req.DataConnectionID == uuid.Nil {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "data_connection_id is required", nil, "")
	}
	if !validCollectionName(req.CollectionName) {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "collection_name must start with a letter or underscore and contain only letters, digits, and underscores (max 63 chars)", nil, "")
	}

	var conn models.DataConnection
	if err := a.DB.Where("id = ? AND organization_id = ?", req.DataConnectionID, orgID).First(&conn).Error; err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "Invalid data connection", nil, "")
	}

	dims := req.EmbeddingDims
	if dims <= 0 {
		dims = 1536
	}
	chunkSize := req.ChunkSize
	if chunkSize <= 0 {
		chunkSize = 1000
	}
	chunkOverlap := req.ChunkOverlap
	if chunkOverlap < 0 || chunkOverlap >= chunkSize {
		chunkOverlap = 150
	}

	contentColumn := req.ContentColumn
	if contentColumn == "" {
		contentColumn = "content"
	}
	embeddingColumn := req.EmbeddingColumn
	if embeddingColumn == "" {
		embeddingColumn = "embedding"
	}

	extraTables, err := extraTablesFromRequest(req.ExtraTables, req.CollectionName)
	if err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, err.Error(), nil, "")
	}

	kb := models.KnowledgeBase{
		OrganizationID:   orgID,
		Name:             req.Name,
		Description:      req.Description,
		DataConnectionID: conn.ID,
		CollectionName:   req.CollectionName,
		ContentColumn:    contentColumn,
		EmbeddingColumn:  embeddingColumn,
		ExtraTables:      extraTables,
		EmbeddingModel:   openAIEmbeddingModel,
		EmbeddingDims:    dims,
		ChunkSize:        chunkSize,
		ChunkOverlap:     chunkOverlap,
		IsActive:         true,
		CreatedByID:      &userID,
	}

	if err := a.DB.Create(&kb).Error; err != nil {
		a.Log.Error("Failed to create knowledge base", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to create knowledge base", nil, "")
	}

	a.logAudit(orgID, userID, models.ResourceKnowledgeBase, kb.ID, models.AuditActionCreated,
		nil, map[string]any{"name": kb.Name})

	kb.DataConnection = &conn
	return r.SendEnvelope(knowledgeBaseToResponse(kb))
}

// UpdateKnowledgeBase edits a knowledge base's name/description/chunking
// settings/active flag. DataConnectionID, CollectionName, and EmbeddingDims
// are intentionally not editable — changing them would orphan already-
// ingested vectors. Delete and recreate the knowledge base instead.
func (a *App) UpdateKnowledgeBase(r *fastglue.Request) error {
	orgID, userID, err := a.requireAuth(r, models.ResourceKnowledgeBase, models.ActionWrite)
	if err != nil {
		return nil
	}

	id, err := parsePathUUID(r, "id", "knowledge base")
	if err != nil {
		return nil
	}

	var kb models.KnowledgeBase
	if err := a.DB.Where("id = ? AND organization_id = ?", id, orgID).Preload("DataConnection").First(&kb).Error; err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusNotFound, "Knowledge base not found", nil, "")
	}

	var req KnowledgeBaseRequest
	if err := a.decodeRequest(r, &req); err != nil {
		return nil
	}

	oldName := kb.Name
	if req.Name != "" {
		kb.Name = req.Name
	}
	kb.Description = req.Description
	if req.ChunkSize > 0 {
		kb.ChunkSize = req.ChunkSize
	}
	if req.ChunkOverlap >= 0 && req.ChunkOverlap < kb.ChunkSize {
		kb.ChunkOverlap = req.ChunkOverlap
	}
	if req.IsActive != nil {
		kb.IsActive = *req.IsActive
	}

	if err := a.DB.Save(&kb).Error; err != nil {
		a.Log.Error("Failed to update knowledge base", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to update knowledge base", nil, "")
	}

	a.logAudit(orgID, userID, models.ResourceKnowledgeBase, kb.ID, models.AuditActionUpdated,
		map[string]any{"name": oldName}, map[string]any{"name": kb.Name})

	return r.SendEnvelope(knowledgeBaseToResponse(kb))
}

// DeleteKnowledgeBase deletes a knowledge base and its documents/chunks.
// Vectors already written to the external store are NOT automatically
// removed (the org's connection may be gone/changed by then) — acceptable
// for phase 1, matching the same tradeoff documented for document deletion.
func (a *App) DeleteKnowledgeBase(r *fastglue.Request) error {
	orgID, userID, err := a.requireAuth(r, models.ResourceKnowledgeBase, models.ActionDelete)
	if err != nil {
		return nil
	}

	id, err := parsePathUUID(r, "id", "knowledge base")
	if err != nil {
		return nil
	}

	var kb models.KnowledgeBase
	if err := a.DB.Where("id = ? AND organization_id = ?", id, orgID).First(&kb).Error; err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusNotFound, "Knowledge base not found", nil, "")
	}

	a.DB.Where("knowledge_base_id = ?", kb.ID).Delete(&models.DocumentChunk{})
	a.DB.Where("knowledge_base_id = ?", kb.ID).Delete(&models.Document{})
	if err := a.DB.Delete(&kb).Error; err != nil {
		a.Log.Error("Failed to delete knowledge base", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to delete knowledge base", nil, "")
	}

	a.logAudit(orgID, userID, models.ResourceKnowledgeBase, kb.ID, models.AuditActionDeleted,
		map[string]any{"name": kb.Name}, nil)

	return r.SendEnvelope(map[string]string{"message": "Knowledge base deleted successfully"})
}

// ListDocuments returns all documents in a knowledge base.
func (a *App) ListDocuments(r *fastglue.Request) error {
	orgID, _, err := a.requireAuth(r, models.ResourceKnowledgeBase, models.ActionRead)
	if err != nil {
		return nil
	}

	kbID, err := parsePathUUID(r, "id", "knowledge base")
	if err != nil {
		return nil
	}

	var kb models.KnowledgeBase
	if err := a.DB.Where("id = ? AND organization_id = ?", kbID, orgID).First(&kb).Error; err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusNotFound, "Knowledge base not found", nil, "")
	}

	var docs []models.Document
	if err := a.DB.Where("knowledge_base_id = ?", kb.ID).Order("created_at DESC").Find(&docs).Error; err != nil {
		a.Log.Error("Failed to list documents", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to list documents", nil, "")
	}

	response := make([]DocumentResponse, len(docs))
	for i, d := range docs {
		response[i] = documentToResponse(d)
	}

	return r.SendEnvelope(map[string]any{"documents": response})
}

// GetDocument returns one document — used by the frontend to poll ingestion status.
func (a *App) GetDocument(r *fastglue.Request) error {
	orgID, _, err := a.requireAuth(r, models.ResourceKnowledgeBase, models.ActionRead)
	if err != nil {
		return nil
	}

	docID, err := parsePathUUID(r, "doc_id", "document")
	if err != nil {
		return nil
	}

	var doc models.Document
	if err := a.DB.Where("id = ? AND organization_id = ?", docID, orgID).First(&doc).Error; err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusNotFound, "Document not found", nil, "")
	}

	return r.SendEnvelope(documentToResponse(doc))
}

// CreateDocument ingests a new document into a knowledge base — either a
// pasted-text JSON body ({name, text}) or a multipart file upload (field
// "file", optional "name"). Only plain text/.md files are supported for
// upload in this phase; PDF/DOCX extraction is a natural follow-up that
// needs a parsing dependency, deliberately deferred to keep this pass small.
// Ingestion (chunk → embed → upsert) runs asynchronously — this returns
// immediately with the document in "pending" status.
func (a *App) CreateDocument(r *fastglue.Request) error {
	orgID, userID, err := a.requireAuth(r, models.ResourceKnowledgeBase, models.ActionWrite)
	if err != nil {
		return nil
	}

	kbID, err := parsePathUUID(r, "id", "knowledge base")
	if err != nil {
		return nil
	}

	var kb models.KnowledgeBase
	if err := a.DB.Where("id = ? AND organization_id = ?", kbID, orgID).First(&kb).Error; err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusNotFound, "Knowledge base not found", nil, "")
	}

	if !a.orgHasEmbeddingsKey(orgID) {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "Add an OpenAI API key in Settings (used for document embeddings) before uploading documents", nil, "")
	}

	var name, text string
	var sourceType models.DocumentSourceType

	contentType := string(r.RequestCtx.Request.Header.ContentType())
	if strings.HasPrefix(contentType, "multipart/form-data") {
		fileHeader, ferr := r.RequestCtx.FormFile("file")
		if ferr != nil {
			return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "No file provided", nil, "")
		}
		lowerName := strings.ToLower(fileHeader.Filename)
		if !strings.HasSuffix(lowerName, ".txt") && !strings.HasSuffix(lowerName, ".md") {
			return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "Only .txt and .md files are supported right now", nil, "")
		}

		file, ferr := fileHeader.Open()
		if ferr != nil {
			a.Log.Error("Failed to open uploaded file", "error", ferr)
			return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to open uploaded file", nil, "")
		}
		defer func() { _ = file.Close() }()

		data, rerr := io.ReadAll(io.LimitReader(file, maxDocumentTextBytes+1))
		if rerr != nil {
			a.Log.Error("Failed to read uploaded file", "error", rerr)
			return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to read uploaded file", nil, "")
		}
		if len(data) > maxDocumentTextBytes {
			return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "File is too large (max 2MB)", nil, "")
		}

		text = string(data)
		name = fileHeader.Filename
		if n := string(r.RequestCtx.FormValue("name")); n != "" {
			name = n
		}
		sourceType = models.DocumentSourceUpload
	} else {
		var req struct {
			Name string `json:"name"`
			Text string `json:"text"`
		}
		if err := a.decodeRequest(r, &req); err != nil {
			return nil
		}
		if req.Name == "" || req.Text == "" {
			return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "name and text are required", nil, "")
		}
		if len(req.Text) > maxDocumentTextBytes {
			return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "Text is too large (max 2MB)", nil, "")
		}
		name = req.Name
		text = req.Text
		sourceType = models.DocumentSourceText
	}

	if strings.TrimSpace(text) == "" {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "Document has no text content", nil, "")
	}

	doc := models.Document{
		OrganizationID:  orgID,
		KnowledgeBaseID: kb.ID,
		Name:            name,
		SourceType:      sourceType,
		RawText:         text,
		Status:          models.DocumentStatusPending,
		CreatedByID:     &userID,
	}
	if err := a.DB.Create(&doc).Error; err != nil {
		a.Log.Error("Failed to create document", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to create document", nil, "")
	}

	a.startDocumentIngestion(doc.ID)

	return r.SendEnvelope(documentToResponse(doc))
}

// RetryDocument re-runs ingestion for a failed (or stuck) document.
func (a *App) RetryDocument(r *fastglue.Request) error {
	orgID, _, err := a.requireAuth(r, models.ResourceKnowledgeBase, models.ActionWrite)
	if err != nil {
		return nil
	}

	docID, err := parsePathUUID(r, "doc_id", "document")
	if err != nil {
		return nil
	}

	var doc models.Document
	if err := a.DB.Where("id = ? AND organization_id = ?", docID, orgID).First(&doc).Error; err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusNotFound, "Document not found", nil, "")
	}
	if doc.Status == models.DocumentStatusProcessing {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "Document is already being processed", nil, "")
	}

	a.DB.Model(&models.Document{}).Where("id = ?", doc.ID).Updates(map[string]any{
		"status":        models.DocumentStatusPending,
		"error_message": "",
	})

	a.startDocumentIngestion(doc.ID)

	return r.SendEnvelope(map[string]string{"message": "Retry started"})
}

// DeleteDocument deletes a document and its chunks, best-effort deleting the
// matching vectors from the external store too (a failure there is logged
// but doesn't block the delete — the DB records are the source of truth).
func (a *App) DeleteDocument(r *fastglue.Request) error {
	orgID, _, err := a.requireAuth(r, models.ResourceKnowledgeBase, models.ActionDelete)
	if err != nil {
		return nil
	}

	docID, err := parsePathUUID(r, "doc_id", "document")
	if err != nil {
		return nil
	}

	var doc models.Document
	if err := a.DB.Where("id = ? AND organization_id = ?", docID, orgID).First(&doc).Error; err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusNotFound, "Document not found", nil, "")
	}

	a.deleteDocumentVectors(doc)

	a.DB.Where("document_id = ?", doc.ID).Delete(&models.DocumentChunk{})
	if err := a.DB.Delete(&doc).Error; err != nil {
		a.Log.Error("Failed to delete document", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to delete document", nil, "")
	}

	return r.SendEnvelope(map[string]string{"message": "Document deleted successfully"})
}

// deleteDocumentVectors best-effort deletes a document's chunk vectors from
// its knowledge base's external store. Errors are logged, never returned —
// callers treat the DB as the source of truth and don't want a flaky
// external store to block a document delete/retry.
func (a *App) deleteDocumentVectors(doc models.Document) {
	var chunks []models.DocumentChunk
	if err := a.DB.Where("document_id = ?", doc.ID).Find(&chunks).Error; err != nil || len(chunks) == 0 {
		return
	}

	var kb models.KnowledgeBase
	if err := a.DB.Where("id = ?", doc.KnowledgeBaseID).First(&kb).Error; err != nil {
		return
	}
	var conn models.DataConnection
	if err := a.DB.Where("id = ?", kb.DataConnectionID).First(&conn).Error; err != nil {
		return
	}

	store, err := vectorstore.NewStore(conn, a.Config.App.EncryptionKey, a.HTTPClient)
	if err != nil {
		a.Log.Warn("Failed to build vector store for cleanup", "doc_id", doc.ID, "error", err)
		return
	}

	ids := make([]string, len(chunks))
	for i, c := range chunks {
		ids[i] = c.VectorRef
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := store.Delete(ctx, kb.CollectionName, ids); err != nil {
		a.Log.Warn("Failed to delete vectors for document", "doc_id", doc.ID, "error", err)
	}
}

// startDocumentIngestion runs the chunk → embed → upsert pipeline in the
// background, using the app's standard fire-and-forget goroutine pattern
// (tracked by a.wg, drained on graceful shutdown — see webhook_dispatch.go
// for the same pattern applied to webhook delivery).
func (a *App) startDocumentIngestion(docID uuid.UUID) {
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		a.runDocumentIngestion(ctx, docID)
	}()
}

func (a *App) runDocumentIngestion(ctx context.Context, docID uuid.UUID) {
	var doc models.Document
	if err := a.DB.Where("id = ?", docID).First(&doc).Error; err != nil {
		a.Log.Error("ingestion: document not found", "doc_id", docID, "error", err)
		return
	}

	fail := func(reason string) {
		a.Log.Error("document ingestion failed", "doc_id", doc.ID, "reason", reason)
		a.DB.Model(&models.Document{}).Where("id = ?", doc.ID).Updates(map[string]any{
			"status":        models.DocumentStatusFailed,
			"error_message": reason,
		})
	}

	a.DB.Model(&models.Document{}).Where("id = ?", doc.ID).Update("status", models.DocumentStatusProcessing)

	var kb models.KnowledgeBase
	if err := a.DB.Where("id = ?", doc.KnowledgeBaseID).First(&kb).Error; err != nil {
		fail("knowledge base not found")
		return
	}

	var conn models.DataConnection
	if err := a.DB.Where("id = ?", kb.DataConnectionID).First(&conn).Error; err != nil {
		fail("data connection not found")
		return
	}

	var org models.Organization
	if err := a.DB.Where("id = ?", doc.OrganizationID).First(&org).Error; err != nil {
		fail("organization not found")
		return
	}
	embKeyEnc, _ := org.Settings["openai_embeddings_key_encrypted"].(string)
	if embKeyEnc == "" {
		fail("OpenAI embeddings API key is not configured for this organization")
		return
	}
	embKey, err := crypto.Decrypt(embKeyEnc, a.Config.App.EncryptionKey)
	if err != nil {
		fail("failed to decrypt embeddings key")
		return
	}

	chunks := chunkText(doc.RawText, kb.ChunkSize, kb.ChunkOverlap)
	if len(chunks) == 0 {
		fail("document has no extractable text")
		return
	}

	store, err := vectorstore.NewStore(conn, a.Config.App.EncryptionKey, a.HTTPClient)
	if err != nil {
		fail(err.Error())
		return
	}
	if err := store.EnsureCollection(ctx, kb.CollectionName, kb.EmbeddingDims); err != nil {
		fail("failed to prepare vector store collection: " + err.Error())
		return
	}

	// Re-ingestion (retry): clear previous chunks/vectors first so we don't
	// end up with duplicate or orphaned vectors if the chunk count changed.
	a.deleteDocumentVectors(doc)
	a.DB.Where("document_id = ?", doc.ID).Delete(&models.DocumentChunk{})

	chunkRecords := make([]models.DocumentChunk, 0, len(chunks))
	for start := 0; start < len(chunks); start += openAIEmbeddingBatchSize {
		end := start + openAIEmbeddingBatchSize
		if end > len(chunks) {
			end = len(chunks)
		}
		batch := chunks[start:end]

		embeddings, err := a.generateOpenAIEmbeddings(embKey, batch, kb.EmbeddingDims)
		if err != nil {
			fail("embedding generation failed: " + err.Error())
			return
		}

		items := make([]vectorstore.UpsertItem, len(batch))
		batchRecords := make([]models.DocumentChunk, len(batch))
		for i, content := range batch {
			idx := start + i
			vecRef := uuid.New().String()
			items[i] = vectorstore.UpsertItem{
				ID:      vecRef,
				Vector:  embeddings[i],
				Content: content,
				Metadata: map[string]any{
					"document_id": doc.ID.String(),
					"chunk_index": idx,
				},
			}
			batchRecords[i] = models.DocumentChunk{
				DocumentID:      doc.ID,
				KnowledgeBaseID: kb.ID,
				ChunkIndex:      idx,
				Content:         content,
				VectorRef:       vecRef,
				TokenCount:      len(content) / 4, // rough estimate, not exact tokenization
			}
		}

		if err := store.Upsert(ctx, kb.CollectionName, items); err != nil {
			fail("failed to upsert vectors: " + err.Error())
			return
		}
		chunkRecords = append(chunkRecords, batchRecords...)
	}

	if err := a.DB.Create(&chunkRecords).Error; err != nil {
		fail(fmt.Sprintf("failed to save chunk records: %v", err))
		return
	}

	now := time.Now()
	a.DB.Model(&models.Document{}).Where("id = ?", doc.ID).Updates(map[string]any{
		"status":        models.DocumentStatusCompleted,
		"chunk_count":   len(chunks),
		"processed_at":  &now,
		"error_message": "",
	})
}
