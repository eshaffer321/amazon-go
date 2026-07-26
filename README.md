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

## Quick Start: Browser Profile Import

The most reliable setup is exporting cookies through a browser profile that is
already logged into Amazon. This uses Playwright only in the helper CLI; the
library itself still fetches orders with normal HTTP requests after cookies are
saved. The importer persists both the browser cookie snapshot and the browser's
user agent/client-hint fingerprint so later HTTP requests retain the identity
Amazon authenticated. The exporter opens Chromium visibly by default so Amazon
can renew a session if needed. Headless export is supported for already-valid
profiles, but it cannot complete Amazon sign-in, passkey, or MFA challenges.

```bash
go run ./cmd/amazon-go import-browser-profile \
  -profile-dir "$HOME/.itemize/amazon/amazon-erick" \
  -account erick
```

Then fetch orders:

```bash
YEAR=$(date +%Y)
go run ./examples/fetch_orders.go -account erick -year "$YEAR" -details -max 10
```

The `-profile-dir` value should be a Chromium/Playwright user-data directory.
For migration from `amazon-order-scraper`, use the same profile directory that
worked there, for example:

```text
~/.itemize/amazon/amazon-erick
~/.itemize/amazon/amazon-wife
```

If Playwright is not auto-detected, install it in the working directory or pass
`-playwright-root` pointing at a directory that contains `node_modules/playwright`:

```bash
npm install playwright
go run ./cmd/amazon-go import-browser-profile \
  -profile-dir "$HOME/.itemize/amazon/amazon-erick" \
  -account erick
```

For automation, add `-headless` after the profile has a valid session:

```bash
go run ./cmd/amazon-go import-browser-profile \
  -headless \
  -profile-dir "$HOME/.itemize/amazon/amazon-wife" \
  -account amazon-wife
```

## Multiple Accounts

Use separate account names for separate browser profiles:

```bash
go run ./cmd/amazon-go import-browser-profile \
  -profile-dir "$HOME/.itemize/amazon/amazon-erick" \
  -account erick

go run ./cmd/amazon-go import-browser-profile \
  -profile-dir "$HOME/.itemize/amazon/amazon-wife" \
  -account amazon-wife
```

Then fetch one account explicitly:

```bash
go run ./examples/fetch_orders.go -account amazon-wife -year "$YEAR" -details -max 10
```

## Auth Check

Verify saved cookies can access Amazon order history:

```bash
go run ./cmd/amazon-go check-auth -account erick
```

If this fails, re-open the browser profile, make sure it can load
`https://www.amazon.com/gp/css/order-history?disableCsd=no-js`, and re-run
`import-browser-profile`.

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
    client, err := amazon.NewClient(amazon.WithAccount("personal"))
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

Browser-profile import persists the matching fingerprint automatically. For a
custom importer, associate the captured identity with the cookie snapshot:

```go
fingerprint := amazon.BrowserFingerprint{
    UserAgent:       browserUserAgent,
    SecCHUA:         browserSecCHUA,
    SecCHUAMobile:   "?0",
    SecCHUAPlatform: `"macOS"`,
}

client, err := amazon.NewClient(
    amazon.WithAccount("personal"),
    amazon.WithBrowserFingerprint(fingerprint),
)
client.CookieStore().Replace(browserCookies)
client.CookieStore().SetBrowserFingerprint(fingerprint)
err = client.SaveCookies()
```

`amazon-go` stores account-specific cookie jars in:

```go
client, err := amazon.NewClient(amazon.WithAccount("personal"))
```

That stores cookies at:

```text
~/.amazon-go/cookies-personal.json
```

Use one browser profile per Amazon account. Amazon's in-page "Switch Accounts"
menu changes whichever identity is active for that one cookie jar; it does not
create separate cookie accounts by itself.

For reliable multi-account scraping, import each profile into its own account:

```bash
go run ./cmd/amazon-go import-browser-profile \
  -profile-dir "$HOME/.itemize/amazon/amazon-erick" \
  -account erick

go run ./cmd/amazon-go import-browser-profile \
  -profile-dir "$HOME/.itemize/amazon/amazon-wife" \
  -account wife

go run ./examples/fetch_orders.go -account wife -details
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
