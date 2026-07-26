package amazon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultCookieDir  = ".amazon-go"
	defaultCookieFile = "cookies.json"
)

// Cookie represents a browser cookie
type Cookie struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Domain   string `json:"domain"`
	Path     string `json:"path"`
	Expires  int64  `json:"expires,omitempty"`
	Secure   bool   `json:"secure,omitempty"`
	HttpOnly bool   `json:"httpOnly,omitempty"`
}

// BrowserFingerprint contains the browser identity headers associated with an
// authenticated cookie snapshot.
type BrowserFingerprint struct {
	UserAgent       string `json:"user_agent,omitempty"`
	SecCHUA         string `json:"sec_ch_ua,omitempty"`
	SecCHUAMobile   string `json:"sec_ch_ua_mobile,omitempty"`
	SecCHUAPlatform string `json:"sec_ch_ua_platform,omitempty"`
}

// CookieStore manages cookie persistence and retrieval
type CookieStore struct {
	cookies            map[string]*Cookie
	browserFingerprint *BrowserFingerprint
	filePath           string
	mu                 sync.RWMutex
}

// CookieFile represents the JSON structure for cookie storage
type CookieFile struct {
	Cookies            []*Cookie           `json:"cookies"`
	BrowserFingerprint *BrowserFingerprint `json:"browser_fingerprint,omitempty"`
	UpdatedAt          time.Time           `json:"updated_at"`
}

// NewCookieStore creates a new cookie store with the given file path
func NewCookieStore(filePath string) (*CookieStore, error) {
	store := &CookieStore{
		cookies:  make(map[string]*Cookie),
		filePath: filePath,
	}

	if err := store.Load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load cookies: %w", err)
	}

	return store, nil
}

// DefaultCookiePath returns the default cookie file path
func DefaultCookiePath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, defaultCookieDir, defaultCookieFile), nil
}

// CookiePathForAccount returns a cookie file path for a specific account
// This allows managing multiple Amazon accounts with separate cookie files
// Example: CookiePathForAccount("personal") -> ~/.amazon-go/cookies-personal.json
func CookiePathForAccount(accountName string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	filename := fmt.Sprintf("cookies-%s.json", accountName)
	return filepath.Join(homeDir, defaultCookieDir, filename), nil
}

// Load reads cookies from the file
func (s *CookieStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}

	var cookieFile CookieFile
	if err := json.Unmarshal(data, &cookieFile); err != nil {
		return fmt.Errorf("failed to parse cookies: %w", err)
	}

	s.cookies = make(map[string]*Cookie)
	for _, c := range cookieFile.Cookies {
		if c == nil || cookieExpired(c, time.Now()) {
			continue
		}
		s.cookies[cookieKey(c)] = c
	}
	s.browserFingerprint = cloneBrowserFingerprint(cookieFile.BrowserFingerprint)

	return nil
}

// Save writes cookies to the file
func (s *CookieStore) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cookies := make([]*Cookie, 0, len(s.cookies))
	for _, c := range s.cookies {
		if c == nil || cookieExpired(c, time.Now()) {
			continue
		}
		cookies = append(cookies, c)
	}
	sortCookies(cookies)

	cookieFile := CookieFile{
		Cookies:            cookies,
		BrowserFingerprint: cloneBrowserFingerprint(s.browserFingerprint),
		UpdatedAt:          time.Now(),
	}

	data, err := json.MarshalIndent(cookieFile, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cookies: %w", err)
	}

	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create cookie directory: %w", err)
	}

	if err := os.WriteFile(s.filePath, data, 0600); err != nil {
		return fmt.Errorf("failed to write cookies: %w", err)
	}

	return nil
}

// Get returns a cookie by name
func (s *CookieStore) Get(name string) *Cookie {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, cookie := range s.cookies {
		if cookie != nil && cookie.Name == name && !cookieExpired(cookie, time.Now()) {
			return cookie
		}
	}
	return nil
}

// Set adds or updates a cookie
func (s *CookieStore) Set(cookie *Cookie) {
	if cookie == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := cookieKey(cookie)
	if cookieExpired(cookie, time.Now()) {
		delete(s.cookies, key)
		return
	}
	s.cookies[key] = cookie
}

// Replace replaces the complete cookie snapshot while retaining the currently
// configured browser fingerprint.
func (s *CookieStore) Replace(cookies []*Cookie) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cookies = make(map[string]*Cookie, len(cookies))
	now := time.Now()
	for _, cookie := range cookies {
		if cookie == nil || cookieExpired(cookie, now) {
			continue
		}
		s.cookies[cookieKey(cookie)] = cookie
	}
}

// SetBrowserFingerprint associates browser identity headers with this cookie
// snapshot. The fingerprint is persisted by Save.
func (s *CookieStore) SetBrowserFingerprint(fingerprint BrowserFingerprint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.browserFingerprint = cloneBrowserFingerprint(&fingerprint)
}

// BrowserFingerprint returns a copy of the saved browser identity, if present.
func (s *CookieStore) BrowserFingerprint() *BrowserFingerprint {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneBrowserFingerprint(s.browserFingerprint)
}

