// Package razorpay is a minimal client for the one payment gateway this app
// integrates (Razorpay Payment Links) — deliberately not a multi-provider
// abstraction; add one only if/when a second gateway is actually needed.
package razorpay

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const baseURL = "https://api.razorpay.com/v1"

// Client talks to the Razorpay Payment Links API using an org's own
// key_id/key_secret (Basic Auth, Razorpay's standard scheme).
type Client struct {
	KeyID      string
	KeySecret  string
	HTTPClient *http.Client
}

// PaymentLink is the subset of Razorpay's payment_link entity this app uses.
type PaymentLink struct {
	ID       string `json:"id"`
	ShortURL string `json:"short_url"`
}

// CreatePaymentLink creates a Razorpay payment link for the given amount
// (in paise — Razorpay's smallest-unit convention, matching how Order.Subtotal
// is stored) and returns its ID and shareable URL.
func (c *Client) CreatePaymentLink(ctx context.Context, amountPaise int64, currency, customerPhone, referenceID, description string) (*PaymentLink, error) {
	body := map[string]any{
		"amount":       amountPaise,
		"currency":     currency,
		"description":  description,
		"reference_id": referenceID,
		"customer": map[string]any{
			"contact": customerPhone,
		},
		"notify": map[string]any{
			"sms":   false,
			"email": false,
		},
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payment link request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/payment_links", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.SetBasicAuth(c.KeyID, c.KeySecret)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("razorpay API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var link PaymentLink
	if err := json.Unmarshal(respBody, &link); err != nil {
		return nil, fmt.Errorf("failed to parse payment link response: %w", err)
	}
	return &link, nil
}

// VerifyWebhookSignature checks Razorpay's X-Razorpay-Signature header
// (HMAC-SHA256 of the raw request body, hex-encoded) against the org's
// configured webhook secret.
func VerifyWebhookSignature(rawBody []byte, signature, secret string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(rawBody)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

// WebhookPayload is the subset of Razorpay's webhook event body this app acts on.
type WebhookPayload struct {
	Event   string `json:"event"`
	Payload struct {
		PaymentLink struct {
			Entity struct {
				ID string `json:"id"`
			} `json:"entity"`
		} `json:"payment_link"`
		Payment struct {
			Entity struct {
				ID string `json:"id"`
			} `json:"entity"`
		} `json:"payment"`
	} `json:"payload"`
}
