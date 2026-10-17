package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/crypto"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/internal/razorpay"
	"github.com/valyala/fasthttp"
	"github.com/zerodha/fastglue"
	"gorm.io/gorm"
)

// --- Response shapes ---

// OrderItemResponse represents one order line item in API responses.
type OrderItemResponse struct {
	ID          uuid.UUID `json:"id"`
	RetailerID  string    `json:"retailer_id"`
	ProductName string    `json:"product_name"`
	Quantity    int       `json:"quantity"`
	UnitPrice   int64     `json:"unit_price"`
	Currency    string    `json:"currency"`
}

// OrderResponse represents an Order in API responses.
type OrderResponse struct {
	ID              uuid.UUID           `json:"id"`
	WhatsAppAccount string              `json:"whatsapp_account"`
	ContactID       uuid.UUID           `json:"contact_id"`
	ContactName     string              `json:"contact_name,omitempty"`
	ContactPhone    string              `json:"contact_phone,omitempty"`
	Status          models.OrderStatus  `json:"status"`
	PaymentMethod   string              `json:"payment_method"`
	PaymentStatus   string              `json:"payment_status"`
	Subtotal        int64               `json:"subtotal"`
	Currency        string              `json:"currency"`
	CustomerNote    string              `json:"customer_note"`
	TrackingCarrier string              `json:"tracking_carrier"`
	TrackingNumber  string              `json:"tracking_number"`
	TrackingURL     string              `json:"tracking_url"`
	PaidAt          *time.Time          `json:"paid_at,omitempty"`
	Items           []OrderItemResponse `json:"items,omitempty"`
	CreatedAt       string              `json:"created_at"`
	UpdatedAt       string              `json:"updated_at"`
}

func orderToResponse(o models.Order) OrderResponse {
	resp := OrderResponse{
		ID:              o.ID,
		WhatsAppAccount: o.WhatsAppAccount,
		ContactID:       o.ContactID,
		Status:          o.Status,
		PaymentMethod:   o.PaymentMethod,
		PaymentStatus:   o.PaymentStatus,
		Subtotal:        o.Subtotal,
		Currency:        o.Currency,
		CustomerNote:    o.CustomerNote,
		TrackingCarrier: o.TrackingCarrier,
		TrackingNumber:  o.TrackingNumber,
		TrackingURL:     o.TrackingURL,
		PaidAt:          o.PaidAt,
		CreatedAt:       o.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:       o.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
	if o.Contact != nil {
		resp.ContactName = o.Contact.ProfileName
		resp.ContactPhone = o.Contact.PhoneNumber
	}
	if len(o.Items) > 0 {
		resp.Items = make([]OrderItemResponse, len(o.Items))
		for i, it := range o.Items {
			resp.Items[i] = OrderItemResponse{
				ID:          it.ID,
				RetailerID:  it.RetailerID,
				ProductName: it.ProductName,
				Quantity:    it.Quantity,
				UnitPrice:   it.UnitPrice,
				Currency:    it.Currency,
			}
		}
	}
	return resp
}

// --- Order capture from WhatsApp ---

// handleIncomingOrder captures a customer order submitted from a WhatsApp
// catalog cart. Orders are idempotent on SourceMessageID (the order
// message's WAMID) so a Meta webhook retry can't create duplicates.
func (a *App) handleIncomingOrder(account *models.WhatsAppAccount, contact *models.Contact, msg IncomingTextMessage) {
	if msg.Order == nil {
		return
	}

	var existing models.Order
	if err := a.DB.Where("source_message_id = ?", msg.ID).First(&existing).Error; err == nil {
		a.Log.Info("Duplicate order webhook, ignoring", "source_message_id", msg.ID)
		return
	}

	var catalogID *uuid.UUID
	var catalog models.Catalog
	if err := a.DB.Where("organization_id = ? AND meta_catalog_id = ?", account.OrganizationID, msg.Order.CatalogID).First(&catalog).Error; err == nil {
		catalogID = &catalog.ID
	}

	var subtotal int64
	items := make([]models.OrderItem, 0, len(msg.Order.ProductItems))
	currency := "INR"
	for _, pi := range msg.Order.ProductItems {
		if pi.Currency != "" {
			currency = pi.Currency
		}
		unitPrice := int64(pi.ItemPrice * 100) // Meta sends a decimal amount; store in paise/cents like everything else
		productName := pi.ProductRetailerID

		var product models.CatalogProduct
		var catalogProductID *uuid.UUID
		if err := a.DB.Where("organization_id = ? AND retailer_id = ?", account.OrganizationID, pi.ProductRetailerID).First(&product).Error; err == nil {
			catalogProductID = &product.ID
			productName = product.Name
		}

		items = append(items, models.OrderItem{
			CatalogProductID: catalogProductID,
			RetailerID:       pi.ProductRetailerID,
			ProductName:      productName,
			Quantity:         pi.Quantity,
			UnitPrice:        unitPrice,
			Currency:         pi.Currency,
		})
		subtotal += unitPrice * int64(pi.Quantity)
	}

	order := models.Order{
		OrganizationID:  account.OrganizationID,
		WhatsAppAccount: account.Name,
		ContactID:       contact.ID,
		CatalogID:       catalogID,
		SourceMessageID: msg.ID,
		Status:          models.OrderStatusNew,
		Subtotal:        subtotal,
		Currency:        currency,
		CustomerNote:    msg.Order.Text,
	}

	err := a.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&order).Error; err != nil {
			return err
		}
		for i := range items {
			items[i].OrderID = order.ID
		}
		return tx.Create(&items).Error
	})
	if err != nil {
		a.Log.Error("Failed to save order", "error", err, "source_message_id", msg.ID)
		return
	}

	summary := fmt.Sprintf("\U0001F6D2 Order: %d item(s) — %s", len(items), formatMinorAmount(subtotal, currency))
	a.saveIncomingMessage(account, contact, msg.ID, string(models.MessageTypeOrder), summary, nil, "")

	// Ask how they'd like to pay.
	buttons := []map[string]any{
		{"id": fmt.Sprintf("order_pay:%s", order.ID), "title": "Pay Online"},
	}
	if a.orgCODEnabled(account.OrganizationID) {
		buttons = append(buttons, map[string]any{"id": fmt.Sprintf("order_cod:%s", order.ID), "title": "Cash on Delivery"})
	}
	if err := a.sendAndSaveInteractiveButtons(account, contact, "Thanks for your order! How would you like to pay?", buttons); err != nil {
		a.Log.Error("Failed to send order payment-method prompt", "error", err, "order_id", order.ID)
	}
}

