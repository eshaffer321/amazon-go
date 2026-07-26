package amazon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExtractFromCurl(t *testing.T) {
	curlCmd := `curl 'https://www.amazon.com/your-orders/orders' -b 'session-id=123-456; ubid-main=abc-def; at-main=Atza|token'`

	cookies, err := ExtractFromCurl(curlCmd)
	if err != nil {
		t.Fatalf("ExtractFromCurl failed: %v", err)
	}

	if len(cookies) != 3 {
		t.Errorf("Expected 3 cookies, got %d", len(cookies))
	}

	// Check specific cookies
	cookieMap := make(map[string]string)
	for _, c := range cookies {
		cookieMap[c.Name] = c.Value
	}

	if cookieMap["session-id"] != "123-456" {
		t.Errorf("Expected session-id=123-456, got %s", cookieMap["session-id"])
	}
}

func TestExtractFromCurl_CookieHeader(t *testing.T) {
	curlCmd := `curl 'https://www.amazon.com/gp/css/order-history?disableCsd=no-js' -H 'accept: text/html' -H 'cookie: session-id=123-456; ubid-main=abc-def; at-main=Atza|token'`

	cookies, err := ExtractFromCurl(curlCmd)
	if err != nil {
		t.Fatalf("ExtractFromCurl failed: %v", err)
	}

	if len(cookies) != 3 {
		t.Errorf("Expected 3 cookies, got %d", len(cookies))
	}

	if cookies[0].Name != "session-id" {
		t.Errorf("Expected first cookie to be session-id, got %s", cookies[0].Name)
	}
}

func TestExtractFromCookieHeader(t *testing.T) {
	cookies, err := ExtractFromCookieHeader("Cookie: session-id=123-456; ubid-main=abc-def")
	if err != nil {
		t.Fatalf("ExtractFromCookieHeader failed: %v", err)
	}

	if len(cookies) != 2 {
		t.Errorf("Expected 2 cookies, got %d", len(cookies))
	}

	if cookies[1].Name != "ubid-main" {
		t.Errorf("Expected second cookie to be ubid-main, got %s", cookies[1].Name)
	}
}

func TestExtractFromCurl_NoMatch(t *testing.T) {
	curlCmd := `curl 'https://www.amazon.com/your-orders/orders'`

	_, err := ExtractFromCurl(curlCmd)
	if err == nil {
		t.Error("Expected error for curl command without cookies")
	}
}

func TestCookieStore(t *testing.T) {
	// Create temp file
	tmpDir := t.TempDir()
	cookieFile := filepath.Join(tmpDir, "cookies.json")

	store, err := NewCookieStore(cookieFile)
	if err != nil {
		t.Fatalf("NewCookieStore failed: %v", err)
	}

	// Add cookies
	store.Set(&Cookie{Name: "test-cookie", Value: "test-value", Domain: ".amazon.com"})
	store.Set(&Cookie{Name: "another-cookie", Value: "another-value", Domain: ".amazon.com"})

	if store.Count() != 2 {
		t.Errorf("Expected 2 cookies, got %d", store.Count())
	}

	// Get cookie
	c := store.Get("test-cookie")
	if c == nil {
		t.Fatal("Expected to get test-cookie")
	}
	if c.Value != "test-value" {
		t.Errorf("Expected value 'test-value', got '%s'", c.Value)
	}

	// Save
	if err := store.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(cookieFile); os.IsNotExist(err) {
		t.Error("Cookie file should exist after save")
	}

	// Create new store and load
	store2, err := NewCookieStore(cookieFile)
	if err != nil {
		t.Fatalf("NewCookieStore (load) failed: %v", err)
	}

	if store2.Count() != 2 {
		t.Errorf("Expected 2 cookies after load, got %d", store2.Count())
	}

	c2 := store2.Get("test-cookie")
	if c2 == nil || c2.Value != "test-value" {
		t.Error("Cookie not properly loaded")
	}
}

func TestCookieStore_ToHTTPCookies(t *testing.T) {
	tmpDir := t.TempDir()
	cookieFile := filepath.Join(tmpDir, "cookies.json")

	store, _ := NewCookieStore(cookieFile)
	store.Set(&Cookie{Name: "session-id", Value: "123", Domain: ".amazon.com", Path: "/"})
	store.Set(&Cookie{Name: "ubid-main", Value: "456", Domain: ".amazon.com", Path: "/"})

	httpCookies := store.ToHTTPCookies()
	if len(httpCookies) != 2 {
		t.Errorf("Expected 2 HTTP cookies, got %d", len(httpCookies))
	}
}

func TestCookieStore_ToHTTPCookiesUnquotesBrowserCookieValues(t *testing.T) {
	cookieFile := filepath.Join(t.TempDir(), "cookies.json")
	store, err := NewCookieStore(cookieFile)
	if err != nil {
		t.Fatalf("NewCookieStore failed: %v", err)
	}
	store.Set(&Cookie{Name: "x-main", Value: `"quoted-browser-value"`, Domain: ".amazon.com", Path: "/"})

	cookies := store.ToHTTPCookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one cookie, got %d", len(cookies))
	}
	if cookies[0].Value != "quoted-browser-value" {
		t.Fatalf("cookie value = %q, want unquoted browser value", cookies[0].Value)
	}
}

