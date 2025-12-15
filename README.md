# amazon-go

> **Project Status: Archived**
>
> As of December 2024, Amazon has implemented client-side encryption on their order history pages. Order data is now encrypted in the HTML and decrypted via JavaScript in the browser (`SiegeClientSideDecryption`). This means server-side HTML parsing no longer works - the raw HTML contains encrypted blobs instead of order details.
>
> **Alternative approach:** Use a browser extension that can read the DOM after JavaScript has decrypted the content. See [monarchmoney-sync-backend](https://github.com/eshaffer321/monarchmoney-sync-backend) for the project that will handle Amazon order syncing via a different method.

---

Go library for fetching Amazon order history and payment transactions by parsing HTML pages.

Amazon doesn't expose a JSON API for order history - pages are server-side rendered HTML. This library uses goquery to parse the HTML and extract order data.

## Why This Doesn't Work Anymore

Amazon now encrypts order data in the HTML response:

```html
<div class="order-card js-order-card">
  <div class="csd-encrypted-sensitive" id="...">
    <script>
      SiegeClientSideDecryption.decryptInElementWithId(elementId, {
        "ct": "S9XspR+u8Ori3uoQzMMh4k4SiVDD...", // encrypted order data
        "iv": "V5t1PF1IfzPo+xrD",
        "kid": "c3a22d"
      });
    </script>
  </div>
</div>
```

The `.order-card` elements exist, but their contents are encrypted ciphertext that requires JavaScript execution to decrypt. Without running a full browser, you can't access the order IDs, dates, totals, or items.

---

## Original Documentation (for reference)

<details>
<summary>Click to expand original usage docs</summary>

### Installation

```bash
go get github.com/eshaffer321/amazon-go
```

### Authentication

The library uses cookie-based authentication. Extract cookies from your browser:

1. Log into Amazon in your browser
2. Go to https://www.amazon.com/your-orders/orders
3. Open DevTools (F12) -> Network tab
4. Refresh the page
5. Right-click the main request -> Copy as cURL

Then import the cookies:

```go
client, _ := amazon.NewClient()
client.ImportCookiesFromCurl(curlCommand)
// Cookies are saved to ~/.amazon-go/cookies.json
```

### Usage

```go
client, _ := amazon.NewClient()

// Fetch orders with item details
orders, _ := client.FetchOrders(ctx, amazon.FetchOptions{
    Year:           2025,
    IncludeDetails: true,
})

for _, order := range orders {
    fmt.Printf("Order %s - $%.2f\n", order.ID, order.Total)
    for _, item := range order.Items {
        fmt.Printf("  %s - $%.2f\n", item.Name, item.Price)
    }
}
```

### Transactions

Amazon orders can have multiple payment transactions (split shipments, partial charges, etc). This is important for matching bank/credit card transactions to orders.

```go
// Get transactions for an order
transactions, _ := client.FetchTransactions(ctx, "114-1234567-1234567")

for _, tx := range transactions {
    fmt.Printf("%s: $%.2f on %s (card ending %s)\n",
        tx.OrderID, tx.Amount, tx.Date.Format("Jan 2"), tx.LastFour)
}
```

### Data structures

```go
type Order struct {
    ID           string
    Date         time.Time
    Total        float64
    Subtotal     float64
    Tax          float64
    ShippingFees float64
    Items        []*OrderItem
}

type OrderItem struct {
    Name      string
    Price     float64   // line total
    Quantity  float64
    UnitPrice float64
    ASIN      string    // Amazon product ID
}

type Transaction struct {
    OrderID       string    // links to Order.ID
    Date          time.Time // when charged (may differ from order date)
    Amount        float64
    PaymentMethod string    // e.g. "Prime Visa ****1211"
    CardType      string    // Visa, Mastercard, Amex, etc.
    LastFour      string    // last 4 digits of card
    Merchant      string    // e.g. "AMZN Mktp US"
    Status        string    // Completed, Pending, etc.
}
```

</details>