// handleOrderPaymentChoice routes the customer's reply to the "Pay Online" /
// "Cash on Delivery" buttons sent by handleIncomingOrder.
func (a *App) handleOrderPaymentChoice(account *models.WhatsAppAccount, contact *models.Contact, buttonID string) {
	isCOD := len(buttonID) > len("order_cod:") && buttonID[:len("order_cod:")] == "order_cod:"
	var orderIDStr string
	if isCOD {
		orderIDStr = buttonID[len("order_cod:"):]
	} else {
		orderIDStr = buttonID[len("order_pay:"):]
	}
	orderID, err := uuid.Parse(orderIDStr)
	if err != nil {
		return
	}

	var order models.Order
	if err := a.DB.Where("id = ? AND contact_id = ?", orderID, contact.ID).First(&order).Error; err != nil {
		a.Log.Warn("Order payment choice for unknown order", "order_id", orderIDStr, "contact_id", contact.ID)
		return
	}
	if order.Status != models.OrderStatusNew {
		return // already handled (duplicate button tap)
	}

	if isCOD {
		a.DB.Model(&order).Updates(map[string]any{"payment_method": "cod", "status": models.OrderStatusConfirmed})
		_ = a.sendAndSaveTextMessage(account, contact, "Your order is confirmed for Cash on Delivery. We'll notify you when it ships!")
		return
	}

	keyID, keySecret, _, err := a.orgRazorpayConfig(account.OrganizationID)
	if err != nil || keyID == "" {
		a.Log.Warn("Razorpay not configured for org", "org_id", account.OrganizationID, "error", err)
		_ = a.sendAndSaveTextMessage(account, contact, "Online payment isn't set up yet — please contact us to complete your order.")
		return
	}

	client := &razorpay.Client{KeyID: keyID, KeySecret: keySecret, HTTPClient: a.HTTPClient}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	link, err := client.CreatePaymentLink(ctx, order.Subtotal, order.Currency, contact.PhoneNumber, order.ID.String(), fmt.Sprintf("Order %s", order.ID.String()[:8]))
	if err != nil {
		a.Log.Error("Failed to create Razorpay payment link", "error", err, "order_id", order.ID)
		_ = a.sendAndSaveTextMessage(account, contact, "Sorry, we couldn't generate a payment link right now. Please try again shortly.")
		return
	}

	a.DB.Model(&order).Updates(map[string]any{
		"payment_method":           "online",
		"status":                   models.OrderStatusPaymentPending,
		"razorpay_payment_link_id": link.ID,
	})
	_ = a.sendAndSaveTextMessage(account, contact, fmt.Sprintf("Please complete your payment here: %s", link.ShortURL))
}

// --- Commerce settings (Organization.Settings JSONB, same convention as
// openai_embeddings_key_encrypted) ---