// GetAll returns all cookies
func (s *CookieStore) GetAll() map[string]*Cookie {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]*Cookie, len(s.cookies))
	for _, cookie := range s.cookies {
		if cookie == nil || cookieExpired(cookie, time.Now()) {
			continue
		}
		if _, exists := result[cookie.Name]; !exists {
			result[cookie.Name] = cookie
		}
	}
	return result
}

// Count returns the number of cookies
func (s *CookieStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.cookies)
}

// ToHTTPCookies converts stored cookies to http.Cookie slice
func (s *CookieStore) ToHTTPCookies() []*http.Cookie {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cookies := make([]*http.Cookie, 0, len(s.cookies))
	now := time.Now()
	for _, c := range s.cookies {
		if c == nil || cookieExpired(c, now) {
			continue
		}
		cookies = append(cookies, toHTTPCookie(c))
	}
	return cookies
}

// CookiesForURL returns unexpired cookies whose domain, path, and secure
// attributes allow them to be sent to the URL.
func (s *CookieStore) CookiesForURL(target *url.URL) []*http.Cookie {
	if target == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	host := strings.ToLower(target.Hostname())
	requestPath := target.EscapedPath()
	if requestPath == "" {
		requestPath = "/"
	}
	secure := strings.EqualFold(target.Scheme, "https")
	now := time.Now()
	cookies := make([]*http.Cookie, 0, len(s.cookies))
	for _, cookie := range s.cookies {
		if cookie == nil || cookieExpired(cookie, now) ||
			(cookie.Secure && !secure) ||
			!cookieDomainMatches(host, cookie.Domain) ||
			!cookiePathMatches(requestPath, cookie.Path) {
			continue
		}
		cookies = append(cookies, toHTTPCookie(cookie))
	}
	sort.Slice(cookies, func(i, j int) bool {
		if len(cookies[i].Path) != len(cookies[j].Path) {
			return len(cookies[i].Path) > len(cookies[j].Path)
		}
		return cookies[i].Name < cookies[j].Name
	})
	return cookies
}

// UpdateFromResponse updates cookies from HTTP response headers
func (s *CookieStore) UpdateFromResponse(resp *http.Response) {
	if resp == nil {
		return
	}
	var requestURL *url.URL
	if resp.Request != nil {
		requestURL = resp.Request.URL
	}
	for _, cookie := range resp.Cookies() {
		domain := cookie.Domain
		path := cookie.Path
		if requestURL != nil {
			if domain == "" {
				domain = requestURL.Hostname()
			}
			if path == "" {
				path = defaultCookiePath(requestURL.Path)
			}
		}
		expires := int64(0)
		if !cookie.Expires.IsZero() {
			expires = cookie.Expires.Unix()
		}
		s.Set(&Cookie{
			Name:     cookie.Name,
			Value:    cookie.Value,
			Domain:   domain,
			Path:     path,
			Expires:  expires,
			Secure:   cookie.Secure,
			HttpOnly: cookie.HttpOnly,
		})
	}
}

func cookieKey(cookie *Cookie) string {
	return cookie.Name + "\x00" + strings.ToLower(cookie.Domain) + "\x00" + cookie.Path
}

func cookieExpired(cookie *Cookie, now time.Time) bool {
	return cookie.Expires > 0 && cookie.Expires <= now.Unix()
}

func toHTTPCookie(cookie *Cookie) *http.Cookie {
	value := cookie.Value
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		if unquoted, err := strconv.Unquote(value); err == nil {
			value = unquoted
		}
	}
	result := &http.Cookie{
		Name:     cookie.Name,
		Value:    value,
		Domain:   cookie.Domain,
		Path:     cookie.Path,
		Secure:   cookie.Secure,
		HttpOnly: cookie.HttpOnly,
	}
	if cookie.Expires > 0 {
		result.Expires = time.Unix(cookie.Expires, 0)
	}
	return result
}

func cookieDomainMatches(host, domain string) bool {
	domain = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(domain)), ".")
	return domain == "" || host == domain || strings.HasSuffix(host, "."+domain)
}

func cookiePathMatches(requestPath, cookiePath string) bool {
	if cookiePath == "" {
		cookiePath = "/"
	}
	if requestPath == cookiePath {
		return true
	}
	if !strings.HasPrefix(requestPath, cookiePath) {
		return false
	}
	return strings.HasSuffix(cookiePath, "/") ||
		(len(requestPath) > len(cookiePath) && requestPath[len(cookiePath)] == '/')
}

func defaultCookiePath(requestPath string) string {
	if requestPath == "" || requestPath[0] != '/' {
		return "/"
	}
	lastSlash := strings.LastIndex(requestPath, "/")
	if lastSlash <= 0 {
		return "/"
	}
	return requestPath[:lastSlash]
}

func cloneBrowserFingerprint(fingerprint *BrowserFingerprint) *BrowserFingerprint {
	if fingerprint == nil {
		return nil
	}
	copy := *fingerprint
	return &copy
}

