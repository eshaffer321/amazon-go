package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	amazon "github.com/eshaffer321/amazon-go"
)

func main() {
	var (
		year             int
		maxOrders        int
		details          bool
		verbose          bool
		importCurl       string
		importCurlFile   string
		importCookie     string
		importCookieFile string
		cookieFile       string
		account          string
		checkAuth        bool
	)

	flag.IntVar(&year, "year", time.Now().Year(), "Year to fetch orders from")
	flag.IntVar(&maxOrders, "max", 0, "Maximum number of orders to fetch (0 = all)")
	flag.BoolVar(&details, "details", false, "Fetch full order details including items")
	flag.BoolVar(&verbose, "verbose", false, "Enable verbose logging")
	flag.StringVar(&importCurl, "import-curl", "", "Import cookies from a curl command (prefer -import-curl-file - to avoid shell history)")
	flag.StringVar(&importCurlFile, "import-curl-file", "", "Import cookies from a file containing copied cURL, or '-' for stdin")
	flag.StringVar(&importCookie, "import-cookie", "", "Import cookies from a raw Cookie header (prefer -import-cookie-file - to avoid shell history)")
	flag.StringVar(&importCookieFile, "import-cookie-file", "", "Import cookies from a file containing a Cookie header, or '-' for stdin")
	flag.StringVar(&cookieFile, "cookie-file", "", "Path to cookie file")
	flag.StringVar(&account, "account", "", "Account name for ~/.amazon-go/cookies-{account}.json")
	flag.BoolVar(&checkAuth, "check-auth", false, "Verify stored cookies can access Amazon")
	flag.Parse()

	// Setup logger
	var logger *slog.Logger
	if verbose {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}))
	} else {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))
	}

	// Create client options
	opts := []amazon.Option{
		amazon.WithLogger(logger),
		amazon.WithAutoSave(true),
	}

	if cookieFile != "" {
		opts = append(opts, amazon.WithCookieFile(cookieFile))
	}
	if account != "" {
		opts = append(opts, amazon.WithAccount(account))
	}

	// Create client
	client, err := amazon.NewClient(opts...)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// Import cookies if provided
	if importCurlFile != "" {
		value, err := readInput(importCurlFile)
		if err != nil {
			log.Fatalf("Failed to read cURL input: %v", err)
		}
		importCurl = value
	}
	if importCurl != "" {
		fmt.Println("Importing cookies from curl command...")
		if err := client.ImportCookiesFromCurl(importCurl); err != nil {
			log.Fatalf("Failed to import cookies: %v", err)
		}
		fmt.Println("Cookies imported successfully!")
		return
	}
	if importCookieFile != "" {
		value, err := readInput(importCookieFile)
		if err != nil {
			log.Fatalf("Failed to read cookie input: %v", err)
		}
		importCookie = value
	}
	if importCookie != "" {
		fmt.Println("Importing cookies from Cookie header...")
		if err := client.ImportCookiesFromHeader(importCookie); err != nil {
			log.Fatalf("Failed to import cookies: %v", err)
		}
		fmt.Println("Cookies imported successfully!")
		return
	}

	if checkAuth {
		if err := client.HealthCheck(); err != nil {
			log.Fatalf("Auth check failed: %v", err)
		}
		fmt.Println("Auth check passed.")
		return
	}

	if account == "" && cookieFile == "" {
		accounts, err := discoverArcAccounts()
		if err != nil {
			log.Fatalf("Failed to discover Arc accounts: %v", err)
		}
		if len(accounts) > 0 {
			fetchAllAccounts(accounts, opts, year, maxOrders, details)
			return
		}
	}

	// Check if we have cookies
	if !client.CookieStore().HasEssentialCookies() {
		fmt.Println("No cookies found. Please import cookies first:")
		fmt.Println("")
		fmt.Println("1. Log into Amazon in your browser")
		fmt.Println("2. Navigate to https://www.amazon.com/gp/css/order-history?disableCsd=no-js")
		fmt.Println("3. Open DevTools -> Network tab")
		fmt.Println("4. Refresh the page")
		fmt.Println("5. Right-click on the main request -> 'Copy as cURL'")
		fmt.Println("6. Run: pbpaste | go run examples/fetch_orders.go -import-curl-file -")
		os.Exit(1)
	}

	orders, err := fetchOrders(client, year, maxOrders, details)
	if err != nil {
		log.Fatalf("Failed to fetch orders: %v", err)
	}
	displayOrders(orders)
}

