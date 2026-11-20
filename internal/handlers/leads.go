package handlers

import (
	"bytes"
	"encoding/csv"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/valyala/fasthttp"
	"github.com/zerodha/fastglue"
	"gorm.io/gorm"
)

// LeadResponse is one captured lead, with the contact's phone joined in.
type LeadResponse struct {
	ID        uuid.UUID `json:"id"`
	ContactID uuid.UUID `json:"contact_id"`
	Name      string    `json:"name"`
	Mobile    string    `json:"mobile"`
	Email     string    `json:"email"`
	Project   string    `json:"project"`
	Message   string    `json:"message"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"created_at"`
}

func (a *App) leadsQuery(r *fastglue.Request, orgID uuid.UUID) *gorm.DB {
	q := a.DB.Where("organization_id = ?", orgID)
	if v := string(r.RequestCtx.QueryArgs().Peek("source")); v != "" {
		q = q.Where("source = ?", v)
	}
	return q
}

// ListLeads returns every captured lead (any flow's form submission, or a
// free-text phone-number pickup) for the org, newest first.
func (a *App) ListLeads(r *fastglue.Request) error {
	orgID, _, err := a.requireAuth(r, models.ResourceAccounts, models.ActionRead)
	if err != nil {
		return nil
	}

	pg := parsePagination(r)

	var total int64
	a.leadsQuery(r, orgID).Model(&models.Lead{}).Count(&total)

	var leads []models.Lead
	if err := pg.Apply(a.leadsQuery(r, orgID).Order("created_at DESC")).Find(&leads).Error; err != nil {
		a.Log.Error("Failed to list leads", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to list leads", nil, "")
	}

	response := make([]LeadResponse, len(leads))
	for i, l := range leads {
		response[i] = leadToResponse(l)
	}
	return r.SendEnvelope(listEnvelope("leads", response, total, pg))
}

// ExportLeads streams every captured lead for the org as a CSV download.
func (a *App) ExportLeads(r *fastglue.Request) error {
	orgID, _, err := a.requireAuth(r, models.ResourceAccounts, models.ActionRead)
	if err != nil {
		return nil
	}

	var leads []models.Lead
	if err := a.leadsQuery(r, orgID).Order("created_at DESC").Find(&leads).Error; err != nil {
		a.Log.Error("Failed to export leads", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to export leads", nil, "")
	}

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"Name", "Mobile", "Email", "Project", "Message", "Source", "Captured At"})
	for _, l := range leads {
		_ = w.Write([]string{
			l.Name, l.Mobile, l.Email, l.Project, l.Message, l.Source,
			l.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	w.Flush()

	r.RequestCtx.Response.Header.Set("Content-Type", "text/csv")
	r.RequestCtx.Response.Header.Set("Content-Disposition", `attachment; filename="leads-`+strconv.FormatInt(time.Now().Unix(), 10)+`.csv"`)
	r.RequestCtx.SetBody(buf.Bytes())
	return nil
}

func leadToResponse(l models.Lead) LeadResponse {
	return LeadResponse{
		ID:        l.ID,
		ContactID: l.ContactID,
		Name:      l.Name,
		Mobile:    l.Mobile,
		Email:     l.Email,
		Project:   l.Project,
		Message:   l.Message,
		Source:    l.Source,
		CreatedAt: l.CreatedAt,
	}
}