func (a *App) orgRazorpayConfig(orgID uuid.UUID) (keyID, keySecret, webhookSecret string, err error) {
	var org models.Organization
	if err := a.DB.Where("id = ?", orgID).First(&org).Error; err != nil {
		return "", "", "", err
	}
	keyID, _ = org.Settings["razorpay_key_id"].(string)
	secretEnc, _ := org.Settings["razorpay_key_secret_encrypted"].(string)
	webhookEnc, _ := org.Settings["razorpay_webhook_secret_encrypted"].(string)
	if secretEnc != "" {
		keySecret, _ = crypto.Decrypt(secretEnc, a.Config.App.EncryptionKey)
	}
	if webhookEnc != "" {
		webhookSecret, _ = crypto.Decrypt(webhookEnc, a.Config.App.EncryptionKey)
	}
	return keyID, keySecret, webhookSecret, nil
}

func (a *App) orgCODEnabled(orgID uuid.UUID) bool {
	var org models.Organization
	if err := a.DB.Where("id = ?", orgID).First(&org).Error; err != nil {
		return false
	}
	enabled, _ := org.Settings["cod_enabled"].(bool)
	return enabled
}

func formatMinorAmount(amountMinor int64, currency string) string {
	return fmt.Sprintf("%s %.2f", currency, float64(amountMinor)/100)
}

// --- Order management API ---

// ListOrders returns orders for the organization, optionally filtered by status or contact.
func (a *App) ListOrders(r *fastglue.Request) error {
	orgID, _, err := a.requireAuth(r, models.ResourceOrders, models.ActionRead)
	if err != nil {
		return nil
	}

	status := string(r.RequestCtx.QueryArgs().Peek("status"))
	contactIDStr := string(r.RequestCtx.QueryArgs().Peek("contact_id"))

	query := a.DB.Where("organization_id = ?", orgID).Preload("Contact")
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if contactIDStr != "" {
		if contactID, err := uuid.Parse(contactIDStr); err == nil {
			query = query.Where("contact_id = ?", contactID)
		}
	}

	var orders []models.Order
	if err := query.Order("created_at DESC").Find(&orders).Error; err != nil {
		a.Log.Error("Failed to list orders", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to list orders", nil, "")
	}

	result := make([]OrderResponse, len(orders))
	for i, o := range orders {
		result[i] = orderToResponse(o)
	}
	return r.SendEnvelope(map[string]any{"orders": result})
}

// GetOrder returns a single order with its items.
func (a *App) GetOrder(r *fastglue.Request) error {
	orgID, _, err := a.requireAuth(r, models.ResourceOrders, models.ActionRead)
	if err != nil {
		return nil
	}

	id, err := parsePathUUID(r, "id", "order")
	if err != nil {
		return nil
	}

	var order models.Order
	if err := a.DB.Where("id = ? AND organization_id = ?", id, orgID).
		Preload("Contact").Preload("Items").First(&order).Error; err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusNotFound, "Order not found", nil, "")
	}

	return r.SendEnvelope(orderToResponse(order))
}

// UpdateOrderStatusRequest is the request body for changing an order's status/tracking.
type UpdateOrderStatusRequest struct {
	Status          models.OrderStatus `json:"status"`
	TrackingCarrier string             `json:"tracking_carrier"`
	TrackingNumber  string             `json:"tracking_number"`
	TrackingURL     string             `json:"tracking_url"`
}

var validOrderStatuses = map[models.OrderStatus]bool{
	models.OrderStatusNew:            true,
	models.OrderStatusPaymentPending: true,
	models.OrderStatusConfirmed:      true,
	models.OrderStatusShipped:        true,
	models.OrderStatusDelivered:      true,
	models.OrderStatusCancelled:      true,
}

// UpdateOrderStatus changes an order's status and/or tracking details. When
// the status is set to "shipped", the customer is notified on WhatsApp with
// the tracking info.
func (a *App) UpdateOrderStatus(r *fastglue.Request) error {
	orgID, userID, err := a.requireAuth(r, models.ResourceOrders, models.ActionWrite)
	if err != nil {
		return nil
	}

	id, err := parsePathUUID(r, "id", "order")
	if err != nil {
		return nil
	}

	var order models.Order
	if err := a.DB.Where("id = ? AND organization_id = ?", id, orgID).First(&order).Error; err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusNotFound, "Order not found", nil, "")
	}

	var req UpdateOrderStatusRequest
	if err := a.decodeRequest(r, &req); err != nil {
		return nil
	}

	if req.Status != "" {
		if !validOrderStatuses[req.Status] {
			return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "Invalid status", nil, "")
		}
		order.Status = req.Status
	}
	if req.TrackingCarrier != "" {
		order.TrackingCarrier = req.TrackingCarrier
	}
	if req.TrackingNumber != "" {
		order.TrackingNumber = req.TrackingNumber
	}
	if req.TrackingURL != "" {
		order.TrackingURL = req.TrackingURL
	}
	order.CreatedByID = &userID

	if err := a.DB.Save(&order).Error; err != nil {
		a.Log.Error("Failed to update order", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to update order", nil, "")
	}

	if req.Status == models.OrderStatusShipped {
		a.notifyOrderShipped(order)
	}

	return r.SendEnvelope(orderToResponse(order))
}

