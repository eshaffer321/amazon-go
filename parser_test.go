package amazon

import (
	"os"
	"strings"
	"testing"
)

func TestParseOrderList(t *testing.T) {
	f, err := os.Open("internal/testdata/order_list.html")
	if err != nil {
		t.Skip("testdata not available:", err)
	}
	defer f.Close()

	parser := NewParser()
	orders, err := parser.ParseOrderList(f)
	if err != nil {
		t.Fatalf("ParseOrderList failed: %v", err)
	}

	if len(orders) == 0 {
		t.Error("Expected to parse at least one order")
	}

	// Verify first order has expected fields
	for i, order := range orders {
		if order.ID == "" {
			t.Errorf("Order %d: expected non-empty ID", i)
		}

		// Order ID should match pattern XXX-XXXXXXX-XXXXXXX
		if len(order.ID) != 19 {
			t.Errorf("Order %d: unexpected ID format: %s", i, order.ID)
		}

		t.Logf("Order %d: ID=%s, Date=%v, Total=%.2f, Items=%d",
			i, order.ID, order.Date, order.Total, order.ItemCount)
	}
}

func TestParseLegacyOrderList(t *testing.T) {
	html := `
		<html><body>
			<h1>Your Orders</h1>
			<div class="js-order-card">
				<div>Order placed November 26, 2025</div>
				<div>Order Total $42.37</div>
				<a href="/gp/css/summary/print.html?orderID=114-9733092-9360267">Order details</a>
			</div>
			<div class="js-order-card">
				<a href="/gp/css/summary/print.html?orderID=D01-1234567-1234567">Digital order</a>
			</div>
		</body></html>`

	parser := NewParser()
	orders, err := parser.ParseOrderList(strings.NewReader(html))
	if err != nil {
		t.Fatalf("ParseOrderList failed: %v", err)
	}

	if len(orders) != 1 {
		t.Fatalf("Expected 1 processable order, got %d", len(orders))
	}
	if orders[0].ID != "114-9733092-9360267" {
		t.Fatalf("Unexpected order ID: %s", orders[0].ID)
	}
	if orders[0].Total != 42.37 {
		t.Fatalf("Unexpected order total: %.2f", orders[0].Total)
	}
	if orders[0].Date.IsZero() {
		t.Fatal("Expected order date to be parsed")
	}
}

func TestParseOrderListDetectsAmazonSignInTitle(t *testing.T) {
	html := `<html><head><title>Amazon Sign-In</title></head><body></body></html>`

	parser := NewParser()
	_, err := parser.ParseOrderList(strings.NewReader(html))
	if err == nil {
		t.Fatal("Expected sign-in page error")
	}
	if !strings.Contains(err.Error(), "session expired") {
		t.Fatalf("Expected session expired error, got: %v", err)
	}
}

func TestParseLegacyOrderDetails(t *testing.T) {
	html := `
		<html><body>
			<div>Order #114-9733092-9360267</div>
			<div>Order placed November 26, 2025</div>
			<div>Item(s) Subtotal: $40.00</div>
			<div>Shipping & Handling: $0.00</div>
			<div>Estimated tax: $2.37</div>
			<div>Grand Total: $42.37</div>
			<a href="/dp/B09XV8WDY6">Useful Kitchen Thing</a>
		</body></html>`

	parser := NewParser()
	order, err := parser.ParseOrderDetails(strings.NewReader(html))
	if err != nil {
		t.Fatalf("ParseOrderDetails failed: %v", err)
	}

	if order.ID != "114-9733092-9360267" {
		t.Fatalf("Unexpected order ID: %s", order.ID)
	}
	if order.Total != 42.37 {
		t.Fatalf("Unexpected total: %.2f", order.Total)
	}
	if len(order.Items) != 1 || order.Items[0].ASIN != "B09XV8WDY6" {
		t.Fatalf("Expected legacy item to be parsed, got %#v", order.Items)
	}
}

