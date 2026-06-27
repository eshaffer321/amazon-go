package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	amazon "github.com/eshaffer321/amazon-go"
)

const amazonOrdersURL = "https://www.amazon.com/gp/css/order-history?disableCsd=no-js"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "import-arc":
		importArc(os.Args[2:])
	case "setup-session":
		setupSession(os.Args[2:])
	case "check-auth":
		checkAuth(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `Usage:
  amazon-go setup-session [flags]
  amazon-go import-arc [flags]
  amazon-go check-auth [flags]

Commands:
  setup-session  Open a browser, wait for Amazon login if needed, then save cookies.
  import-arc     Import Amazon cookies directly from Arc's local cookie database.
  check-auth     Verify saved cookies can access Amazon order history.

`)
}

func importArc(args []string) {
	fs := flag.NewFlagSet("import-arc", flag.ExitOnError)
	profile := fs.String("profile", "", "Path to one Arc profile Cookies database; default scans all Arc profiles")
	userDataDir := fs.String("user-data-dir", defaultArcUserDataDir(), "Arc User Data directory")
	cookieFile := fs.String("cookie-file", "", "Path to amazon-go cookie file")
	account := fs.String("account", "", "Account name for ~/.amazon-go/cookies-{account}.json")
	skipAuthCheck := fs.Bool("skip-auth-check", false, "Skip validating imported cookies")
	fs.Parse(args)

	if *account != "" && *profile == "" {
		log.Fatal("-account requires -profile so multiple Arc profiles do not overwrite one cookie jar")
	}
	if *cookieFile != "" && *profile == "" {
		log.Fatal("-cookie-file requires -profile so multiple Arc profiles do not overwrite one cookie jar")
	}

	profiles, err := arcProfilesForImport(*userDataDir, *profile, *account)
	if err != nil {
		log.Fatalf("Failed to find Arc profiles: %v", err)
	}
	if len(profiles) == 0 {
		log.Fatalf("No Arc cookie databases found under %s", *userDataDir)
	}

	successes := 0
	firstAccount := ""
	for _, profile := range profiles {
		ok := importArcProfile(profile, *cookieFile, *skipAuthCheck)
		if ok {
			successes++
			if firstAccount == "" {
				firstAccount = profile.Account
			}
		}
	}
	if successes == 0 {
		log.Fatal("No Arc profiles produced a usable Amazon cookie jar")
	}

	if len(profiles) > 1 {
		fmt.Printf("Imported %d/%d Arc profiles. Fetch with -account, for example: go run ./examples/fetch_orders.go -account %s -year %d -details\n",
			successes, len(profiles), firstAccount, time.Now().Year())
	}
}

type arcProfile struct {
	Name        string
	Account     string
	CookiesPath string
}

func arcProfilesForImport(userDataDir, profilePath, account string) ([]arcProfile, error) {
	if profilePath != "" {
		name := filepath.Base(filepath.Dir(profilePath))
		if name == "." || name == string(filepath.Separator) {
			name = "Arc"
		}
		if account == "" {
			account = accountNameForArcProfile(name)
		}
		return []arcProfile{{
			Name:        name,
			Account:     account,
			CookiesPath: profilePath,
		}}, nil
	}

	profiles, err := discoverArcProfiles(userDataDir)
	if err != nil {
		return nil, err
	}
	for i := range profiles {
		profiles[i].Account = accountNameForArcProfile(profiles[i].Name)
	}
	return profiles, nil
}