// notifyOrderShipped sends the customer a WhatsApp message with tracking
// info, following the same fire-and-forget background-send pattern used
// elsewhere (see sla_processor.go's sendChatbotReminder).
func (a *App) notifyOrderShipped(order models.Order) {
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		var contact models.Contact
		if err := a.DB.Where("id = ?", order.ContactID).First(&contact).Error; err != nil {
			return
		}
		account, err := a.resolveWhatsAppAccount(order.OrganizationID, order.WhatsAppAccount)
		if err != nil {
			return
		}
		msg := "Your order has shipped!"
		if order.TrackingNumber != "" {
			msg += fmt.Sprintf(" Tracking number: %s", order.TrackingNumber)
		}
		if order.TrackingURL != "" {
			msg += fmt.Sprintf("\nTrack here: %s", order.TrackingURL)
		}
		if err := a.sendAndSaveTextMessage(account, &contact, msg); err != nil {
			a.Log.Error("Failed to send shipped notification", "error", err, "order_id", order.ID)
		}
	}()
}

// ListContactOrders returns a contact's order history for the contact panel.
func (a *App) ListContactOrders(r *fastglue.Request) error {
	orgID, _, err := a.requireAuth(r, models.ResourceOrders, models.ActionRead)
	if err != nil {
		return nil
	}

	contactID, err := parsePathUUID(r, "id", "contact")
	if err != nil {
		return nil
	}

	var orders []models.Order
	if err := a.DB.Where("organization_id = ? AND contact_id = ?", orgID, contactID).
		Preload("Items").Order("created_at DESC").Find(&orders).Error; err != nil {
		a.Log.Error("Failed to list contact orders", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusInternalServerError, "Failed to list orders", nil, "")
	}

	result := make([]OrderResponse, len(orders))
	for i, o := range orders {
		result[i] = orderToResponse(o)
	}
	return r.SendEnvelope(map[string]any{"orders": result})
}

// --- Razorpay webhook ---

// RazorpayWebhook handles Razorpay's payment_link.paid event. Public route —
// the org can't be known until the payload is parsed, so the signature is
// verified against the org owning the referenced payment link, not globally.
func (a *App) RazorpayWebhook(r *fastglue.Request) error {
	body := r.RequestCtx.PostBody()
	signature := string(r.RequestCtx.Request.Header.Peek("X-Razorpay-Signature"))

	var payload razorpay.WebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		a.Log.Error("Failed to parse Razorpay webhook payload", "error", err)
		return r.SendErrorEnvelope(fasthttp.StatusBadRequest, "Invalid payload", nil, "")
	}

	linkID := payload.Payload.PaymentLink.Entity.ID
	if linkID == "" {
		return r.SendEnvelope(map[string]string{"status": "ignored"})
	}

	var order models.Order
	if err := a.DB.Where("razorpay_payment_link_id = ?", linkID).First(&order).Error; err != nil {
		a.Log.Warn("Razorpay webhook for unknown payment link", "link_id", linkID)
		return r.SendEnvelope(map[string]string{"status": "ignored"})
	}

	_, _, webhookSecret, err := a.orgRazorpayConfig(order.OrganizationID)
	if err != nil || webhookSecret == "" || !razorpay.VerifyWebhookSignature(body, signature, webhookSecret) {
		a.Log.Warn("Razorpay webhook signature verification failed", "order_id", order.ID)
		return r.SendErrorEnvelope(fasthttp.StatusUnauthorized, "Invalid signature", nil, "")
	}

	if payload.Event != "payment_link.paid" {
		return r.SendEnvelope(map[string]string{"status": "ignored"})
	}
	if order.PaymentStatus == "paid" {
		return r.SendEnvelope(map[string]string{"status": "already_processed"})
	}

	now := time.Now()
	a.DB.Model(&order).Updates(map[string]any{
		"payment_status":      "paid",
		"status":              models.OrderStatusConfirmed,
		"paid_at":             &now,
		"razorpay_payment_id": payload.Payload.Payment.Entity.ID,
	})

	var contact models.Contact
	if err := a.DB.Where("id = ?", order.ContactID).First(&contact).Error; err == nil {
		if account, err := a.resolveWhatsAppAccount(order.OrganizationID, order.WhatsAppAccount); err == nil {
			_ = a.sendAndSaveTextMessage(account, &contact, "Payment received — your order is confirmed! We'll notify you when it ships.")
		}
	}

	return r.SendEnvelope(map[string]string{"status": "ok"})
}
