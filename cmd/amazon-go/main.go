package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	amazon "github.com/eshaffer321/amazon-go"
)

const amazonOrdersURL = "https://www.amazon.com/gp/css/order-history?disableCsd=no-js"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "import-browser-profile":
		importBrowserProfile(os.Args[2:])
	case "check-auth":
		checkAuth(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `Usage:
  amazon-go import-browser-profile [flags]
  amazon-go check-auth [flags]

Commands:
  import-browser-profile  Export Amazon cookies from a Playwright/Chromium browser profile.
  check-auth              Verify saved cookies can access Amazon order history.

Examples:
  amazon-go import-browser-profile -profile-dir "$HOME/.itemize/amazon/amazon-erick" -account erick
  amazon-go check-auth -account erick

`)
}

func importBrowserProfile(args []string) {
	fs := flag.NewFlagSet("import-browser-profile", flag.ExitOnError)
	profileDir := fs.String("profile-dir", "", "Path to a Playwright/Chromium user data directory")
	cookieFile := fs.String("cookie-file", "", "Path to amazon-go cookie file")
	account := fs.String("account", "", "Account name for ~/.amazon-go/cookies-{account}.json")
	playwrightRoot := fs.String("playwright-root", "", "Directory containing node_modules/playwright (auto-detected when omitted)")
	headless := fs.Bool("headless", false, "Run browser profile export headlessly")
	skipAuthCheck := fs.Bool("skip-auth-check", false, "Skip validating imported cookies")
	fs.Parse(args)

	if *profileDir == "" {
		log.Fatal("-profile-dir is required")
	}
	if *account == "" && *cookieFile == "" {
		log.Fatal("set -account or -cookie-file so the imported cookies have a destination")
	}

	resolvedProfileDir, err := expandPath(*profileDir)
	if err != nil {
		log.Fatalf("Failed to resolve profile dir: %v", err)
	}
	if info, err := os.Stat(resolvedProfileDir); err != nil || !info.IsDir() {
		if err == nil {
			err = fmt.Errorf("not a directory")
		}
		log.Fatalf("Invalid profile dir %q: %v", resolvedProfileDir, err)
	}

	root, err := resolvePlaywrightRoot(*playwrightRoot)
	if err != nil {
		log.Fatalf("Failed to find Playwright: %v", err)
	}

	cookies, title, err := exportCookiesWithPlaywright(root, resolvedProfileDir, *headless)
	if err != nil {
		log.Fatalf("Failed to export cookies from browser profile: %v", err)
	}
	if len(cookies) == 0 {
		log.Fatal("No Amazon cookies were available in the browser profile")
	}

	client, err := newClient(*cookieFile, *account)
	if err != nil {
		log.Fatalf("Failed to create Amazon client: %v", err)
	}
	for _, c := range cookies {
		client.CookieStore().Set(c)
	}
	if !*skipAuthCheck {
		if err := client.HealthCheck(); err != nil {
			log.Fatalf("Exported %d cookies from %q, but auth check failed: %v", len(cookies), resolvedProfileDir, err)
		}
	}
	if err := client.SaveCookies(); err != nil {
		log.Fatalf("Failed to save cookies: %v", err)
	}

	destination := *cookieFile
	if destination == "" {
		destination = "account " + *account
	}
	fmt.Printf("Imported %d Amazon cookies from %q (%s) into %s.\n", len(cookies), resolvedProfileDir, title, destination)
	if !*skipAuthCheck {
		fmt.Println("Auth check passed.")
	}
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

type exportedCookies struct {
	Title   string          `json:"title"`
	Cookies []browserCookie `json:"cookies"`
}

type browserCookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Expires  float64 `json:"expires"`
	Secure   bool    `json:"secure"`
	HttpOnly bool    `json:"httpOnly"`
}

func exportCookiesWithPlaywright(playwrightRoot, profileDir string, headless bool) ([]*amazon.Cookie, string, error) {
	scriptPath, err := writePlaywrightExportScript(playwrightRoot)
	if err != nil {
		return nil, "", err
	}
	defer os.Remove(scriptPath)

	cmd := exec.Command("node", scriptPath, profileDir, amazonOrdersURL, fmt.Sprintf("%t", headless))
	cmd.Dir = playwrightRoot
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, "", fmt.Errorf("node/playwright failed: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, "", err
	}

	var exported exportedCookies
	if err := json.Unmarshal(out, &exported); err != nil {
		return nil, "", fmt.Errorf("failed to parse Playwright cookie export: %w", err)
	}

	cookies := make([]*amazon.Cookie, 0, len(exported.Cookies))
	for _, c := range exported.Cookies {
		if c.Name == "" || c.Value == "" || !domainMatchesAmazon(c.Domain) {
			continue
		}
		cookies = append(cookies, &amazon.Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Expires:  int64(c.Expires),
			Secure:   c.Secure,
			HttpOnly: c.HttpOnly,
		})
	}

	return cookies, exported.Title, nil
}