func TestParseOrderDetailsReadsImageOverlayQuantity(t *testing.T) {
	html := `
		<html><body>
			<div>Order #112-1131457-5825001</div>
			<div>Order placed July 4, 2026</div>
			<div data-component="chargeSummary">
				<div class="od-line-item-row">
					<span class="od-line-item-row-label">Item(s) Subtotal:</span>
					<span class="od-line-item-row-content">$79.98</span>
				</div>
				<div class="od-line-item-row">
					<span class="od-line-item-row-label">Grand Total:</span>
					<span class="od-line-item-row-content">$84.78</span>
				</div>
			</div>
			<div data-component="shipments">
				<div data-component="shipmentsLeftGrid">
					<div data-component="itemImage">
						<a href="/dp/B099RSXLGH"><img alt="Baby Sound Machine" /></a>
						<div class="od-item-view-qty"><span>2</span></div>
					</div>
				</div>
				<div data-component="purchasedItemsRightGrid">
					<div data-component="itemTitle">
						<a href="/dp/B099RSXLGH">Baby Sound Machine</a>
					</div>
					<div data-component="quantity"><span class="a-size-small"></span></div>
					<div data-component="unitPrice"><span class="a-offscreen">$39.99</span></div>
				</div>
			</div>
		</body></html>`

	parser := NewParser()
	order, err := parser.ParseOrderDetails(strings.NewReader(html))
	if err != nil {
		t.Fatalf("ParseOrderDetails failed: %v", err)
	}

	if len(order.Items) != 1 {
		t.Fatalf("Expected 1 item, got %d", len(order.Items))
	}
	item := order.Items[0]
	if item.Quantity != 2 {
		t.Fatalf("Expected quantity 2, got %.0f", item.Quantity)
	}
	if item.UnitPrice != 39.99 {
		t.Fatalf("Expected unit price 39.99, got %.2f", item.UnitPrice)
	}
	if item.Price != 79.98 {
		t.Fatalf("Expected line total 79.98, got %.2f", item.Price)
	}
}

func TestParseOrderDetails(t *testing.T) {
	f, err := os.Open("internal/testdata/order_detail.html")
	if err != nil {
		t.Skip("testdata not available:", err)
	}
	defer f.Close()

	parser := NewParser()
	order, err := parser.ParseOrderDetails(f)
	if err != nil {
		t.Fatalf("ParseOrderDetails failed: %v", err)
	}

	// Verify order has expected fields
	t.Logf("Order ID: %s", order.ID)
	t.Logf("Total: %.2f", order.Total)
	t.Logf("Subtotal: %.2f", order.Subtotal)
	t.Logf("Tax: %.2f", order.Tax)
	t.Logf("Shipping: %.2f", order.ShippingFees)
	t.Logf("Items: %d", len(order.Items))

	for i, item := range order.Items {
		t.Logf("  Item %d: %s (ASIN: %s, Price: %.2f, Qty: %.0f)",
			i, item.Name, item.ASIN, item.Price, item.Quantity)
	}

	// Expected values from our test HTML
	if order.Total == 0 {
		t.Error("Expected non-zero total")
	}

	if len(order.Items) == 0 {
		t.Error("Expected to parse at least one item")
	}
}

func TestExtractOrderIDFromURL(t *testing.T) {
	tests := []struct {
		url      string
		expected string
	}{
		{
			url:      "/gp/css/order-details?orderID=114-9733092-9360267&ref=ppx_yo2ov_dt_b_fed_order_details",
			expected: "114-9733092-9360267",
		},
		{
			url:      "https://www.amazon.com/your-orders/order-details?orderID=113-7382612-3141857",
			expected: "113-7382612-3141857",
		},
		{
			url:      "/some/other/path",
			expected: "",
		},
	}

	for _, tc := range tests {
		result := extractOrderIDFromURL(tc.url)
		if result != tc.expected {
			t.Errorf("extractOrderIDFromURL(%q) = %q, want %q", tc.url, result, tc.expected)
		}
	}
}

func TestExtractOrderIDFromText(t *testing.T) {
	tests := []struct {
		text     string
		expected string
	}{
		{
			text:     "Order #114-9733092-9360267",
			expected: "114-9733092-9360267",
		},
		{
			text:     "Your order 113-7382612-3141857 has shipped",
			expected: "113-7382612-3141857",
		},
		{
			text:     "No order here",
			expected: "",
		},
	}

	for _, tc := range tests {
		result := extractOrderIDFromText(tc.text)
		if result != tc.expected {
			t.Errorf("extractOrderIDFromText(%q) = %q, want %q", tc.text, result, tc.expected)
		}
	}
}

