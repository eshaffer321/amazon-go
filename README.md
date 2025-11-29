# amazon-go

Go library for fetching Amazon order history and payment transactions by parsing HTML pages.

Amazon doesn't expose a JSON API for order history - pages are server-side rendered HTML. This library uses goquery to parse the HTML and extract order data.

## Installation

```bash
go get github.com/eshaffer321/amazon-go
```

## Authentication

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

## Usage

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

## Transactions

Amazon orders can have multiple payment transactions (split shipments, partial charges, etc). This is important for matching bank/credit card transactions to orders.

```go
// Get transactions for an order
transactions, _ := client.FetchTransactions(ctx, "114-1234567-1234567")

for _, tx := range transactions {
    fmt.Printf("%s: $%.2f on %s (card ending %s)\n",
        tx.OrderID, tx.Amount, tx.Date.Format("Jan 2"), tx.LastFour)
}
```

Example: an order totaling $111.30 might have three transactions:
- $52.55 charged Nov 25 (Prime Visa ****1211)
- $50.72 charged Nov 25 (Prime Visa ****1211)
- $8.03 charged Nov 24 (Amazon Visa points)

## Multiple accounts

```go
// Each account gets its own cookie file
personalClient, _ := amazon.NewClient(amazon.WithAccount("personal"))
workClient, _ := amazon.NewClient(amazon.WithAccount("work"))

// Cookies stored at:
// ~/.amazon-go/cookies-personal.json
// ~/.amazon-go/cookies-work.json
```

## Data structures

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

## Client options

```go
client, _ := amazon.NewClient(
    amazon.WithAccount("personal"),           // multi-account support
    amazon.WithCookieFile("/custom/path"),    // custom cookie location
    amazon.WithRateLimit(2*time.Second),      // default is 1s
    amazon.WithMaxRetries(3),
    amazon.WithLogger(slog.Default()),
)
```

## Fetch options

```go
orders, _ := client.FetchOrders(ctx, amazon.FetchOptions{
    Year:           2025,              // specific year
    StartDate:      time.Time{},       // or date range
    EndDate:        time.Time{},
    MaxOrders:      50,                // limit results
    IncludeDetails: true,              // fetch item details (slower)
})
```

## Limitations

- Requires manual cookie extraction from browser
- Only supports amazon.com (not international)
- HTML parsing may break if Amazon changes their page structure
- Rate limited by default to avoid detection
