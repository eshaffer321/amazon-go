# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build and test commands

```bash
go build ./...          # build all packages
go test ./...           # run all tests
go test ./... -v        # run tests with verbose output
go test -run TestName   # run a specific test
```

## Architecture

This library fetches Amazon order history by parsing server-side rendered HTML (Amazon has no JSON API for orders). It uses goquery for HTML parsing.

**Core components:**

- `client.go` - HTTP client with cookie management, rate limiting (default 1 req/sec), and retry logic. Uses functional options pattern (`WithAccount`, `WithRateLimit`, etc.)
- `auth.go` - Cookie persistence to `~/.amazon-go/cookies.json`. Supports multiple accounts via `CookiePathForAccount()`. Can import cookies from curl commands.
- `parser.go` - HTML parsing with goquery. Extracts orders from list pages, order details, and payment transactions.
- `orders.go` - Coordinates fetching: gets order summaries from list pages, optionally fetches full details for each order, fetches payment transactions.
- `types.go` - Data structures: `Order`, `OrderItem`, `OrderSummary`, `Transaction`. Order and OrderItem have getter methods for interface compatibility.

**Key data flow:**

1. `Client.FetchOrders()` calls `fetchOrderSummaries()` which paginates through order list pages
2. If `IncludeDetails: true`, it fetches each order's detail page for item info
3. `Client.FetchTransactions()` fetches payment data separately (orders can have multiple transactions due to split shipments)

**Test data:**

Tests that parse HTML use files in `internal/testdata/` which are gitignored (contain personal data). Tests skip gracefully when testdata is unavailable.
