package amazon

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	baseURL           = "https://www.amazon.com"
	ordersURL         = baseURL + "/your-orders/orders"
	orderDetailsURL   = baseURL + "/your-orders/order-details"
	transactionsURL   = baseURL + "/cpe/yourpayments/transactions"
	defaultRateLimit  = 1 * time.Second
	defaultTimeout    = 30 * time.Second
	defaultMaxRetries = 3
)

// ClientConfig holds configuration options for the Amazon client
type ClientConfig struct {
	CookieFile  string
	AccountName string // For multi-account support (e.g., "personal", "work")
	RateLimit   time.Duration
	MaxRetries  int
	AutoSave    bool
	Logger      *slog.Logger
	UserAgent   string
}

// Client represents an Amazon client for fetching order data
type Client struct {
	httpClient  *http.Client
	cookieStore *CookieStore
	rateLimit   time.Duration
	maxRetries  int
	autoSave    bool
	logger      *slog.Logger
	userAgent   string
	lastRequest time.Time
	mu          sync.RWMutex
}

// Option is a function that configures the client
type Option func(*ClientConfig)

// WithCookieFile sets the cookie file path
func WithCookieFile(path string) Option {
	return func(c *ClientConfig) {
		c.CookieFile = path
	}
}

// WithRateLimit sets the rate limit between requests
func WithRateLimit(d time.Duration) Option {
	return func(c *ClientConfig) {
		c.RateLimit = d
	}
}

// WithMaxRetries sets the maximum number of retries for failed requests
func WithMaxRetries(n int) Option {
	return func(c *ClientConfig) {
		c.MaxRetries = n
	}
}

// WithAutoSave enables automatic cookie saving after each request
func WithAutoSave(enabled bool) Option {
	return func(c *ClientConfig) {
		c.AutoSave = enabled
	}
}

// WithLogger sets the logger for the client
func WithLogger(logger *slog.Logger) Option {
	return func(c *ClientConfig) {
		c.Logger = logger
	}
}

// WithUserAgent sets a custom user agent
func WithUserAgent(ua string) Option {
	return func(c *ClientConfig) {
		c.UserAgent = ua
	}
}

// WithAccount sets the account name for multi-account support
// Cookies will be stored in ~/.amazon-go/cookies-{accountName}.json
func WithAccount(name string) Option {
	return func(c *ClientConfig) {
		c.AccountName = name
	}
}

// NewClient creates a new Amazon client with the given options
func NewClient(opts ...Option) (*Client, error) {
	// Set defaults
	config := &ClientConfig{
		RateLimit:  defaultRateLimit,
		MaxRetries: defaultMaxRetries,
		AutoSave:   true,
		UserAgent:  "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	}

	// Apply options
	for _, opt := range opts {
		opt(config)
	}

	// Use default cookie path if not specified
	if config.CookieFile == "" {
		var path string
		var err error
		if config.AccountName != "" {
			path, err = CookiePathForAccount(config.AccountName)
		} else {
			path, err = DefaultCookiePath()
		}
		if err != nil {
			return nil, fmt.Errorf("failed to get cookie path: %w", err)
		}
		config.CookieFile = path
	}

	// Create cookie store
	cookieStore, err := NewCookieStore(config.CookieFile)
	if err != nil {
		return nil, fmt.Errorf("failed to create cookie store: %w", err)
	}

	// Create logger if not provided
	logger := config.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	// Create HTTP client with redirect handling
	httpClient := &http.Client{
		Timeout: defaultTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Follow redirects but preserve cookies
			return nil
		},
	}

	return &Client{
		httpClient:  httpClient,
		cookieStore: cookieStore,
		rateLimit:   config.RateLimit,
		maxRetries:  config.MaxRetries,
		autoSave:    config.AutoSave,
		logger:      logger.With("client", "amazon"),
		userAgent:   config.UserAgent,
	}, nil
}

// CookieStore returns the cookie store for manual cookie management
func (c *Client) CookieStore() *CookieStore {
	return c.cookieStore
}