func discoverArcProfiles(userDataDir string) ([]arcProfile, error) {
	matches, err := filepath.Glob(filepath.Join(userDataDir, "*", "Cookies"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)

	profiles := make([]arcProfile, 0, len(matches))
	for _, path := range matches {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		profiles = append(profiles, arcProfile{
			Name:        filepath.Base(filepath.Dir(path)),
			CookiesPath: path,
		})
	}
	return profiles, nil
}

func importArcProfile(profile arcProfile, cookieFile string, skipAuthCheck bool) bool {
	client, err := newClient(cookieFile, profile.Account)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Skipping %s: failed to create Amazon client: %v\n", profile.Name, err)
		return false
	}

	cookies, err := readArcAmazonCookies(profile.CookiesPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Skipping %s: failed to read Arc cookies: %v\n", profile.Name, err)
		return false
	}
	if len(cookies) == 0 {
		fmt.Fprintf(os.Stderr, "Skipping %s: no Amazon cookies found\n", profile.Name)
		return false
	}

	for _, c := range cookies {
		client.CookieStore().Set(c)
	}
	if !skipAuthCheck {
		if err := client.HealthCheck(); err != nil {
			fmt.Fprintf(os.Stderr, "Skipping %s: found %d Amazon cookies for account %q, but auth check failed: %v\n", profile.Name, len(cookies), profile.Account, err)
			return false
		}
	}
	if err := client.SaveCookies(); err != nil {
		fmt.Fprintf(os.Stderr, "Skipping %s: failed to save cookies: %v\n", profile.Name, err)
		return false
	}

	fmt.Printf("Imported %d Amazon cookies from Arc profile %q as account %q.\n", len(cookies), profile.Name, profile.Account)
	return true
}

func accountNameForArcProfile(name string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteByte('-')
			lastDash = true
		}
	}
	account := strings.Trim(b.String(), "-")
	if account == "" {
		account = "profile"
	}
	return "arc-" + account
}

func setupSession(args []string) {
	fs := flag.NewFlagSet("setup-session", flag.ExitOnError)
	browserPath := fs.String("browser", "", "Path to a Chromium browser executable")
	profileDir := fs.String("profile-dir", defaultBrowserProfileDir(), "Browser profile dir for Amazon login")
	cookieFile := fs.String("cookie-file", "", "Path to amazon-go cookie file")
	account := fs.String("account", "", "Account name for ~/.amazon-go/cookies-{account}.json")
	timeout := fs.Duration("timeout", 5*time.Minute, "How long to wait for Amazon login")
	fs.Parse(args)

	path := *browserPath
	if path == "" {
		var err error
		path, err = findBrowser()
		if err != nil {
			log.Fatalf("Could not find a Chromium browser. Pass -browser /path/to/browser: %v", err)
		}
	}

	client, err := newClient(*cookieFile, *account)
	if err != nil {
		log.Fatalf("Failed to create Amazon client: %v", err)
	}

	if err := os.MkdirAll(*profileDir, 0700); err != nil {
		log.Fatalf("Failed to create browser profile dir: %v", err)
	}

	fmt.Printf("Opening browser: %s\n", path)
	fmt.Printf("Using browser profile: %s\n", *profileDir)
	fmt.Println("Log into Amazon in the opened browser if prompted. This command will continue automatically.")

	allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(path),
		chromedp.UserDataDir(*profileDir),
		chromedp.Flag("headless", false),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
	)
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), allocOpts...)
	defer cancelAlloc()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	if err := chromedp.Run(ctx,
		network.Enable(),
		chromedp.Navigate(amazonOrdersURL),
	); err != nil {
		log.Fatalf("Failed to open Amazon order history: %v", err)
	}

	if err := waitForAmazonOrders(ctx, *timeout); err != nil {
		log.Fatalf("Amazon login did not complete: %v", err)
	}

	var browserCookies []*network.Cookie
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		browserCookies, err = network.GetCookies().WithURLs([]string{
			"https://www.amazon.com",
			amazonOrdersURL,
		}).Do(ctx)
		return err
	})); err != nil {
		log.Fatalf("Failed to read browser cookies: %v", err)
	}

	imported := 0
	for _, c := range browserCookies {
		if c.Domain == "" || !domainMatchesAmazon(c.Domain) {
			continue
		}
		client.CookieStore().Set(&amazon.Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Expires:  int64(c.Expires),
			Secure:   c.Secure,
			HttpOnly: c.HTTPOnly,
		})
		imported++
	}

	if imported == 0 {
		log.Fatal("No Amazon cookies were available after login")
	}
	if err := client.SaveCookies(); err != nil {
		log.Fatalf("Failed to save cookies: %v", err)
	}
	if err := client.HealthCheck(); err != nil {
		log.Fatalf("Saved cookies, but auth check failed: %v", err)
	}

	fmt.Printf("Saved %d Amazon cookies. Auth check passed.\n", imported)
}

