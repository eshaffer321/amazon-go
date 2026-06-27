# amazon-go

Go library and helper CLI for fetching Amazon order history by parsing Amazon's HTML order pages.

## What Works Now

Amazon's modern `/your-orders/...` pages can contain client-side encrypted order data, which makes plain server-side parsing unreliable. This library uses Amazon's older no-JS order pages instead:

- Order history: `https://www.amazon.com/gp/css/order-history?disableCsd=no-js`
- Printable order details: `https://www.amazon.com/gp/css/summary/print.html?orderID=...`

The parser does not need a browser once it has a valid logged-in Amazon cookie set. Authentication still comes from a real browser session.

## Install

```bash
go get github.com/eshaffer321/amazon-go
```

## Quick Start: Arc on macOS

If you use Arc on macOS, the easiest setup is importing the Amazon cookies from your existing Arc profiles:

```bash
go run ./cmd/amazon-go import-arc
```

By default, this scans every Arc profile under:

```text
~/Library/Application Support/Arc/User Data
```

Each usable profile is validated and saved as a separate amazon-go account, for example:

```text
~/.amazon-go/cookies-arc-profile-1.json
```

Then fetch orders:

```bash
YEAR=$(date +%Y)
go run ./examples/fetch_orders.go -year "$YEAR" -details -max 10
```

With no `-account` or `-cookie-file`, the example command automatically tries every imported `cookies-arc-*.json` account it can authenticate. Expired or signed-out profiles are skipped.

To fetch one imported profile explicitly:

```bash
YEAR=$(date +%Y)
go run ./examples/fetch_orders.go -account arc-profile-1 -year "$YEAR" -details -max 10
```

To import one Arc profile with a custom account name:

```bash
go run ./cmd/amazon-go import-arc \
  -profile "$HOME/Library/Application Support/Arc/User Data/Profile 1/Cookies" \
  -account personal
```

## Browser Setup Fallback

If Arc import is not available, `setup-session` opens a dedicated Chromium profile, waits while you log into Amazon if needed, and saves the cookies into `~/.amazon-go/cookies.json`.

```bash
go run ./cmd/amazon-go setup-session
go run ./cmd/amazon-go check-auth
```

The dedicated browser profile lives at:

```text
~/.amazon-go/browser-profile
```

## Manual Cookie Import

You can also import cookies from a copied browser request.

1. Log into Amazon in your browser.
2. Open `https://www.amazon.com/gp/css/order-history?disableCsd=no-js`.
3. Open DevTools, then the Network tab.
4. Refresh the page.
5. Right-click the main request and choose "Copy as cURL".

Recommended, to avoid putting cookies in shell history:

```bash
pbpaste | go run ./examples/fetch_orders.go -import-curl-file -
go run ./examples/fetch_orders.go -check-auth
```

You can also import a raw `Cookie:` header:

```bash
pbpaste | go run ./examples/fetch_orders.go -import-cookie-file -
```

## Library Usage

```go
package main

import (
    "context"
    "fmt"
    "time"

    amazon "github.com/eshaffer321/amazon-go"
)

func main() {
    client, err := amazon.NewClient(amazon.WithAccount("arc-profile-1"))
    if err != nil {
        panic(err)
    }

    orders, err := client.FetchOrders(context.Background(), amazon.FetchOptions{
        Year:           time.Now().Year(),
        IncludeDetails: true,
    })
    if err != nil {
        panic(err)
    }

    for _, order := range orders {
        fmt.Printf("Order %s - $%.2f\n", order.GetID(), order.GetTotal())
        for _, item := range order.GetItems() {
            fmt.Printf("  %s - $%.2f\n", item.GetName(), item.GetPrice())
        }
    }
}
```

## Multiple Accounts

`amazon-go` supports multiple cookie jars through account names:

```go
client, err := amazon.NewClient(amazon.WithAccount("personal"))
```

That stores cookies at:

```text
~/.amazon-go/cookies-personal.json
```

The Arc importer uses this mechanism automatically. It handles multiple browser profiles, not Amazon's in-page "Switch Accounts" menu. If one browser profile can switch between two Amazon identities, `amazon-go` fetches whichever identity is currently active for that cookie jar.

For reliable multi-account scraping, use one browser profile per Amazon account, then run:

```bash
go run ./cmd/amazon-go import-arc
go run ./examples/fetch_orders.go -details
```

## Transactions

Amazon orders can have multiple payment transactions, such as split shipments or partial charges. The client exposes transaction fetching separately:

```go
transactions, err := client.FetchTransactions(ctx, "114-1234567-1234567")
if err != nil {
    panic(err)
}

for _, tx := range transactions {
    fmt.Printf("%s: $%.2f on %s\n", tx.OrderID, tx.Amount, tx.Date.Format("Jan 2"))
}
```

Order summary and item-detail scraping are the primary verified path. Transaction pages can be more variable and should be tested against your account before relying on them for reconciliation.

## Limitations

- Amazon can change these HTML pages at any time.
- Cookies expire and may need to be re-imported.
- Arc cookie import is macOS and Arc specific.
- The CLI does not automatically enumerate Amazon's "Switch Accounts" identities inside one browser session.
- Keep exported cookie files private. They are bearer credentials for your Amazon session.

## Data Structures

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
    Price     float64
    Quantity  float64
    UnitPrice float64
    ASIN      string
}

type Transaction struct {
    OrderID       string
    Date          time.Time
    Amount        float64
    PaymentMethod string
    CardType      string
    LastFour      string
    Merchant      string
    Status        string
}
```