func TestExtractASINFromURL(t *testing.T) {
	tests := []struct {
		url      string
		expected string
	}{
		{
			url:      "/dp/B09XV8WDY6?ref=ppx_yo2ov_dt_b_fed_asin_title",
			expected: "B09XV8WDY6",
		},
		{
			url:      "https://www.amazon.com/dp/B0FJDMHXD1",
			expected: "B0FJDMHXD1",
		},
		{
			url:      "/your-orders/pop?asin=B0D6VC4PM6&orderId=114-9733092-9360267",
			expected: "B0D6VC4PM6",
		},
		{
			url:      "/some/other/path",
			expected: "",
		},
	}

	for _, tc := range tests {
		result := extractASINFromURL(tc.url)
		if result != tc.expected {
			t.Errorf("extractASINFromURL(%q) = %q, want %q", tc.url, result, tc.expected)
		}
	}
}

func TestParsePrice(t *testing.T) {
	tests := []struct {
		text     string
		expected float64
	}{
		{"$42.37", 42.37},
		{"USD 42.37", 42.37},
		{"$1,234.56", 1234.56},
		{"  $99.99  ", 99.99},
		{"0.00", 0.00},
		{"invalid", 0.00},
	}

	for _, tc := range tests {
		result := parsePrice(tc.text)
		if result != tc.expected {
			t.Errorf("parsePrice(%q) = %.2f, want %.2f", tc.text, result, tc.expected)
		}
	}
}

func TestParseQuantity(t *testing.T) {
	tests := []struct {
		text     string
		expected float64
	}{
		{"Qty: 2", 2},
		{"qty:3", 3},
		{"x5", 5},
		{"2x", 2},
		{"1", 1},
		{"", 0},
		{"invalid", 0},
	}

	for _, tc := range tests {
		result := parseQuantity(tc.text)
		if result != tc.expected {
			t.Errorf("parseQuantity(%q) = %.0f, want %.0f", tc.text, result, tc.expected)
		}
	}
}

func TestParseAmazonDate(t *testing.T) {
	tests := []struct {
		text    string
		wantErr bool
	}{
		{"November 26, 2025", false},
		{"Nov 26, 2025", false},
		{"Ordered November 26, 2025", false},
		{"Order placed January 15, 2024", false},
		{"invalid date", true},
	}

	for _, tc := range tests {
		_, err := parseAmazonDate(tc.text)
		if (err != nil) != tc.wantErr {
			t.Errorf("parseAmazonDate(%q) error = %v, wantErr = %v", tc.text, err, tc.wantErr)
		}
	}
}

func TestContainsMonth(t *testing.T) {
	tests := []struct {
		text     string
		expected bool
	}{
		{"November 26, 2025", true},
		{"Nov 26", true},
		{"2025-11-26", false},
		{"Order placed January 15", true},
		{"no date here", false},
	}

	for _, tc := range tests {
		result := containsMonth(tc.text)
		if result != tc.expected {
			t.Errorf("containsMonth(%q) = %v, want %v", tc.text, result, tc.expected)
		}
	}
}

func TestParseTransactions(t *testing.T) {
	f, err := os.Open("internal/testdata/transactions.html")
	if err != nil {
		t.Skip("testdata not available:", err)
	}
	defer f.Close()

	parser := NewParser()
	transactions, err := parser.ParseTransactions(f)
	if err != nil {
		t.Fatalf("ParseTransactions failed: %v", err)
	}

	if len(transactions) == 0 {
		t.Error("Expected to parse at least one transaction")
	}

	for i, tx := range transactions {
		t.Logf("Transaction %d:", i)
		t.Logf("  OrderID: %s", tx.OrderID)
		t.Logf("  Date: %v", tx.Date)
		t.Logf("  Amount: %.2f", tx.Amount)
		t.Logf("  PaymentMethod: %s", tx.PaymentMethod)
		t.Logf("  CardType: %s", tx.CardType)
		t.Logf("  LastFour: %s", tx.LastFour)
		t.Logf("  Merchant: %s", tx.Merchant)
		t.Logf("  Status: %s", tx.Status)
	}

	// Verify expected values from the test HTML
	// The test HTML shows: Prime Visa ****1211, -$44.91, Order #114-9733092-9360267
	found := false
	for _, tx := range transactions {
		if tx.OrderID == "114-9733092-9360267" {
			found = true
			if tx.Amount != 44.91 {
				t.Errorf("Expected amount 44.91, got %.2f", tx.Amount)
			}
			if tx.LastFour != "1211" {
				t.Errorf("Expected last four 1211, got %s", tx.LastFour)
			}
			if tx.CardType != "Visa" {
				t.Errorf("Expected card type Visa, got %s", tx.CardType)
			}
		}
	}

	if !found {
		t.Error("Expected to find transaction for order 114-9733092-9360267")
	}
}