func checkAuth(args []string) {
	fs := flag.NewFlagSet("check-auth", flag.ExitOnError)
	cookieFile := fs.String("cookie-file", "", "Path to amazon-go cookie file")
	account := fs.String("account", "", "Account name for ~/.amazon-go/cookies-{account}.json")
	fs.Parse(args)

	client, err := newClient(*cookieFile, *account)
	if err != nil {
		log.Fatalf("Failed to create Amazon client: %v", err)
	}
	if err := client.HealthCheck(); err != nil {
		log.Fatalf("Auth check failed: %v", err)
	}
	fmt.Println("Auth check passed.")
}

type arcCookieRow struct {
	HostKey        string `json:"host_key"`
	Name           string `json:"name"`
	Path           string `json:"path"`
	Value          string `json:"value"`
	EncryptedValue string `json:"encrypted_value"`
	ExpiresUTC     int64  `json:"expires_utc"`
	IsSecure       int    `json:"is_secure"`
	IsHTTPOnly     int    `json:"is_httponly"`
}

func readArcAmazonCookies(cookiesPath string) ([]*amazon.Cookie, error) {
	if _, err := os.Stat(cookiesPath); err != nil {
		return nil, err
	}

	tmp, err := os.CreateTemp("", "amazon-go-arc-cookies-*")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	data, err := os.ReadFile(cookiesPath)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return nil, err
	}

	query := `
		select
			host_key,
			name,
			path,
			value,
			hex(encrypted_value) as encrypted_value,
			expires_utc,
			is_secure,
			is_httponly
		from cookies
		where host_key like '%amazon.com'
	`
	out, err := exec.Command("sqlite3", "-json", tmpPath, query).Output()
	if err != nil {
		return nil, fmt.Errorf("sqlite3 failed: %w", err)
	}

	var rows []arcCookieRow
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("failed to parse sqlite3 JSON: %w", err)
	}

	key, err := arcCookieKey()
	if err != nil {
		return nil, err
	}

	cookies := make([]*amazon.Cookie, 0, len(rows))
	for _, row := range rows {
		value := row.Value
		if value == "" && row.EncryptedValue != "" {
			value, err = decryptChromiumCookie(row.EncryptedValue, key, row.HostKey)
			if err != nil {
				continue
			}
		}
		if value == "" || !domainMatchesAmazon(row.HostKey) {
			continue
		}
		cookies = append(cookies, &amazon.Cookie{
			Name:     row.Name,
			Value:    value,
			Domain:   row.HostKey,
			Path:     row.Path,
			Expires:  chromeTimeToUnix(row.ExpiresUTC),
			Secure:   row.IsSecure != 0,
			HttpOnly: row.IsHTTPOnly != 0,
		})
	}

	return cookies, nil
}

func arcCookieKey() ([]byte, error) {
	out, err := exec.Command("security", "find-generic-password", "-w", "-a", "Arc", "-s", "Arc Safe Storage").Output()
	if err != nil {
		return nil, fmt.Errorf("failed to read Arc Safe Storage from Keychain: %w", err)
	}
	return pbkdf2SHA1(strings.TrimSpace(string(out)), []byte("saltysalt"), 1003, 16), nil
}

