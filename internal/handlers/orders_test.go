package handlers

import (
	"testing"

	"github.com/google/uuid"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/test/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newOrderTestMessage(wamid, catalogMetaID string, items ...struct {
	RetailerID string
	Quantity   int
	Price      float64
	Currency   string
}) IncomingTextMessage {
	msg := IncomingTextMessage{
		From: "15550001111",
		ID:   wamid,
		Type: "order",
		Order: &struct {
			CatalogID    string `json:"catalog_id"`
			Text         string `json:"text,omitempty"`
			ProductItems []struct {
				ProductRetailerID string  `json:"product_retailer_id"`
				Quantity          int     `json:"quantity"`
				ItemPrice         float64 `json:"item_price"`
				Currency          string  `json:"currency"`
			} `json:"product_items"`
		}{CatalogID: catalogMetaID},
	}
	for _, it := range items {
		msg.Order.ProductItems = append(msg.Order.ProductItems, struct {
			ProductRetailerID string  `json:"product_retailer_id"`
			Quantity          int     `json:"quantity"`
			ItemPrice         float64 `json:"item_price"`
			Currency          string  `json:"currency"`
		}{ProductRetailerID: it.RetailerID, Quantity: it.Quantity, ItemPrice: it.Price, Currency: it.Currency})
	}
	return msg
}

func TestHandleIncomingOrder_CreatesOrderAndResolvesKnownProduct(t *testing.T) {
	app := newProcessorTestApp(t)
	org, account := createProcessorTestOrg(t, app)
	contact := testutil.CreateTestContact(t, app.DB, org.ID)

	catalog := models.Catalog{OrganizationID: org.ID, WhatsAppAccount: account.Name, MetaCatalogID: "meta_cat_1", Name: "Main Catalog", IsActive: true}
	require.NoError(t, app.DB.Create(&catalog).Error)

	product := models.CatalogProduct{OrganizationID: org.ID, CatalogID: catalog.ID, MetaProductID: "meta_prod_1", Name: "Blue T-Shirt", Price: 49900, Currency: "INR", RetailerID: "sku-blue-tee", IsActive: true}
	require.NoError(t, app.DB.Create(&product).Error)

	msg := newOrderTestMessage("wamid.order1", "meta_cat_1", struct {
		RetailerID string
		Quantity   int
		Price      float64
		Currency   string
	}{RetailerID: "sku-blue-tee", Quantity: 2, Price: 499.00, Currency: "INR"})

	app.handleIncomingOrder(account, contact, msg)

	var order models.Order
	require.NoError(t, app.DB.Where("source_message_id = ?", "wamid.order1").First(&order).Error)
	assert.Equal(t, org.ID, order.OrganizationID)
	assert.Equal(t, contact.ID, order.ContactID)
	assert.Equal(t, catalog.ID, *order.CatalogID)
	assert.Equal(t, models.OrderStatusNew, order.Status)
	assert.Equal(t, int64(99800), order.Subtotal) // 499.00 * 100 paise * 2 qty

	var items []models.OrderItem
	require.NoError(t, app.DB.Where("order_id = ?", order.ID).Find(&items).Error)
	require.Len(t, items, 1)
	assert.Equal(t, "Blue T-Shirt", items[0].ProductName)
	assert.Equal(t, product.ID, *items[0].CatalogProductID)
	assert.Equal(t, 2, items[0].Quantity)
}

func TestHandleIncomingOrder_UnmatchedRetailerIDFallsBackToRetailerIDAsName(t *testing.T) {
	app := newProcessorTestApp(t)
	org, account := createProcessorTestOrg(t, app)
	contact := testutil.CreateTestContact(t, app.DB, org.ID)

	msg := newOrderTestMessage("wamid.order2", "unknown_catalog", struct {
		RetailerID string
		Quantity   int
		Price      float64
		Currency   string
	}{RetailerID: "sku-unknown", Quantity: 1, Price: 100, Currency: "INR"})

	app.handleIncomingOrder(account, contact, msg)

	var order models.Order
	require.NoError(t, app.DB.Where("source_message_id = ?", "wamid.order2").First(&order).Error)
	assert.Nil(t, order.CatalogID)

	var items []models.OrderItem
	require.NoError(t, app.DB.Where("order_id = ?", order.ID).Find(&items).Error)
	require.Len(t, items, 1)
	assert.Equal(t, "sku-unknown", items[0].ProductName)
	assert.Nil(t, items[0].CatalogProductID)
}

func TestHandleIncomingOrder_DuplicateWebhookIsIdempotent(t *testing.T) {
	app := newProcessorTestApp(t)
	org, account := createProcessorTestOrg(t, app)
	contact := testutil.CreateTestContact(t, app.DB, org.ID)

	msg := newOrderTestMessage("wamid.order3", "cat", struct {
		RetailerID string
		Quantity   int
		Price      float64
		Currency   string
	}{RetailerID: "sku-1", Quantity: 1, Price: 100, Currency: "INR"})

	app.handleIncomingOrder(account, contact, msg)
	app.handleIncomingOrder(account, contact, msg) // simulate a webhook retry

	var count int64
	app.DB.Model(&models.Order{}).Where("source_message_id = ?", "wamid.order3").Count(&count)
	assert.Equal(t, int64(1), count)
}

func TestHandleOrderPaymentChoice_CODConfirmsOrder(t *testing.T) {
	app := newProcessorTestApp(t)
	org, account := createProcessorTestOrg(t, app)
	contact := testutil.CreateTestContact(t, app.DB, org.ID)

	order := models.Order{OrganizationID: org.ID, WhatsAppAccount: account.Name, ContactID: contact.ID, SourceMessageID: uuid.New().String(), Status: models.OrderStatusNew, Subtotal: 10000, Currency: "INR"}
	require.NoError(t, app.DB.Create(&order).Error)

	app.handleOrderPaymentChoice(account, contact, "order_cod:"+order.ID.String())

	var updated models.Order
	require.NoError(t, app.DB.Where("id = ?", order.ID).First(&updated).Error)
	assert.Equal(t, models.OrderStatusConfirmed, updated.Status)
	assert.Equal(t, "cod", updated.PaymentMethod)
}
