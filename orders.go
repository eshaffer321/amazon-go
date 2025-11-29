package amazon

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// FetchOptions specifies options for fetching orders
type FetchOptions struct {
	StartDate      time.Time
	EndDate        time.Time
	Year           int
	MaxOrders      int
	IncludeDetails bool
}

// FetchOrders fetches orders within the specified date range
func (c *Client) FetchOrders(ctx context.Context, opts FetchOptions) ([]*Order, error) {
	// Get order summaries first
	summaries, err := c.fetchOrderSummaries(ctx, opts)
	if err != nil {
		return nil, err
	}

	c.logger.Info("fetched order summaries", "count", len(summaries))

	// If details are not requested, convert summaries to orders
	if !opts.IncludeDetails {
		orders := make([]*Order, len(summaries))
		for i, summary := range summaries {
			orders[i] = &Order{
				ID:    summary.ID,
				Date:  summary.Date,
				Total: summary.Total,
			}
		}
		return orders, nil
	}

	// Fetch full details for each order
	var orders []*Order
	parser := NewParser()

	for _, summary := range summaries {
		select {
		case <-ctx.Done():
			return orders, ctx.Err()
		default:
		}

		c.logger.Debug("fetching order details", "orderID", summary.ID)

		order, err := c.fetchOrderDetails(summary.ID, parser)
		if err != nil {
			c.logger.Warn("failed to fetch order details",
				"orderID", summary.ID,
				"error", err,
			)
			// Use summary data as fallback
			orders = append(orders, &Order{
				ID:    summary.ID,
				Date:  summary.Date,
				Total: summary.Total,
			})
			continue
		}

		// Fill in date from summary if not parsed from details
		if order.Date.IsZero() {
			order.Date = summary.Date
		}

		orders = append(orders, order)
	}

	return orders, nil
}

// FetchOrder fetches a single order by ID
func (c *Client) FetchOrder(ctx context.Context, orderID string) (*Order, error) {
	parser := NewParser()
	return c.fetchOrderDetails(orderID, parser)
}

// fetchOrderSummaries fetches order list pages and returns summaries
func (c *Client) fetchOrderSummaries(ctx context.Context, opts FetchOptions) ([]*OrderSummary, error) {
	parser := NewParser()
	var allSummaries []*OrderSummary

	// Determine which years to fetch
	years := c.determineYears(opts)

	for _, year := range years {
		select {
		case <-ctx.Done():
			return allSummaries, ctx.Err()
		default:
		}

		summaries, err := c.fetchYearOrders(year, parser, opts)
		if err != nil {
			c.logger.Warn("failed to fetch orders for year",
				"year", year,
				"error", err,
			)
			continue
		}

		// Filter by date range if specified
		for _, s := range summaries {
			if c.isWithinDateRange(s.Date, opts) {
				allSummaries = append(allSummaries, s)
			}
		}

		// Check if we have enough orders
		if opts.MaxOrders > 0 && len(allSummaries) >= opts.MaxOrders {
			allSummaries = allSummaries[:opts.MaxOrders]
			break
		}
	}

	return allSummaries, nil
}

// determineYears returns the list of years to fetch based on options
func (c *Client) determineYears(opts FetchOptions) []int {
	if opts.Year > 0 {
		return []int{opts.Year}
	}

	currentYear := time.Now().Year()

	// If date range is specified, calculate years in range
	if !opts.StartDate.IsZero() && !opts.EndDate.IsZero() {
		startYear := opts.StartDate.Year()
		endYear := opts.EndDate.Year()

		var years []int
		for y := endYear; y >= startYear; y-- {
			years = append(years, y)
		}
		return years
	}

	// Default: current year only
	if !opts.EndDate.IsZero() {
		return []int{opts.EndDate.Year()}
	}

	return []int{currentYear}
}

// isWithinDateRange checks if a date is within the specified range
func (c *Client) isWithinDateRange(date time.Time, opts FetchOptions) bool {
	if date.IsZero() {
		return true // Include orders with unknown dates
	}

	if !opts.StartDate.IsZero() && date.Before(opts.StartDate) {
		return false
	}

	if !opts.EndDate.IsZero() && date.After(opts.EndDate) {
		return false
	}

	return true
}

