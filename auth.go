package amazon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
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

// CookieStore manages cookie persistence and retrieval
type CookieStore struct {
	cookies  map[string]*Cookie
	filePath string
	mu       sync.RWMutex
}

// CookieFile represents the JSON structure for cookie storage
type CookieFile struct {
	Cookies   []*Cookie `json:"cookies"`
	UpdatedAt time.Time `json:"updated_at"`
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
		s.cookies[c.Name] = c
	}

	return nil
}

// Save writes cookies to the file
func (s *CookieStore) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cookies := make([]*Cookie, 0, len(s.cookies))
	for _, c := range s.cookies {
		cookies = append(cookies, c)
	}

	cookieFile := CookieFile{
		Cookies:   cookies,
		UpdatedAt: time.Now(),
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
	return s.cookies[name]
}

// Set adds or updates a cookie
func (s *CookieStore) Set(cookie *Cookie) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cookies[cookie.Name] = cookie
}

// GetAll returns all cookies
func (s *CookieStore) GetAll() map[string]*Cookie {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]*Cookie, len(s.cookies))
	for k, v := range s.cookies {
		result[k] = v
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
	for _, c := range s.cookies {
		httpCookie := &http.Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Secure:   c.Secure,
			HttpOnly: c.HttpOnly,
		}
		if c.Expires > 0 {
			httpCookie.Expires = time.Unix(c.Expires, 0)
		}
		cookies = append(cookies, httpCookie)
	}
	return cookies
}

// UpdateFromResponse updates cookies from HTTP response headers
func (s *CookieStore) UpdateFromResponse(resp *http.Response) {
	for _, cookie := range resp.Cookies() {
		s.Set(&Cookie{
			Name:     cookie.Name,
			Value:    cookie.Value,
			Domain:   cookie.Domain,
			Path:     cookie.Path,
			Expires:  cookie.Expires.Unix(),
			Secure:   cookie.Secure,
			HttpOnly: cookie.HttpOnly,
		})
	}
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

	return nil, fmt.Errorf("no cookies found in curl command")
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
