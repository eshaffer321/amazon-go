package amazon

import (
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// Parser handles HTML parsing for Amazon order pages
type Parser struct{}

// NewParser creates a new parser instance
func NewParser() *Parser {
	return &Parser{}
}

// ParseOrderList parses the order list page and returns order summaries
func (p *Parser) ParseOrderList(r io.Reader) ([]*OrderSummary, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	var orders []*OrderSummary

	// Find all order cards
	doc.Find(".order-card").Each(func(i int, s *goquery.Selection) {
		order, err := p.parseOrderCard(s)
		if err != nil {
			// Log error but continue parsing other orders
			return
		}
		orders = append(orders, order)
	})

	return orders, nil
}

// parseOrderCard extracts order summary from an order card element
func (p *Parser) parseOrderCard(s *goquery.Selection) (*OrderSummary, error) {
	order := &OrderSummary{}

	// Extract order ID from the order-id div or link
	// Look for order details link which contains the order ID
	detailLink := s.Find("a[href*='order-details']").First()
	if href, exists := detailLink.Attr("href"); exists {
		order.DetailURL = "https://www.amazon.com" + href
		// Extract order ID from URL
		if id := extractOrderIDFromURL(href); id != "" {
			order.ID = id
		}
	}

	// Fallback: try to find order ID in yohtmlc-order-id div
	if order.ID == "" {
		orderIDDiv := s.Find(".yohtmlc-order-id").First()
		orderIDText := strings.TrimSpace(orderIDDiv.Text())
		if id := extractOrderIDFromText(orderIDText); id != "" {
			order.ID = id
		}
	}

	// Extract date from order header
	// Look for the date text in the header list
	s.Find(".order-header__header-list-item").Each(func(i int, item *goquery.Selection) {
		text := strings.TrimSpace(item.Text())
		// Check if this looks like a date (contains month names)
		if containsMonth(text) {
			if date, err := parseAmazonDate(text); err == nil {
				order.Date = date
			}
		}
	})

	// Extract order total
	// Look for "Order Total" or total amount in header
	s.Find(".order-header__header-list-item").Each(func(i int, item *goquery.Selection) {
		text := strings.TrimSpace(item.Text())
		if strings.Contains(strings.ToLower(text), "total") || strings.HasPrefix(text, "$") {
			if price := parsePrice(text); price > 0 {
				order.Total = price
			}
		}
	})

	// Count items and extract item names
	s.Find(".item-box").Each(func(i int, item *goquery.Selection) {
		order.ItemCount++
		// Get item name from product title
		title := strings.TrimSpace(item.Find(".yohtmlc-product-title").Text())
		if title == "" {
			title = strings.TrimSpace(item.Find("a[href*='/dp/']").Text())
		}
		if title != "" {
			order.ItemNames = append(order.ItemNames, title)
		}
	})

	// Check for quantity indicators that might show multiple of same item
	s.Find(".product-image__qty").Each(func(i int, qty *goquery.Selection) {
		qtyText := strings.TrimSpace(qty.Text())
		if q := parseQuantity(qtyText); q > 1 {
			order.ItemCount += int(q) - 1
		}
	})

	return order, nil
}

// ParseOrderDetails parses the order details page and returns a full order
func (p *Parser) ParseOrderDetails(r io.Reader) (*Order, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	order := &Order{}

	// Extract order ID from the page
	doc.Find("span:contains('Order #'), bdi:contains('Order #')").Each(func(i int, s *goquery.Selection) {
		text := strings.TrimSpace(s.Text())
		if id := extractOrderIDFromText(text); id != "" {
			order.ID = id
		}
	})

	// Try to find order ID from URL in page content
	if order.ID == "" {
		doc.Find("a[href*='orderID=']").Each(func(i int, s *goquery.Selection) {
			if href, exists := s.Attr("href"); exists {
				if id := extractOrderIDFromURL(href); id != "" {
					order.ID = id
					return
				}
			}
		})
	}

	// Parse order summary section for pricing
	p.parseOrderSummary(doc, order)

	// Parse items from shipments
	p.parseShipmentItems(doc, order)

	return order, nil
}

// parseOrderSummary extracts pricing info from the order summary section
func (p *Parser) parseOrderSummary(doc *goquery.Document, order *Order) {
	// Find the order summary section
	doc.Find("#od-subtotals, [data-component='chargeSummary']").Each(func(i int, s *goquery.Selection) {
		s.Find(".od-line-item-row").Each(func(j int, row *goquery.Selection) {
			label := strings.ToLower(strings.TrimSpace(row.Find(".od-line-item-row-label").Text()))
			valueText := strings.TrimSpace(row.Find(".od-line-item-row-content").Text())
			value := parsePrice(valueText)

			switch {
			case strings.Contains(label, "item") && strings.Contains(label, "subtotal"):
				order.Subtotal = value
			case strings.Contains(label, "shipping") || strings.Contains(label, "handling"):
				order.ShippingFees = value
			case strings.Contains(label, "tax"):
				order.Tax = value
			case strings.Contains(label, "grand total"):
				order.Total = value
			}
		})
	})

	// Alternative parsing if above didn't work
	if order.Total == 0 {
		doc.Find("span:contains('Grand Total')").Each(func(i int, s *goquery.Selection) {
			// Find the next sibling or parent's next element with the price
			parent := s.Parent()
			priceText := parent.Next().Text()
			if price := parsePrice(priceText); price > 0 {
				order.Total = price
			}
		})
	}
}

// parseShipmentItems extracts items from shipment sections
func (p *Parser) parseShipmentItems(doc *goquery.Document, order *Order) {
	// Build a map of ASIN to item name by finding all title links first
	asinToName := make(map[string]string)
	doc.Find("a[href*='/dp/']").Each(func(i int, link *goquery.Selection) {
		href, exists := link.Attr("href")
		if !exists {
			return
		}
		asin := extractASINFromURL(href)
		if asin == "" {
			return
		}
		// Only use links with actual text (title links, not image links)
		text := strings.TrimSpace(link.Text())
		if text != "" && len(text) > 5 {
			asinToName[asin] = text
		}
	})

	// Find items in shipment sections
	seenASINs := make(map[string]bool)
	doc.Find("[data-component='shipments'], [data-component='shipmentsLeftGrid']").Each(func(i int, shipment *goquery.Selection) {
		// Each item is usually in a row with an image and title
		shipment.Find("a[href*='/dp/']").Each(func(j int, link *goquery.Selection) {
			href, exists := link.Attr("href")
			if !exists {
				return
			}

			asin := extractASINFromURL(href)
			if asin == "" || seenASINs[asin] {
				return
			}
			seenASINs[asin] = true

			item := &OrderItem{
				ASIN: asin,
				Name: asinToName[asin],
			}

			order.Items = append(order.Items, item)
		})
	})

	// Parse prices for items
	doc.Find("[data-component='unitPrice']").Each(func(i int, priceDiv *goquery.Selection) {
		priceText := priceDiv.Find(".a-offscreen").First().Text()
		if priceText == "" {
			priceText = priceDiv.Find(".a-price").Text()
		}
		price := parsePrice(priceText)

		// Associate price with item by index
		if i < len(order.Items) {
			order.Items[i].UnitPrice = price
			order.Items[i].Price = price
			if order.Items[i].Quantity == 0 {
				order.Items[i].Quantity = 1
			}
		}
	})

	// Parse quantities
	doc.Find("[data-component='quantity']").Each(func(i int, qtyDiv *goquery.Selection) {
		qtyText := strings.TrimSpace(qtyDiv.Text())
		qty := parseQuantity(qtyText)
		if qty > 0 && i < len(order.Items) {
			order.Items[i].Quantity = qty
			// Update line total
			order.Items[i].Price = order.Items[i].UnitPrice * qty
		}
	})

	// Set default quantity of 1 for items without explicit quantity
	for _, item := range order.Items {
		if item.Quantity == 0 {
			item.Quantity = 1
			item.Price = item.UnitPrice
		}
	}
}

// Helper functions

// extractOrderIDFromURL extracts order ID from a URL query parameter
func extractOrderIDFromURL(url string) string {
	// Match orderID=XXX-XXXXXXX-XXXXXXX
	re := regexp.MustCompile(`orderID=(\d{3}-\d{7}-\d{7})`)
	matches := re.FindStringSubmatch(url)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// extractOrderIDFromText extracts order ID from text content
func extractOrderIDFromText(text string) string {
	// Match XXX-XXXXXXX-XXXXXXX pattern
	re := regexp.MustCompile(`(\d{3}-\d{7}-\d{7})`)
	matches := re.FindStringSubmatch(text)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// extractASINFromURL extracts ASIN from an Amazon product URL
func extractASINFromURL(url string) string {
	// Match /dp/XXXXXXXXXX or asin=XXXXXXXXXX
	re := regexp.MustCompile(`(?:/dp/|asin=)([A-Z0-9]{10})`)
	matches := re.FindStringSubmatch(url)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// parsePrice extracts a price value from text like "$42.37" or "USD 42.37"
func parsePrice(text string) float64 {
	// Remove currency symbols and clean up
	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, "$", "")
	text = strings.ReplaceAll(text, "USD", "")
	text = strings.ReplaceAll(text, ",", "")
	text = strings.TrimSpace(text)

	// Extract first number pattern
	re := regexp.MustCompile(`(\d+\.?\d*)`)
	matches := re.FindStringSubmatch(text)
	if len(matches) > 1 {
		price, err := strconv.ParseFloat(matches[1], 64)
		if err == nil {
			return price
		}
	}
	return 0
}

// parseQuantity extracts quantity from text like "Qty: 2" or "x2"
func parseQuantity(text string) float64 {
	text = strings.ToLower(strings.TrimSpace(text))

	// Try patterns like "qty: 2", "qty:2", "x2", "2x"
	patterns := []string{
		`qty[:\s]*(\d+)`,
		`x(\d+)`,
		`(\d+)x`,
		`^(\d+)$`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(text)
		if len(matches) > 1 {
			qty, err := strconv.ParseFloat(matches[1], 64)
			if err == nil && qty > 0 {
				return qty
			}
		}
	}

	return 1 // Default to 1 if not found
}

// containsMonth checks if text contains a month name
func containsMonth(text string) bool {
	months := []string{
		"january", "february", "march", "april", "may", "june",
		"july", "august", "september", "october", "november", "december",
		"jan", "feb", "mar", "apr", "jun", "jul", "aug", "sep", "oct", "nov", "dec",
	}
	lower := strings.ToLower(text)
	for _, month := range months {
		if strings.Contains(lower, month) {
			return true
		}
	}
	return false
}

// parseAmazonDate parses various Amazon date formats
func parseAmazonDate(text string) (time.Time, error) {
	text = strings.TrimSpace(text)

	// Remove common prefixes
	prefixes := []string{"Ordered", "Order placed", "Placed on"}
	for _, prefix := range prefixes {
		text = strings.TrimPrefix(text, prefix)
		text = strings.TrimSpace(text)
	}

	// Common Amazon date formats
	formats := []string{
		"January 2, 2006",
		"Jan 2, 2006",
		"January 02, 2006",
		"Jan 02, 2006",
		"2 January 2006",
		"02 January 2006",
		"2006-01-02",
		"01/02/2006",
		"1/2/2006",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, text); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse date: %s", text)
}

// ParseTransactions parses the transactions page for an order
// Returns all payment transactions associated with the order
func (p *Parser) ParseTransactions(r io.Reader) ([]*Transaction, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	var transactions []*Transaction
	var currentStatus string
	var currentDate time.Time

	// Find status headers (e.g., "Completed", "Pending")
	doc.Find(".apx-transactions-sleeve-header-container").Each(func(j int, header *goquery.Selection) {
		statusText := strings.TrimSpace(header.Find(".a-text-bold").Text())
		if statusText != "" {
			currentStatus = statusText
		}
	})

	// Process date containers and their associated transactions
	// Each date container is followed by transaction line items
	doc.Find(".apx-transaction-date-container").Each(func(j int, dateDiv *goquery.Selection) {
		dateText := strings.TrimSpace(dateDiv.Find("span").Text())
		if dateText == "" {
			dateText = strings.TrimSpace(dateDiv.Text())
		}
		if date, err := parseAmazonDate(dateText); err == nil {
			currentDate = date
		}
	})

	// Look for transaction line items
	doc.Find(".apx-transactions-line-item-component-container").Each(func(j int, lineItem *goquery.Selection) {
		tx := &Transaction{
			Status: currentStatus,
			Date:   currentDate,
		}

		// Find the date by looking at the preceding date container
		// Walk up to find the parent section and then find date within it
		lineItem.PrevAll().Each(func(k int, prev *goquery.Selection) {
			if prev.HasClass("apx-transaction-date-container") {
				dateText := strings.TrimSpace(prev.Find("span").Text())
				if dateText == "" {
					dateText = strings.TrimSpace(prev.Text())
				}
				if date, err := parseAmazonDate(dateText); err == nil {
					tx.Date = date
				}
			}
		})

		// Extract payment method (e.g., "Prime Visa ****1211")
		lineItem.Find("[data-pmts-component-id]").Each(func(k int, row *goquery.Selection) {
			// Payment method is in the first column with bold text
			paymentMethod := strings.TrimSpace(row.Find(".a-column.a-span9 .a-text-bold").Text())
			if paymentMethod != "" {
				tx.PaymentMethod = paymentMethod
				tx.CardType, tx.LastFour = parsePaymentMethod(paymentMethod)
			}

			// Amount is in the last column with bold text (negative value like "-$44.91")
			amountText := strings.TrimSpace(row.Find(".a-column.a-span3 .a-text-bold").Text())
			if amountText != "" {
				// Remove the negative sign and parse
				amountText = strings.TrimPrefix(amountText, "-")
				tx.Amount = parsePrice(amountText)
			}

			// Look for order ID link
			row.Find("a[href*='orderID=']").Each(func(l int, link *goquery.Selection) {
				if href, exists := link.Attr("href"); exists {
					tx.OrderID = extractOrderIDFromURL(href)
				}
				// Also check link text for order ID
				linkText := strings.TrimSpace(link.Text())
				if tx.OrderID == "" && strings.Contains(linkText, "Order #") {
					tx.OrderID = extractOrderIDFromText(linkText)
				}
			})

			// Merchant name
			merchantText := strings.TrimSpace(row.Find(".a-column.a-span12 .a-size-base").Text())
			if merchantText != "" && !strings.Contains(merchantText, "Order #") {
				tx.Merchant = merchantText
			}
		})

		// Only add if we have meaningful data
		if tx.Amount > 0 || tx.PaymentMethod != "" {
			transactions = append(transactions, tx)
		}
	})

	// Alternative parsing if the above didn't find transactions
	if len(transactions) == 0 {
		transactions = p.parseTransactionsAlternative(doc)
	}

	return transactions, nil
}

// parseTransactionsAlternative tries alternative selectors for transaction parsing
func (p *Parser) parseTransactionsAlternative(doc *goquery.Document) []*Transaction {
	var transactions []*Transaction
	var currentDate time.Time
	var currentStatus string

	// Look for transaction sections more broadly
	doc.Find("h3:contains('Transactions from Order')").Each(func(i int, h3 *goquery.Selection) {
		text := h3.Text()
		orderID := extractOrderIDFromText(text)

		// Find the parent container and look for transactions within it
		container := h3.Parent().Parent()

		// Find status header
		container.Find(".a-box-title .a-text-bold").Each(func(j int, s *goquery.Selection) {
			currentStatus = strings.TrimSpace(s.Text())
		})

		// Find date
		container.Find("span").Each(func(j int, s *goquery.Selection) {
			text := strings.TrimSpace(s.Text())
			if containsMonth(text) && !strings.Contains(text, "Order") {
				if date, err := parseAmazonDate(text); err == nil {
					currentDate = date
				}
			}
		})

		// Find payment info by looking for card patterns
		container.Find(".a-text-bold").Each(func(j int, s *goquery.Selection) {
			text := strings.TrimSpace(s.Text())

			// Check if this looks like a payment method (contains **** for card)
			if strings.Contains(text, "****") {
				tx := &Transaction{
					OrderID:       orderID,
					Date:          currentDate,
					Status:        currentStatus,
					PaymentMethod: text,
				}
				tx.CardType, tx.LastFour = parsePaymentMethod(text)

				// Look for amount in sibling or nearby element
				parent := s.Parent()
				amountText := strings.TrimSpace(parent.Find(".a-span3 .a-text-bold, .a-text-right .a-text-bold").Text())
				if amountText != "" {
					amountText = strings.TrimPrefix(amountText, "-")
					tx.Amount = parsePrice(amountText)
				}

				if tx.Amount > 0 {
					transactions = append(transactions, tx)
				}
			}
		})
	})

	return transactions
}

// parsePaymentMethod extracts card type and last 4 digits from payment method string
// e.g., "Prime Visa ****1211" -> ("Visa", "1211")
func parsePaymentMethod(method string) (cardType, lastFour string) {
	// Extract last 4 digits after ****
	re := regexp.MustCompile(`\*{4}(\d{4})`)
	matches := re.FindStringSubmatch(method)
	if len(matches) > 1 {
		lastFour = matches[1]
	}

	// Extract card type
	methodLower := strings.ToLower(method)
	switch {
	case strings.Contains(methodLower, "visa"):
		cardType = "Visa"
	case strings.Contains(methodLower, "mastercard") || strings.Contains(methodLower, "master card"):
		cardType = "Mastercard"
	case strings.Contains(methodLower, "amex") || strings.Contains(methodLower, "american express"):
		cardType = "Amex"
	case strings.Contains(methodLower, "discover"):
		cardType = "Discover"
	case strings.Contains(methodLower, "gift card"):
		cardType = "Gift Card"
	case strings.Contains(methodLower, "debit"):
		cardType = "Debit"
	default:
		// Try to extract card type before the asterisks
		parts := strings.Split(method, "****")
		if len(parts) > 0 {
			cardType = strings.TrimSpace(parts[0])
		}
	}

	return cardType, lastFour
}