func decryptChromiumCookie(encryptedHex string, key []byte, hostKey string) (string, error) {
	encrypted, err := hex.DecodeString(encryptedHex)
	if err != nil {
		return "", err
	}
	if len(encrypted) < 3 || string(encrypted[:3]) != "v10" {
		return "", fmt.Errorf("unsupported cookie encryption prefix")
	}
	ciphertext := encrypted[3:]
	if len(ciphertext)%aes.BlockSize != 0 {
		return "", fmt.Errorf("invalid cookie ciphertext size")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	iv := []byte("                ")
	plain := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, ciphertext)

	plain, err = pkcs7Unpad(plain, aes.BlockSize)
	if err != nil {
		return "", err
	}
	if len(plain) > sha256.Size {
		hostDigest := sha256.Sum256([]byte(hostKey))
		if hmac.Equal(plain[:sha256.Size], hostDigest[:]) {
			plain = plain[sha256.Size:]
		}
	}
	return string(plain), nil
}

func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, fmt.Errorf("invalid padded data")
	}
	pad := int(data[len(data)-1])
	if pad == 0 || pad > blockSize || pad > len(data) {
		return nil, fmt.Errorf("invalid padding")
	}
	for _, b := range data[len(data)-pad:] {
		if int(b) != pad {
			return nil, fmt.Errorf("invalid padding")
		}
	}
	return data[:len(data)-pad], nil
}

func pbkdf2SHA1(password string, salt []byte, iter, keyLen int) []byte {
	hLen := sha1.Size
	numBlocks := (keyLen + hLen - 1) / hLen
	dk := make([]byte, 0, numBlocks*hLen)
	for block := 1; block <= numBlocks; block++ {
		mac := hmac.New(sha1.New, []byte(password))
		mac.Write(salt)
		mac.Write([]byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)})
		u := mac.Sum(nil)
		t := append([]byte(nil), u...)
		for i := 1; i < iter; i++ {
			mac = hmac.New(sha1.New, []byte(password))
			mac.Write(u)
			u = mac.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		dk = append(dk, t...)
	}
	return dk[:keyLen]
}

func chromeTimeToUnix(chromeTime int64) int64 {
	if chromeTime <= 0 {
		return 0
	}
	return chromeTime/1_000_000 - 11644473600
}

func waitForAmazonOrders(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	printedLoginHint := false

	for time.Now().Before(deadline) {
		var state struct {
			Login bool   `json:"login"`
			Ready bool   `json:"ready"`
			URL   string `json:"url"`
			Title string `json:"title"`
		}
		err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
			const body = document.body ? document.body.innerText : "";
			const login = !!document.querySelector("input#ap_email,input[name='email'],input#ap_password,input[name='password']");
			const ready = !login && (body.includes("Your Orders") || document.querySelectorAll(".js-order-card,.order-card").length > 0);
			return { login, ready, url: location.href, title: document.title };
		})()`, &state))
		if err == nil && state.Ready {
			return nil
		}
		if err == nil && state.Login && !printedLoginHint {
			fmt.Println("Amazon is asking for login. Complete login/MFA in the browser window.")
			printedLoginHint = true
		}
		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("timed out after %s", timeout)
}

func newClient(cookieFile, account string) (*amazon.Client, error) {
	var opts []amazon.Option
	if cookieFile != "" {
		opts = append(opts, amazon.WithCookieFile(cookieFile))
	}
	if account != "" {
		opts = append(opts, amazon.WithAccount(account))
	}
	return amazon.NewClient(opts...)
}

func defaultBrowserProfileDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".amazon-go-browser-profile"
	}
	return filepath.Join(home, ".amazon-go", "browser-profile")
}

func defaultArcUserDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join("Arc", "User Data")
	}
	return filepath.Join(home, "Library", "Application Support", "Arc", "User Data")
}

func findBrowser() (string, error) {
	candidates := browserCandidates()
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("checked %v", candidates)
}

func browserCandidates() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/Applications/Arc.app/Contents/MacOS/Arc",
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}
	default:
		return []string{
			"google-chrome",
			"chromium",
			"chromium-browser",
			"microsoft-edge",
		}
	}
}

func domainMatchesAmazon(domain string) bool {
	return domain == "amazon.com" || domain == ".amazon.com" ||
		domain == "www.amazon.com" || domain == ".www.amazon.com"
}
