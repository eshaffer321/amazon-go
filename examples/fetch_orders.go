package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	amazon "github.com/eshaffer321/amazon-go"
)

func main() {
	var (
		year       int
		maxOrders  int
		details    bool
		verbose    bool
		importCurl string
		cookieFile string
	)

	flag.IntVar(&year, "year", time.Now().Year(), "Year to fetch orders from")
	flag.IntVar(&maxOrders, "max", 0, "Maximum number of orders to fetch (0 = all)")
	flag.BoolVar(&details, "details", false, "Fetch full order details including items")
	flag.BoolVar(&verbose, "verbose", false, "Enable verbose logging")
	flag.StringVar(&importCurl, "import-curl", "", "Import cookies from a curl command")
	flag.StringVar(&cookieFile, "cookie-file", "", "Path to cookie file")
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

	// Create client
	client, err := amazon.NewClient(opts...)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// Import cookies if provided
	if importCurl != "" {
		fmt.Println("Importing cookies from curl command...")
		if err := client.ImportCookiesFromCurl(importCurl); err != nil {
			log.Fatalf("Failed to import cookies: %v", err)
		}
		fmt.Println("Cookies imported successfully!")
		return
	}

	// Check if we have cookies
	if !client.CookieStore().HasEssentialCookies() {
		fmt.Println("No cookies found. Please import cookies first:")
		fmt.Println("")
		fmt.Println("1. Log into Amazon in your browser")
		fmt.Println("2. Navigate to https://www.amazon.com/your-orders/orders")
		fmt.Println("3. Open DevTools -> Network tab")
		fmt.Println("4. Refresh the page")
		fmt.Println("5. Right-click on the main request -> 'Copy as cURL'")
		fmt.Println("6. Run: go run examples/fetch_orders.go -import-curl \"<paste curl command here>\"")
		os.Exit(1)
	}

	// Fetch orders
	fmt.Printf("Fetching orders for year %d...\n", year)

	ctx := context.Background()
	fetchOpts := amazon.FetchOptions{
		Year:           year,
		MaxOrders:      maxOrders,
		IncludeDetails: details,
	}

	orders, err := client.FetchOrders(ctx, fetchOpts)
	if err != nil {
		log.Fatalf("Failed to fetch orders: %v", err)
	}

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
