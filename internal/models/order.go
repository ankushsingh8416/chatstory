package models

import (
	"time"

	"github.com/google/uuid"
)

// OrderStatus tracks an Order through fulfillment.
type OrderStatus string

const (
	OrderStatusNew            OrderStatus = "new"             // captured, payment method not yet chosen
	OrderStatusPaymentPending OrderStatus = "payment_pending" // Razorpay link sent, awaiting payment
	OrderStatusConfirmed      OrderStatus = "confirmed"       // paid, or COD accepted
	OrderStatusShipped        OrderStatus = "shipped"
	OrderStatusDelivered      OrderStatus = "delivered"
	OrderStatusCancelled      OrderStatus = "cancelled"
)

// Order is a customer order captured from a WhatsApp catalog "order" message.
type Order struct {
	BaseModel
	OrganizationID  uuid.UUID  `gorm:"type:uuid;index;not null" json:"organization_id"`
	WhatsAppAccount string     `gorm:"size:100;index" json:"whatsapp_account"`
	ContactID       uuid.UUID  `gorm:"type:uuid;index;not null" json:"contact_id"`
	CatalogID       *uuid.UUID `gorm:"type:uuid;index" json:"catalog_id,omitempty"`
	// SourceMessageID is the order message's WhatsApp message ID — the
	// idempotency key that stops a webhook retry from creating a duplicate order.
	SourceMessageID string      `gorm:"size:100;uniqueIndex;not null" json:"source_message_id"`
	Status          OrderStatus `gorm:"size:20;default:'new';index" json:"status"`
	// PaymentMethod is "" until the customer picks a payment-method button;
	// then "cod" or "online".
	PaymentMethod string `gorm:"size:20" json:"payment_method"`
	PaymentStatus string `gorm:"size:20;default:'unpaid'" json:"payment_status"`
	Subtotal      int64  `gorm:"not null" json:"subtotal"` // smallest currency unit (paise/cents)
	Currency      string `gorm:"size:3;default:'INR'" json:"currency"`
	CustomerNote  string `gorm:"type:text" json:"customer_note"`

	RazorpayPaymentLinkID string     `gorm:"size:100;index" json:"razorpay_payment_link_id,omitempty"`
	RazorpayPaymentID     string     `gorm:"size:100" json:"razorpay_payment_id,omitempty"`
	PaidAt                *time.Time `json:"paid_at,omitempty"`

	TrackingCarrier string `gorm:"size:100" json:"tracking_carrier"`
	TrackingNumber  string `gorm:"size:100" json:"tracking_number"`
	TrackingURL     string `gorm:"size:500" json:"tracking_url"`

	CreatedByID *uuid.UUID `gorm:"type:uuid" json:"created_by_id,omitempty"`

	// Relations
	Organization *Organization `gorm:"foreignKey:OrganizationID" json:"organization,omitempty"`
	Contact      *Contact      `gorm:"foreignKey:ContactID" json:"contact,omitempty"`
	Catalog      *Catalog      `gorm:"foreignKey:CatalogID" json:"catalog,omitempty"`
	Items        []OrderItem   `gorm:"foreignKey:OrderID" json:"items,omitempty"`
}

func (Order) TableName() string {
	return "orders"
}

// OrderItem is one line item of an Order, snapshotting product details at
// order time (price/name can change in the catalog later; the order should not).
type OrderItem struct {
	BaseModel
	OrderID          uuid.UUID  `gorm:"type:uuid;index;not null" json:"order_id"`
	CatalogProductID *uuid.UUID `gorm:"type:uuid;index" json:"catalog_product_id,omitempty"`
	// RetailerID is Meta's product_retailer_id — always present even when it
	// doesn't match a known CatalogProduct (e.g. the catalog wasn't synced yet).
	RetailerID  string `gorm:"size:100;not null" json:"retailer_id"`
	ProductName string `gorm:"size:255;not null" json:"product_name"`
	Quantity    int    `gorm:"not null" json:"quantity"`
	UnitPrice   int64  `gorm:"not null" json:"unit_price"`
	Currency    string `gorm:"size:3;default:'INR'" json:"currency"`

	// Relations
	Order          *Order          `gorm:"foreignKey:OrderID" json:"order,omitempty"`
	CatalogProduct *CatalogProduct `gorm:"foreignKey:CatalogProductID" json:"catalog_product,omitempty"`
}

func (OrderItem) TableName() string {
	return "order_items"
}