func TestCookieStoreReplacePersistsFingerprintAndDropsExpiredCookies(t *testing.T) {
	cookieFile := filepath.Join(t.TempDir(), "cookies.json")
	store, err := NewCookieStore(cookieFile)
	if err != nil {
		t.Fatalf("NewCookieStore failed: %v", err)
	}
	store.Set(&Cookie{Name: "stale", Value: "old", Domain: ".amazon.com", Path: "/"})

	fingerprint := BrowserFingerprint{
		UserAgent:       "Mozilla/5.0 Chrome/143.0.7499.4 Safari/537.36",
		SecCHUA:         `"Chromium";v="143", "Not A(Brand";v="24"`,
		SecCHUAMobile:   "?0",
		SecCHUAPlatform: `"macOS"`,
	}
	store.Replace([]*Cookie{
		{Name: "session-id", Value: "active", Domain: ".amazon.com", Path: "/"},
		{Name: "ak_bmsc", Value: "expired", Domain: ".amazon.com", Path: "/", Expires: time.Now().Add(-time.Hour).Unix()},
	})
	store.SetBrowserFingerprint(fingerprint)
	if err := store.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	data, err := os.ReadFile(cookieFile)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	var saved CookieFile
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if len(saved.Cookies) != 1 || saved.Cookies[0].Name != "session-id" {
		t.Fatalf("expected only the active replacement cookie, got %#v", saved.Cookies)
	}
	if saved.BrowserFingerprint == nil || saved.BrowserFingerprint.UserAgent != fingerprint.UserAgent {
		t.Fatalf("fingerprint was not persisted: %#v", saved.BrowserFingerprint)
	}

	loaded, err := NewCookieStore(cookieFile)
	if err != nil {
		t.Fatalf("NewCookieStore reload failed: %v", err)
	}
	if loaded.Get("stale") != nil || loaded.Get("ak_bmsc") != nil {
		t.Fatal("stale or expired cookies survived replacement")
	}
	if got := loaded.BrowserFingerprint(); got == nil || got.SecCHUA != fingerprint.SecCHUA {
		t.Fatalf("fingerprint was not loaded: %#v", got)
	}
}

func TestHasEssentialCookies(t *testing.T) {
	tmpDir := t.TempDir()
	cookieFile := filepath.Join(tmpDir, "cookies.json")

	store, _ := NewCookieStore(cookieFile)

	// Initially should not have essential cookies
	if store.HasEssentialCookies() {
		t.Error("Should not have essential cookies initially")
	}

	// Add essential cookies
	store.Set(&Cookie{Name: "session-id", Value: "123"})
	store.Set(&Cookie{Name: "session-token", Value: "abc"})
	store.Set(&Cookie{Name: "ubid-main", Value: "def"})
	store.Set(&Cookie{Name: "at-main", Value: "ghi"})

	if !store.HasEssentialCookies() {
		t.Error("Should have essential cookies after adding them")
	}
}

func TestImportFromCurl(t *testing.T) {
	tmpDir := t.TempDir()
	cookieFile := filepath.Join(tmpDir, "cookies.json")

	store, _ := NewCookieStore(cookieFile)

	curlCmd := `curl 'https://www.amazon.com/' -b 'session-id=test123; ubid-main=xyz789'`

	if err := store.ImportFromCurl(curlCmd); err != nil {
		t.Fatalf("ImportFromCurl failed: %v", err)
	}

	// Verify cookies were imported
	if store.Count() != 2 {
		t.Errorf("Expected 2 cookies, got %d", store.Count())
	}

	// Verify file was saved
	if _, err := os.Stat(cookieFile); os.IsNotExist(err) {
		t.Error("Cookie file should exist after import")
	}
}

func TestImportFromCookieHeader(t *testing.T) {
	tmpDir := t.TempDir()
	cookieFile := filepath.Join(tmpDir, "cookies.json")

	store, _ := NewCookieStore(cookieFile)

	if err := store.ImportFromCookieHeader("Cookie: session-id=test123; ubid-main=xyz789"); err != nil {
		t.Fatalf("ImportFromCookieHeader failed: %v", err)
	}

	if store.Count() != 2 {
		t.Errorf("Expected 2 cookies, got %d", store.Count())
	}

	if _, err := os.Stat(cookieFile); os.IsNotExist(err) {
		t.Error("Cookie file should exist after import")
	}
}

func TestEssentialCookies(t *testing.T) {
	essential := EssentialCookies()
	if len(essential) == 0 {
		t.Error("Expected at least one essential cookie")
	}

	// Check for known essential cookies
	found := make(map[string]bool)
	for _, name := range essential {
		found[name] = true
	}

	requiredCookies := []string{"session-id", "session-token", "ubid-main", "at-main"}
	for _, name := range requiredCookies {
		if !found[name] {
			t.Errorf("Expected %s to be in essential cookies", name)
		}
	}
}
