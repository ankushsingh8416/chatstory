package handlers

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/crypto"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/internal/vectorstore"
	"github.com/valyala/fasthttp"
	"github.com/zerodha/fastglue"
)

// validateDataConnectionHost blocks obviously-internal hostnames/IP literals
// for a bare host:port pair (the Postgres connection fields don't carry a
// full URL, unlike the Qdrant URL, which reuses validateWebhookURL from
// webhooks.go instead). This is structural, input-time validation only —
// runtime SSRF protection (DNS rebinding included) is provided by
// netutil.SSRFSafeDialer at actual connect time, in internal/vectorstore.
func validateDataConnectionHost(host string) error {
	if host == "" {
		return fmt.Errorf("host is required")
	}
	lower := strings.ToLower(host)
	if lower == "localhost" || lower == "0.0.0.0" || strings.HasSuffix(lower, ".local") || strings.HasSuffix(lower, ".internal") {
		return fmt.Errorf("host must not point to an internal address")
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return fmt.Errorf("host must not point to an internal address")
		}
	}
	return nil
}

// DataConnectionRequest represents the request body for creating/editing a data connection.
type DataConnectionRequest struct {
	Name     string                    `json:"name"`
	Type     models.DataConnectionType `json:"type"`
	Host     string                    `json:"host"`
	Port     int                       `json:"port"`
	Database string                    `json:"database"`
	Username string                    `json:"username"`
	Password string                    `json:"password"` // empty on update = leave unchanged
	SSLMode  string                    `json:"ssl_mode"`

	QdrantURL    string `json:"qdrant_url"`
	QdrantAPIKey string `json:"qdrant_api_key"` // empty on update = leave unchanged
}