func TestParsePaymentMethod(t *testing.T) {
	tests := []struct {
		method       string
		wantCardType string
		wantLastFour string
	}{
		{"Prime Visa ****1211", "Visa", "1211"},
		{"Mastercard ****5678", "Mastercard", "5678"},
		{"American Express ****0001", "Amex", "0001"},
		{"Discover ****9999", "Discover", "9999"},
		{"Amazon Gift Card", "Gift Card", ""},
		{"Debit Card ****1234", "Debit", "1234"},
		{"Chase Sapphire ****4321", "Chase Sapphire", "4321"},
		{"Amazon Visa points", "Visa", ""},
	}

	for _, tc := range tests {
		cardType, lastFour := parsePaymentMethod(tc.method)
		if cardType != tc.wantCardType {
			t.Errorf("parsePaymentMethod(%q) cardType = %q, want %q", tc.method, cardType, tc.wantCardType)
		}
		if lastFour != tc.wantLastFour {
			t.Errorf("parsePaymentMethod(%q) lastFour = %q, want %q", tc.method, lastFour, tc.wantLastFour)
		}
	}
}

func TestParseMultipleTransactions(t *testing.T) {
	f, err := os.Open("internal/testdata/transactions_multiple.html")
	if err != nil {
		t.Skip("testdata not available:", err)
	}
	defer f.Close()

	parser := NewParser()
	transactions, err := parser.ParseTransactions(f)
	if err != nil {
		t.Fatalf("ParseTransactions failed: %v", err)
	}

	// Should have 3 transactions based on the HTML
	if len(transactions) < 2 {
		t.Errorf("Expected at least 2 transactions, got %d", len(transactions))
	}

	t.Logf("Found %d transactions", len(transactions))

	for i, tx := range transactions {
		t.Logf("Transaction %d:", i)
		t.Logf("  OrderID: %s", tx.OrderID)
		t.Logf("  Date: %v", tx.Date)
		t.Logf("  Amount: %.2f", tx.Amount)
		t.Logf("  PaymentMethod: %s", tx.PaymentMethod)
		t.Logf("  CardType: %s", tx.CardType)
		t.Logf("  LastFour: %s", tx.LastFour)
		t.Logf("  Merchant: %s", tx.Merchant)
		t.Logf("  Status: %s", tx.Status)
	}

	// Verify all transactions are for the same order
	expectedOrderID := "112-4559127-2161020"
	for i, tx := range transactions {
		if tx.OrderID != expectedOrderID {
			t.Errorf("Transaction %d: expected order ID %s, got %s", i, expectedOrderID, tx.OrderID)
		}
	}

	// Expected amounts from the HTML: $52.55, $50.72, $8.03
	expectedAmounts := []float64{52.55, 50.72, 8.03}
	foundAmounts := make(map[float64]bool)
	for _, tx := range transactions {
		foundAmounts[tx.Amount] = true
	}

	for _, expected := range expectedAmounts {
		if !foundAmounts[expected] {
			t.Errorf("Expected to find transaction with amount %.2f", expected)
		}
	}

	// Calculate total
	var total float64
	for _, tx := range transactions {
		total += tx.Amount
	}
	t.Logf("Total of all transactions: %.2f", total)

	// The total should be approximately $111.30 (52.55 + 50.72 + 8.03)
	expectedTotal := 111.30
	if total < expectedTotal-0.01 || total > expectedTotal+0.01 {
		t.Errorf("Expected total %.2f, got %.2f", expectedTotal, total)
	}
}
