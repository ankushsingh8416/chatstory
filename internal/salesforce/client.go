// Package salesforce is a thin client for the Max Estates WhatsApp Lead
// Capture API (a custom Apex REST endpoint on Salesforce, documented in the
// vendor's "WhatsApp Lead Capture API – Integration Guide"). It handles the
// OAuth2 client_credentials token exchange and the two lead-sync payloads.
package salesforce

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// Config holds one organization's Salesforce Connected App credentials and
// endpoints, as issued by Salesforce (see the integration guide).
type Config struct {
	ClientID     string
	ClientSecret string
	AuthURL      string // POST .../services/oauth2/token
	LeadAPIURL   string // POST .../services/apexrest/whatsapp/lead/v1/
}

// Client is safe for concurrent use; it caches the OAuth token in memory
// and refreshes it on expiry or a 401 from the Lead API.
type Client struct {
	cfg        Config
	httpClient *http.Client

	mu          sync.Mutex
	token       string
	tokenExpiry time.Time
}

func New(cfg Config) *Client {
	return &Client{cfg: cfg, httpClient: &http.Client{Timeout: 15 * time.Second}}
}

// MessageItem is one entry in a payload's "messages" array.
type MessageItem struct {
	MessageID string `json:"messageId"`
	Direction string `json:"direction"` // "Inbound" | "Outbound"
	Sender    string `json:"sender"`
	Body      string `json:"body"`
}

// FirstContactPayload creates or updates the Lead (matched by Mobile, then
// Email) and seeds its message history.
type FirstContactPayload struct {
	Name           string        `json:"name"`
	Mobile         string        `json:"mobile"`
	Email          string        `json:"email,omitempty"`
	Project        string        `json:"project,omitempty"`
	Message        string        `json:"message,omitempty"`
	ConversationID string        `json:"conversationId"`
	AgentName      string        `json:"agentName,omitempty"`
	UTMSource      string        `json:"utmSource,omitempty"`
	Messages       []MessageItem `json:"messages"`
}

// FollowUpPayload appends messages to an already-created Lead's history.
type FollowUpPayload struct {
	Mobile         string        `json:"mobile"`
	ConversationID string        `json:"conversationId"`
	Messages       []MessageItem `json:"messages"`
}

// LeadResponse is the shared response shape for both endpoints.
type LeadResponse struct {
	Success       bool     `json:"success"`
	Action        string   `json:"action"`
	LeadID        string   `json:"leadId"`
	MessagesSaved int      `json:"messagesSaved"`
	Message       string   `json:"message"`
	Warnings      []string `json:"warnings"`
}

// CreateOrUpdateLead sends the first-contact payload.
func (c *Client) CreateOrUpdateLead(ctx context.Context, p FirstContactPayload) (*LeadResponse, error) {
	return c.postLead(ctx, p)
}

// AppendMessages sends the follow-up payload for an existing Lead.
func (c *Client) AppendMessages(ctx context.Context, p FollowUpPayload) (*LeadResponse, error) {
	return c.postLead(ctx, p)
}

func (c *Client) postLead(ctx context.Context, payload any) (*LeadResponse, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	token, err := c.getToken(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("get token: %w", err)
	}

	resp, status, err := c.doLeadRequest(ctx, token, body)
	if err == nil && status == http.StatusUnauthorized {
		// Token may have expired server-side despite our cache — refetch once.
		token, err = c.getToken(ctx, true)
		if err != nil {
			return nil, fmt.Errorf("refresh token: %w", err)
		}
		resp, status, err = c.doLeadRequest(ctx, token, body)
	}
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("lead API returned status %d: %s", status, string(resp))
	}

	var out LeadResponse
	if err := json.Unmarshal(resp, &out); err != nil {
		return nil, fmt.Errorf("parse lead response: %w", err)
	}
	return &out, nil
}

func (c *Client) doLeadRequest(ctx context.Context, token string, body []byte) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.LeadAPIURL, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("read response: %w", err)
	}
	return respBody, resp.StatusCode, nil
}

// getToken returns the cached token unless it's near expiry or force is
// true, in which case it re-authenticates. Salesforce's sample response
// doesn't include expires_in, so a conservative fixed TTL is used and the
// caller falls back here again on any 401 from the Lead API.
func (c *Client) getToken(ctx context.Context, force bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !force && c.token != "" && time.Now().Before(c.tokenExpiry) {
		return c.token, nil
	}

	q := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {c.cfg.ClientID},
		"client_secret": {c.cfg.ClientSecret},
	}
	reqURL := c.cfg.AuthURL + "?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, nil)
	if err != nil {
		return "", fmt.Errorf("build token request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read token response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("token endpoint returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(respBody, &tok); err != nil {
		return "", fmt.Errorf("parse token response: %w", err)
	}
	if tok.AccessToken == "" {
		return "", fmt.Errorf("token response had no access_token: %s", string(respBody))
	}

	c.token = tok.AccessToken
	// ponytail: fixed 15-minute TTL (no expires_in in the vendor's response
	// to size this against); if the real token lifetime differs, the 401
	// retry in postLead still recovers correctness, just with one wasted
	// request.
	c.tokenExpiry = time.Now().Add(15 * time.Minute)
	return c.token, nil
}