// fetchYearOrders fetches all orders for a specific year
func (c *Client) fetchYearOrders(year int, parser *Parser, opts FetchOptions) ([]*OrderSummary, error) {
	var allSummaries []*OrderSummary
	startIndex := 0
	pageSize := 10 // Amazon typically shows 10 orders per page

	for {
		// Build URL with pagination
		orderURL := c.buildOrderListURL(year, startIndex)

		c.logger.Debug("fetching order list page",
			"year", year,
			"startIndex", startIndex,
			"url", orderURL,
		)

		resp, err := c.get(orderURL)
		if err != nil {
			return allSummaries, fmt.Errorf("failed to fetch orders: %w", err)
		}

		summaries, err := parser.ParseOrderList(resp.Body)
		resp.Body.Close()

		if err != nil {
			return allSummaries, fmt.Errorf("failed to parse order list: %w", err)
		}

		// No more orders found
		if len(summaries) == 0 {
			break
		}

		allSummaries = append(allSummaries, summaries...)

		// Check if we've hit the limit
		if opts.MaxOrders > 0 && len(allSummaries) >= opts.MaxOrders {
			break
		}

		// Check if we got a full page (more pages likely available)
		if len(summaries) < pageSize {
			break
		}

		startIndex += pageSize
	}

	return allSummaries, nil
}

// buildOrderListURL builds the URL for the order list page
func (c *Client) buildOrderListURL(year int, startIndex int) string {
	u, _ := url.Parse(ordersURL)
	q := u.Query()
	q.Set("timeFilter", fmt.Sprintf("year-%d", year))
	if startIndex > 0 {
		q.Set("startIndex", strconv.Itoa(startIndex))
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// fetchOrderDetails fetches and parses a single order's details
func (c *Client) fetchOrderDetails(orderID string, parser *Parser) (*Order, error) {
	u, _ := url.Parse(orderDetailsURL)
	q := u.Query()
	q.Set("orderID", orderID)
	u.RawQuery = q.Encode()

	resp, err := c.get(u.String())
	if err != nil {
		return nil, fmt.Errorf("failed to fetch order details: %w", err)
	}
	defer resp.Body.Close()

	order, err := parser.ParseOrderDetails(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse order details: %w", err)
	}

	// Ensure order ID is set
	if order.ID == "" {
		order.ID = orderID
	}

	return order, nil
}

// GetOrderYears returns the list of years that have orders
func (c *Client) GetOrderYears(ctx context.Context) ([]int, error) {
	// Fetch the order list page and parse available year filters
	resp, err := c.get(ordersURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch orders page: %w", err)
	}
	defer resp.Body.Close()

	// For now, return reasonable defaults
	// Could enhance to parse the year dropdown from the page
	currentYear := time.Now().Year()
	years := make([]int, 0, 5)
	for y := currentYear; y >= currentYear-4; y-- {
		years = append(years, y)
	}

	return years, nil
}

// FetchTransactions fetches payment transactions for an order
// This returns the actual charges made to payment methods
func (c *Client) FetchTransactions(ctx context.Context, orderID string) ([]*Transaction, error) {
	parser := NewParser()

	// Build transactions URL
	u, _ := url.Parse(transactionsURL)
	q := u.Query()
	q.Set("transactionTag", orderID)
	u.RawQuery = q.Encode()

	c.logger.Debug("fetching transactions", "orderID", orderID, "url", u.String())

	resp, err := c.get(u.String())
	if err != nil {
		return nil, fmt.Errorf("failed to fetch transactions: %w", err)
	}
	defer resp.Body.Close()

	transactions, err := parser.ParseTransactions(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse transactions: %w", err)
	}

	// Ensure order ID is set on all transactions
	for _, tx := range transactions {
		if tx.OrderID == "" {
			tx.OrderID = orderID
		}
	}

	c.logger.Debug("parsed transactions", "orderID", orderID, "count", len(transactions))

	return transactions, nil
}

// FetchOrderWithTransactions fetches an order with its payment transactions
func (c *Client) FetchOrderWithTransactions(ctx context.Context, orderID string) (*Order, []*Transaction, error) {
	parser := NewParser()

	// Fetch order details
	order, err := c.fetchOrderDetails(orderID, parser)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch order: %w", err)
	}

	// Fetch transactions
	transactions, err := c.FetchTransactions(ctx, orderID)
	if err != nil {
		c.logger.Warn("failed to fetch transactions", "orderID", orderID, "error", err)
		// Return order without transactions if transaction fetch fails
		return order, nil, nil
	}

	return order, transactions, nil
}

// FetchAllTransactions fetches transactions for multiple orders
func (c *Client) FetchAllTransactions(ctx context.Context, orderIDs []string) (map[string][]*Transaction, error) {
	result := make(map[string][]*Transaction)

	for _, orderID := range orderIDs {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		transactions, err := c.FetchTransactions(ctx, orderID)
		if err != nil {
			c.logger.Warn("failed to fetch transactions for order",
				"orderID", orderID,
				"error", err,
			)
			continue
		}

		result[orderID] = transactions
	}

	return result, nil
}