// doRequest performs an HTTP request with rate limiting and retry logic
func (c *Client) doRequest(req *http.Request) (*http.Response, error) {
	c.mu.Lock()
	// Rate limiting
	if !c.lastRequest.IsZero() {
		elapsed := time.Since(c.lastRequest)
		if elapsed < c.rateLimit {
			time.Sleep(c.rateLimit - elapsed)
		}
	}
	c.lastRequest = time.Now()
	c.mu.Unlock()

	// Set headers
	c.setHeaders(req)

	// Set cookies
	c.setCookies(req)

	var resp *http.Response
	var err error

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			c.logger.Warn("retrying request",
				"attempt", attempt,
				"url", req.URL.String(),
			)
			time.Sleep(time.Duration(attempt) * time.Second)
		}

		resp, err = c.httpClient.Do(req)
		if err != nil {
			continue
		}

		// Check for rate limiting or auth errors
		if resp.StatusCode == http.StatusTooManyRequests {
			c.logger.Warn("rate limited, waiting before retry")
			resp.Body.Close()
			time.Sleep(5 * time.Second)
			continue
		}

		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
			resp.Body.Close()
			return nil, fmt.Errorf("authentication failed: cookies may be expired (status %d)", resp.StatusCode)
		}

		// Success
		break
	}

	if err != nil {
		return nil, fmt.Errorf("request failed after %d attempts: %w", c.maxRetries+1, err)
	}

	// Update cookies from response
	c.cookieStore.UpdateFromResponse(resp)

	// Auto-save cookies
	if c.autoSave {
		if err := c.cookieStore.Save(); err != nil {
			c.logger.Warn("failed to save cookies", "error", err)
		}
	}

	return resp, nil
}

// setHeaders sets common request headers
func (c *Client) setHeaders(req *http.Request) {
	headers := map[string]string{
		"accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8",
		"accept-language": "en-US,en;q=0.9",
		"cache-control":   "no-cache",
		"pragma":          "no-cache",
		"user-agent":      c.userAgent,
		"dnt":             "1",
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}
}

// setCookies sets cookies on the request from the cookie store
func (c *Client) setCookies(req *http.Request) {
	allCookies := c.cookieStore.GetAll()
	var cookiePairs []string

	for name, cookie := range allCookies {
		cookiePairs = append(cookiePairs, fmt.Sprintf("%s=%s", name, cookie.Value))
	}

	if len(cookiePairs) > 0 {
		req.Header.Set("Cookie", strings.Join(cookiePairs, "; "))
	}
}

// get performs a GET request to the given URL
func (c *Client) get(url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	return c.doRequest(req)
}

// HealthCheck verifies that the client can authenticate with Amazon
func (c *Client) HealthCheck() error {
	if !c.cookieStore.HasEssentialCookies() {
		return fmt.Errorf("missing essential cookies: please import cookies from a browser session")
	}

	// Try to fetch the orders page
	resp, err := c.get(ordersURL)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed: unexpected status %d", resp.StatusCode)
	}

	// Read body to check if we got the actual orders page or a login redirect
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("health check failed: could not read response: %w", err)
	}

	bodyStr := string(body)

	// Amazon returns 200 with login page when cookies are expired
	// Check for login form fields (ap_email input) - NOT just /ap/signin which appears in nav
	isLoginPage := (strings.Contains(bodyStr, "ap_email") || strings.Contains(bodyStr, "ap_password")) &&
		!strings.Contains(bodyStr, "order-card")

	if isLoginPage {
		return fmt.Errorf("authentication failed: cookies are expired, please re-import cookies from browser")
	}

	return nil
}

// SaveCookies saves cookies to the file
func (c *Client) SaveCookies() error {
	return c.cookieStore.Save()
}

// ImportCookiesFromCurl imports cookies from a curl command string
func (c *Client) ImportCookiesFromCurl(curlCmd string) error {
	return c.cookieStore.ImportFromCurl(curlCmd)
}