// DataConnectionResponse represents a data connection in API responses. Secret
// fields are never included — HasPassword/HasQdrantKey tell the UI whether a
// credential is set without ever exposing it.
type DataConnectionResponse struct {
	ID       uuid.UUID                 `json:"id"`
	Name     string                    `json:"name"`
	Type     models.DataConnectionType `json:"type"`
	Host     string                    `json:"host,omitempty"`
	Port     int                       `json:"port,omitempty"`
	Database string                    `json:"database,omitempty"`
	Username string                    `json:"username,omitempty"`
	SSLMode  string                    `json:"ssl_mode,omitempty"`

	QdrantURL string `json:"qdrant_url,omitempty"`

	HasPassword  bool `json:"has_password"`
	HasQdrantKey bool `json:"has_qdrant_key"`

	Status        models.DataConnectionStatus `json:"status"`
	LastTestedAt  *time.Time                  `json:"last_tested_at,omitempty"`
	LastTestError string                      `json:"last_test_error,omitempty"`

	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func dataConnectionToResponse(c models.DataConnection) DataConnectionResponse {
	return DataConnectionResponse{
		ID:            c.ID,
		Name:          c.Name,
		Type:          c.Type,
		Host:          c.Host,
		Port:          c.Port,
		Database:      c.Database,
		Username:      c.Username,
		SSLMode:       c.SSLMode,
		QdrantURL:     c.QdrantURL,
		HasPassword:   c.Password != "",
		HasQdrantKey:  c.QdrantAPIKey != "",
		Status:        c.Status,
		LastTestedAt:  c.LastTestedAt,
		LastTestError: c.LastTestError,
		CreatedAt:     c.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:     c.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
}

// ListDataConnections returns all data connections for the organization.
func (a *App) ListDataConnections(r *fastglue.Request) error {
	orgID, _, err := a.requireAuth(r, models.ResourceDataConnections, models.ActionRead)
	if err != nil {
		return nil
	}

	var conns []models.DataConnection
	if err := a.DB.Where("organization_id = ?", orgID).Order("name ASC").Find(&conns).Error; err != nil {
		a.Log.Error("Failed to list data connections", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to list data connections", nil, "")
	}

	response := make([]DataConnectionResponse, len(conns))
	for i, c := range conns {
		response[i] = dataConnectionToResponse(c)
	}

	return r.SendEnvelope(map[string]any{"data_connections": response})
}

// validateDataConnectionRequest checks the fields required for the request's
// Type are present. requirePassword controls whether Password/QdrantAPIKey
// are required (true on create; false on update, where empty means "keep
// the existing credential").
func validateDataConnectionRequest(req DataConnectionRequest, requirePassword bool) error {
	if req.Name == "" {
		return fmt.Errorf("name is required")
	}
	switch req.Type {
	case models.DataConnectionTypePostgres:
		if err := validateDataConnectionHost(req.Host); err != nil {
			return err
		}
		if req.Database == "" {
			return fmt.Errorf("database is required")
		}
		if req.Username == "" {
			return fmt.Errorf("username is required")
		}
		if requirePassword && req.Password == "" {
			return fmt.Errorf("password is required")
		}
	case models.DataConnectionTypeQdrant:
		if err := validateWebhookURL(req.QdrantURL); err != nil {
			return err
		}
		// QdrantAPIKey is optional — some self-hosted instances run without auth.
	default:
		return fmt.Errorf("type must be either \"postgres\" or \"qdrant\"")
	}
	return nil
}

// CreateDataConnection creates a new data connection for the organization.
func (a *App) CreateDataConnection(r *fastglue.Request) error {
	orgID, userID, err := a.requireAuth(r, models.ResourceDataConnections, models.ActionWrite)
	if err != nil {
		return nil
	}

	var req DataConnectionRequest
	if err := a.decodeRequest(r, &req); err != nil {
		return nil
	}

	if err := validateDataConnectionRequest(req, true); err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, err.Error(), nil, "")
	}

	conn := models.DataConnection{
		OrganizationID: orgID,
		Name:           req.Name,
		Type:           req.Type,
		Host:           req.Host,
		Port:           req.Port,
		Database:       req.Database,
		Username:       req.Username,
		Password:       req.Password,
		SSLMode:        req.SSLMode,
		QdrantURL:      req.QdrantURL,
		QdrantAPIKey:   req.QdrantAPIKey,
		Status:         models.DataConnectionStatusUntested,
		CreatedByID:    &userID,
		UpdatedByID:    &userID,
	}

	if err := crypto.EncryptFields(a.Config.App.EncryptionKey, &conn.Password, &conn.QdrantAPIKey); err != nil {
		a.Log.Error("Failed to encrypt data connection credentials", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to create data connection", nil, "")
	}

	if err := a.DB.Create(&conn).Error; err != nil {
		a.Log.Error("Failed to create data connection", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to create data connection", nil, "")
	}

	a.logAudit(orgID, userID, models.ResourceDataConnections, conn.ID, models.AuditActionCreated,
		nil, map[string]any{"name": conn.Name, "type": conn.Type})

	return r.SendEnvelope(dataConnectionToResponse(conn))
}

// UpdateDataConnection edits an existing data connection. Empty
// password/qdrant_api_key fields leave the stored credential unchanged.
func (a *App) UpdateDataConnection(r *fastglue.Request) error {
	orgID, userID, err := a.requireAuth(r, models.ResourceDataConnections, models.ActionWrite)
	if err != nil {
		return nil
	}

	id, err := parsePathUUID(r, "id", "data connection")
	if err != nil {
		return nil
	}

	var conn models.DataConnection
	if err := a.DB.Where("id = ? AND organization_id = ?", id, orgID).First(&conn).Error; err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusNotFound, "Data connection not found", nil, "")
	}

	var req DataConnectionRequest
	if err := a.decodeRequest(r, &req); err != nil {
		return nil
	}

	if err := validateDataConnectionRequest(req, false); err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, err.Error(), nil, "")
	}

	oldName := conn.Name
	conn.Name = req.Name
	conn.Type = req.Type
	conn.Host = req.Host
	conn.Port = req.Port
	conn.Database = req.Database
	conn.Username = req.Username
	conn.SSLMode = req.SSLMode
	conn.QdrantURL = req.QdrantURL
	conn.UpdatedByID = &userID

	if req.Password != "" {
		enc, err := crypto.Encrypt(req.Password, a.Config.App.EncryptionKey)
		if err != nil {
			a.Log.Error("Failed to encrypt password", "error", err)
			return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to update data connection", nil, "")
		}
		conn.Password = enc
	}
	if req.QdrantAPIKey != "" {
		enc, err := crypto.Encrypt(req.QdrantAPIKey, a.Config.App.EncryptionKey)
		if err != nil {
			a.Log.Error("Failed to encrypt Qdrant API key", "error", err)
			return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to update data connection", nil, "")
		}
		conn.QdrantAPIKey = enc
	}

	// Credentials or endpoint changed — the last test result no longer applies.
	conn.Status = models.DataConnectionStatusUntested
	conn.LastTestedAt = nil
	conn.LastTestError = ""

	if err := a.DB.Save(&conn).Error; err != nil {
		a.Log.Error("Failed to update data connection", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to update data connection", nil, "")
	}

	a.logAudit(orgID, userID, models.ResourceDataConnections, conn.ID, models.AuditActionUpdated,
		map[string]any{"name": oldName}, map[string]any{"name": conn.Name})

	return r.SendEnvelope(dataConnectionToResponse(conn))
}