func fetchOrders(client *amazon.Client, year, maxOrders int, details bool) ([]*amazon.Order, error) {
	fmt.Printf("Fetching orders for year %d...\n", year)
	ctx := context.Background()
	fetchOpts := amazon.FetchOptions{
		Year:           year,
		MaxOrders:      maxOrders,
		IncludeDetails: details,
	}

	return client.FetchOrders(ctx, fetchOpts)
}

func displayOrders(orders []*amazon.Order) {
	// Display results
	fmt.Printf("\nFound %d orders:\n\n", len(orders))

	for _, order := range orders {
		fmt.Printf("Order ID: %s\n", order.GetID())
		if !order.GetDate().IsZero() {
			fmt.Printf("  Date:     %s\n", order.GetDate().Format("January 2, 2006"))
		}
		fmt.Printf("  Total:    $%.2f\n", order.GetTotal())

		if order.GetSubtotal() > 0 {
			fmt.Printf("  Subtotal: $%.2f\n", order.GetSubtotal())
		}
		if order.GetTax() > 0 {
			fmt.Printf("  Tax:      $%.2f\n", order.GetTax())
		}
		if order.GetFees() > 0 {
			fmt.Printf("  Shipping: $%.2f\n", order.GetFees())
		}

		items := order.GetItems()
		if len(items) > 0 {
			fmt.Printf("  Items (%d):\n", len(items))
			for _, item := range items {
				name := item.GetName()
				if len(name) > 60 {
					name = name[:57] + "..."
				}
				fmt.Printf("    - %s", name)
				if item.GetPrice() > 0 {
					fmt.Printf(" ($%.2f", item.GetPrice())
					if item.GetQuantity() > 1 {
						fmt.Printf(" x%.0f", item.GetQuantity())
					}
					fmt.Print(")")
				}
				fmt.Println()
			}
		}
		fmt.Println()
	}

	// Summary
	var total float64
	for _, order := range orders {
		total += order.GetTotal()
	}
	fmt.Printf("Total spent: $%.2f across %d orders\n", total, len(orders))
}

func fetchAllAccounts(accounts []string, baseOpts []amazon.Option, year, maxOrders int, details bool) {
	var totalOrders int
	var totalSpent float64
	fetched := 0

	for _, account := range accounts {
		opts := append([]amazon.Option{}, baseOpts...)
		opts = append(opts, amazon.WithAccount(account))

		client, err := amazon.NewClient(opts...)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Skipping account %q: %v\n", account, err)
			continue
		}
		if !client.CookieStore().HasEssentialCookies() {
			fmt.Fprintf(os.Stderr, "Skipping account %q: no essential Amazon cookies\n", account)
			continue
		}
		if err := client.HealthCheck(); err != nil {
			fmt.Fprintf(os.Stderr, "Skipping account %q: auth check failed: %v\n", account, err)
			continue
		}

		fmt.Printf("\n=== Account: %s ===\n", account)
		orders, err := fetchOrders(client, year, maxOrders, details)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Skipping account %q: fetch failed: %v\n", account, err)
			continue
		}
		displayOrders(orders)

		fetched++
		totalOrders += len(orders)
		for _, order := range orders {
			totalSpent += order.GetTotal()
		}
	}

	if fetched == 0 {
		log.Fatal("No Arc accounts could be fetched. Run: go run ./cmd/amazon-go import-arc")
	}
	fmt.Printf("\nFetched %d Arc accounts. Combined total spent: $%.2f across %d orders\n", fetched, totalSpent, totalOrders)
}

func discoverArcAccounts() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	matches, err := filepath.Glob(filepath.Join(home, ".amazon-go", "cookies-arc-*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)

	accounts := make([]string, 0, len(matches))
	for _, path := range matches {
		name := filepath.Base(path)
		account := strings.TrimSuffix(strings.TrimPrefix(name, "cookies-"), ".json")
		if account != "" {
			accounts = append(accounts, account)
		}
	}
	return accounts, nil
}

func readInput(path string) (string, error) {
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}
