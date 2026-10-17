package razorpay

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVerifyWebhookSignature(t *testing.T) {
	secret := "test_webhook_secret"
	body := []byte(`{"event":"payment_link.paid","payload":{"payment_link":{"entity":{"id":"plink_123"}}}}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	validSignature := hex.EncodeToString(mac.Sum(nil))

	tests := []struct {
		name      string
		body      []byte
		signature string
		secret    string
		want      bool
	}{
		{"valid signature", body, validSignature, secret, true},
		{"wrong secret", body, validSignature, "wrong_secret", false},
		{"tampered body", []byte(`{"event":"tampered"}`), validSignature, secret, false},
		{"empty signature", body, "", secret, false},
		{"garbage signature", body, "not-a-valid-hex-signature", secret, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, VerifyWebhookSignature(tt.body, tt.signature, tt.secret))
		})
	}
}