// DeleteDataConnection deletes a data connection.
func (a *App) DeleteDataConnection(r *fastglue.Request) error {
	orgID, userID, err := a.requireAuth(r, models.ResourceDataConnections, models.ActionDelete)
	if err != nil {
		return nil
	}

	id, err := parsePathUUID(r, "id", "data connection")
	if err != nil {
		return nil
	}

	var conn models.DataConnection
	if err := a.DB.Where("id = ? AND organization_id = ?", id, orgID).First(&conn).Error; err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusNotFound, "Data connection not found", nil, "")
	}

	if err := a.DB.Delete(&conn).Error; err != nil {
		a.Log.Error("Failed to delete data connection", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to delete data connection", nil, "")
	}

	a.logAudit(orgID, userID, models.ResourceDataConnections, conn.ID, models.AuditActionDeleted,
		map[string]any{"name": conn.Name}, nil)

	return r.SendEnvelope(map[string]string{"message": "Data connection deleted successfully"})
}

// ListDataConnectionTables introspects an org's own connected Postgres/
// Supabase database (read-only) and returns its tables/columns, flagging
// which already look like a usable vector store (a pgvector column with a
// readable dimension) - so the Knowledge Base UI can offer "point at my
// existing table" instead of requiring the org to hand-type exact
// table/column names for a schema only they know.
func (a *App) ListDataConnectionTables(r *fastglue.Request) error {
	orgID, _, err := a.requireAuth(r, models.ResourceDataConnections, models.ActionRead)
	if err != nil {
		return nil
	}

	id, err := parsePathUUID(r, "id", "data connection")
	if err != nil {
		return nil
	}

	var conn models.DataConnection
	if err := a.DB.Where("id = ? AND organization_id = ?", id, orgID).First(&conn).Error; err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusNotFound, "Data connection not found", nil, "")
	}
	if conn.Type != models.DataConnectionTypePostgres {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "Table introspection is only available for Postgres/Supabase connections", nil, "")
	}

	tables, err := vectorstore.ListPostgresTables(conn, a.Config.App.EncryptionKey)
	if err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, err.Error(), nil, "")
	}

	return r.SendEnvelope(map[string]any{"tables": tables})
}

// TestDataConnection attempts to connect to the external store with the
// stored (decrypted) credentials and records the result.
func (a *App) TestDataConnection(r *fastglue.Request) error {
	orgID, userID, err := a.requireAuth(r, models.ResourceDataConnections, models.ActionExecute)
	if err != nil {
		return nil
	}

	id, err := parsePathUUID(r, "id", "data connection")
	if err != nil {
		return nil
	}

	var conn models.DataConnection
	if err := a.DB.Where("id = ? AND organization_id = ?", id, orgID).First(&conn).Error; err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusNotFound, "Data connection not found", nil, "")
	}

	store, err := vectorstore.NewStore(conn, a.Config.App.EncryptionKey, a.HTTPClient)
	if err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, err.Error(), nil, "")
	}

	ctx, cancel := context.WithTimeout(r.RequestCtx, 15*time.Second)
	defer cancel()

	testErr := store.Ping(ctx)

	now := time.Now()
	conn.LastTestedAt = &now
	if testErr != nil {
		conn.Status = models.DataConnectionStatusFailed
		conn.LastTestError = testErr.Error()
	} else {
		conn.Status = models.DataConnectionStatusOK
		conn.LastTestError = ""
	}

	if err := a.DB.Model(&models.DataConnection{}).Where("id = ?", conn.ID).Updates(map[string]any{
		"status":          conn.Status,
		"last_tested_at":  conn.LastTestedAt,
		"last_test_error": conn.LastTestError,
	}).Error; err != nil {
		a.Log.Error("Failed to save connection test result", "error", err)
	}

	a.logAudit(orgID, userID, models.ResourceDataConnections, conn.ID, models.AuditActionUpdated,
		nil, map[string]any{"action": "test", "status": conn.Status})

	if testErr != nil {
		return r.SendEnvelope(map[string]any{
			"status":  conn.Status,
			"message": testErr.Error(),
		})
	}
	return r.SendEnvelope(map[string]any{
		"status":  conn.Status,
		"message": "Connection successful",
	})
}