func writePlaywrightExportScript(playwrightRoot string) (string, error) {
	script := `const { chromium } = require("playwright");

async function main() {
  const profileDir = process.argv[2];
  const ordersURL = process.argv[3];
  const headless = process.argv[4] === "true";
  const options = {
    headless,
    viewport: { width: 1280, height: 800 },
    locale: "en-US"
  };
  if (headless) {
    const version = await chromiumVersion();
    options.userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/" + version + " Safari/537.36";
    options.extraHTTPHeaders = {
      "sec-ch-ua": "\"Chromium\";v=\"" + version.split(".")[0] + "\", \"Not A(Brand\";v=\"24\"",
      "sec-ch-ua-platform": "\"macOS\""
    };
  }
  const context = await chromium.launchPersistentContext(profileDir, options);
  if (headless) {
    await context.addInitScript(() => {
      Object.defineProperty(navigator, "webdriver", { get: () => undefined });
      Object.defineProperty(navigator, "languages", { get: () => ["en-US", "en"] });
      Object.defineProperty(navigator, "plugins", { get: () => [1, 2, 3, 4, 5] });
    });
  }
  try {
    const page = await context.newPage();
    await page.goto(ordersURL, { waitUntil: "domcontentloaded", timeout: 30000 });
    await page.waitForTimeout(2000);
    const state = await page.evaluate(() => {
      const body = document.body ? document.body.innerText : "";
      const login = !!document.querySelector("input#ap_email,input[name='email'],input#ap_password,input[name='password']") ||
        /amazon sign-in/i.test(document.title);
      const ready = !login && (body.includes("Your Orders") || document.querySelectorAll(".js-order-card,.order-card").length > 0);
      return { login, ready, title: document.title };
    });
    if (!state.ready) {
      throw new Error("profile did not open an authenticated Amazon orders page (title=" + JSON.stringify(state.title) + ", login=" + state.login + ")");
    }
    const cookies = await context.cookies(["https://www.amazon.com", ordersURL]);
    const amazonCookies = cookies.filter(c => ["amazon.com", ".amazon.com", "www.amazon.com", ".www.amazon.com"].includes(c.domain));
    console.log(JSON.stringify({ title: await page.title(), cookies: amazonCookies }));
  } finally {
    await context.close();
  }
}

async function chromiumVersion() {
  const browser = await chromium.launch({ headless: true });
  try {
    return await browser.version();
  } finally {
    await browser.close();
  }
}

main().catch(err => {
  console.error(err && err.stack ? err.stack : String(err));
  process.exit(1);
});`

	f, err := os.CreateTemp(playwrightRoot, "amazon-go-import-*.cjs")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.WriteString(script); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func resolvePlaywrightRoot(explicit string) (string, error) {
	var candidates []string
	if explicit != "" {
		candidates = append(candidates, explicit)
	}

	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, cwd)
		for dir := cwd; ; {
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			candidates = append(candidates, parent)
			dir = parent
		}
	}

	if root, err := npmRoot("-g"); err == nil && root != "" {
		candidates = append(candidates, filepath.Dir(root), root)
	}

	if home, err := os.UserHomeDir(); err == nil {
		matches, _ := filepath.Glob(filepath.Join(home, ".npm", "_npx", "*", "node_modules", "playwright", "package.json"))
		sort.Strings(matches)
		for i := len(matches) - 1; i >= 0; i-- {
			candidates = append(candidates, filepath.Dir(filepath.Dir(filepath.Dir(matches[i]))))
		}
	}

	seen := make(map[string]bool)
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		candidate, _ = expandPath(candidate)
		if seen[candidate] {
			continue
		}
		seen[candidate] = true
		if hasPlaywright(candidate) {
			return candidate, nil
		}
		// Also accept the package directory itself.
		if filepath.Base(candidate) == "playwright" && fileExists(filepath.Join(candidate, "package.json")) {
			return filepath.Dir(filepath.Dir(candidate)), nil
		}
	}

	return "", fmt.Errorf("install Playwright with `npm install playwright` or pass -playwright-root pointing at a directory containing node_modules/playwright")
}

func npmRoot(args ...string) (string, error) {
	cmdArgs := append([]string{"root"}, args...)
	out, err := exec.Command("npm", cmdArgs...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func hasPlaywright(root string) bool {
	return fileExists(filepath.Join(root, "node_modules", "playwright", "package.json"))
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
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

func expandPath(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, path[2:])
	}
	return filepath.Abs(path)
}

func domainMatchesAmazon(domain string) bool {
	return domain == "amazon.com" || domain == ".amazon.com" ||
		domain == "www.amazon.com" || domain == ".www.amazon.com"
}