func sortCookies(cookies []*Cookie) {
	sort.Slice(cookies, func(i, j int) bool {
		if cookies[i].Name != cookies[j].Name {
			return cookies[i].Name < cookies[j].Name
		}
		if cookies[i].Domain != cookies[j].Domain {
			return cookies[i].Domain < cookies[j].Domain
		}
		return cookies[i].Path < cookies[j].Path
	})
}

// ExtractFromCurl parses cookies from a curl command string
func ExtractFromCurl(curlCmd string) ([]*Cookie, error) {
	// Match -b or --cookie followed by the cookie string
	// Handles: -b 'cookies', -b "cookies", and -b cookies (unquoted until next flag or newline)

	// Try single-quoted first (most common from "Copy as cURL")
	// This allows double quotes inside the value (e.g., x-main="...")
	singleQuoteRegex := regexp.MustCompile(`(?:-b|--cookie)\s+'([^']+)'`)
	matches := singleQuoteRegex.FindStringSubmatch(curlCmd)

	if len(matches) >= 2 {
		return parseCookieString(matches[1]), nil
	}

	// Try double-quoted (allows single quotes inside)
	doubleQuoteRegex := regexp.MustCompile(`(?:-b|--cookie)\s+"([^"]+)"`)
	matches = doubleQuoteRegex.FindStringSubmatch(curlCmd)

	if len(matches) >= 2 {
		return parseCookieString(matches[1]), nil
	}

	// Try unquoted (stops at newline, backslash, or next -H flag)
	unquotedRegex := regexp.MustCompile(`(?:-b|--cookie)\s+([^\s\\]+(?:;[^\s\\]+)*)`)
	matches = unquotedRegex.FindStringSubmatch(curlCmd)

	if len(matches) >= 2 {
		return parseCookieString(matches[1]), nil
	}

	// Chrome/Arc "Copy as cURL" often emits the Cookie header as -H 'cookie: ...'
	// instead of using -b/--cookie.
	singleQuoteHeaderRegex := regexp.MustCompile(`(?i)-H\s+'cookie:\s*([^']+)'`)
	matches = singleQuoteHeaderRegex.FindStringSubmatch(curlCmd)

	if len(matches) >= 2 {
		return ExtractFromCookieHeader(matches[1])
	}

	doubleQuoteHeaderRegex := regexp.MustCompile(`(?i)-H\s+"cookie:\s*([^"]+)"`)
	matches = doubleQuoteHeaderRegex.FindStringSubmatch(curlCmd)

	if len(matches) >= 2 {
		return ExtractFromCookieHeader(matches[1])
	}

	return nil, fmt.Errorf("no cookies found in curl command")
}

// ExtractFromCookieHeader parses a raw Cookie header value into Cookie objects.
// It accepts either "a=b; c=d" or "Cookie: a=b; c=d".
func ExtractFromCookieHeader(header string) ([]*Cookie, error) {
	header = strings.TrimSpace(header)
	if strings.Contains(header, ":") {
		parts := strings.SplitN(header, ":", 2)
		if strings.EqualFold(strings.TrimSpace(parts[0]), "cookie") {
			header = strings.TrimSpace(parts[1])
		}
	}

	cookies := parseCookieString(header)
	if len(cookies) == 0 {
		return nil, fmt.Errorf("no cookies found in cookie header")
	}
	return cookies, nil
}

// parseCookieString parses a cookie header string into Cookie objects
func parseCookieString(cookieStr string) []*Cookie {
	var cookies []*Cookie

	pairs := strings.Split(cookieStr, ";")
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}

		idx := strings.Index(pair, "=")
		if idx == -1 {
			continue
		}

		name := strings.TrimSpace(pair[:idx])
		value := strings.TrimSpace(pair[idx+1:])

		cookies = append(cookies, &Cookie{
			Name:   name,
			Value:  value,
			Domain: ".amazon.com",
			Path:   "/",
		})
	}

	return cookies
}

// EssentialCookies returns the list of essential cookie names for Amazon
func EssentialCookies() []string {
	return []string{
		"session-id",
		"session-id-time",
		"session-token",
		"ubid-main",
		"at-main",
		"sess-at-main",
		"sst-main",
		"x-main",
		"lc-main",
		"i18n-prefs",
	}
}

// HasEssentialCookies checks if the store has the essential cookies for authentication
func (s *CookieStore) HasEssentialCookies() bool {
	essential := EssentialCookies()
	minRequired := 4 // At minimum need session-id, session-token, ubid-main, at-main

	count := 0
	for _, name := range essential {
		if s.Get(name) != nil {
			count++
		}
	}

	return count >= minRequired
}

// ImportFromCurl imports cookies from a curl command and saves them
func (s *CookieStore) ImportFromCurl(curlCmd string) error {
	cookies, err := ExtractFromCurl(curlCmd)
	if err != nil {
		return err
	}

	for _, c := range cookies {
		s.Set(c)
	}

	return s.Save()
}

// ImportFromCookieHeader imports cookies from a raw Cookie header and saves them.
func (s *CookieStore) ImportFromCookieHeader(header string) error {
	cookies, err := ExtractFromCookieHeader(header)
	if err != nil {
		return err
	}

	for _, c := range cookies {
		s.Set(c)
	}

	return s.Save()
}
