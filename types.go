package amazon

import "time"

// OrderItemInterface defines the interface for order items
type OrderItemInterface interface {
	GetName() string
	GetPrice() float64
	GetQuantity() float64
	GetUnitPrice() float64
	GetDescription() string
	GetSKU() string
	GetCategory() string
}

// Order represents an Amazon order with all its details
type Order struct {
	ID           string
	Date         time.Time
	Total        float64
	Subtotal     float64
	Tax          float64
	ShippingFees float64
	Items        []*OrderItem
}

// GetID returns the order ID
func (o *Order) GetID() string {
	return o.ID
}

// GetDate returns the order date
func (o *Order) GetDate() time.Time {
	return o.Date
}

// GetTotal returns the grand total of the order
func (o *Order) GetTotal() float64 {
	return o.Total
}

// GetSubtotal returns the subtotal before tax and fees
func (o *Order) GetSubtotal() float64 {
	return o.Subtotal
}

// GetTax returns the tax amount
func (o *Order) GetTax() float64 {
	return o.Tax
}

// GetTip returns the tip amount (Amazon doesn't support tips, always 0)
func (o *Order) GetTip() float64 {
	return 0
}

// GetFees returns shipping and handling fees
func (o *Order) GetFees() float64 {
	return o.ShippingFees
}

// GetItems returns all items in the order
func (o *Order) GetItems() []*OrderItem {
	return o.Items
}

// GetProviderName returns the provider name
func (o *Order) GetProviderName() string {
	return "Amazon"
}

// GetRawData returns the raw order data
func (o *Order) GetRawData() interface{} {
	return o
}

// OrderItem represents a single item in an Amazon order
type OrderItem struct {
	Name        string
	Price       float64
	Quantity    float64
	UnitPrice   float64
	ASIN        string
	Description string
	Category    string
}

// GetName returns the item name
func (i *OrderItem) GetName() string {
	return i.Name
}

// GetPrice returns the line total for this item
func (i *OrderItem) GetPrice() float64 {
	return i.Price
}

// GetQuantity returns the quantity of this item
func (i *OrderItem) GetQuantity() float64 {
	return i.Quantity
}

// GetUnitPrice returns the unit price of this item
func (i *OrderItem) GetUnitPrice() float64 {
	return i.UnitPrice
}

// GetDescription returns the item description
func (i *OrderItem) GetDescription() string {
	return i.Description
}

// GetSKU returns the ASIN (Amazon's SKU)
func (i *OrderItem) GetSKU() string {
	return i.ASIN
}

// GetCategory returns the item category (may be empty)
func (i *OrderItem) GetCategory() string {
	return i.Category
}

// OrderSummary represents basic order info from the order list page
type OrderSummary struct {
	ID         string
	Date       time.Time
	Total      float64
	ItemCount  int
	ItemNames  []string
	DetailURL  string
}

// Transaction represents a payment transaction for an order
// This is the actual charge made to a payment method
type Transaction struct {
	OrderID       string    // The associated order ID (e.g., "114-9733092-9360267")
	Date          time.Time // Date the charge was made
	Amount        float64   // Amount charged (positive value)
	PaymentMethod string    // Payment method description (e.g., "Prime Visa ****1211")
	CardType      string    // Card type extracted (e.g., "Visa", "Mastercard", "Amex")
	LastFour      string    // Last 4 digits of card (e.g., "1211")
	Merchant      string    // Merchant name (e.g., "AMZN Mktp US")
	Status        string    // Transaction status (e.g., "Completed", "Pending", "Refunded")
}

// GetOrderID returns the associated order ID
func (t *Transaction) GetOrderID() string {
	return t.OrderID
}

// GetDate returns the transaction date
func (t *Transaction) GetDate() time.Time {
	return t.Date
}

// GetAmount returns the transaction amount
func (t *Transaction) GetAmount() float64 {
	return t.Amount
}

// GetPaymentMethod returns the payment method description
func (t *Transaction) GetPaymentMethod() string {
	return t.PaymentMethod
}

// GetCardType returns the card type
func (t *Transaction) GetCardType() string {
	return t.CardType
}

// GetLastFour returns the last 4 digits of the card
func (t *Transaction) GetLastFour() string {
	return t.LastFour
}

// GetMerchant returns the merchant name
func (t *Transaction) GetMerchant() string {
	return t.Merchant
}

// GetStatus returns the transaction status
func (t *Transaction) GetStatus() string {
	return t.Status
}
