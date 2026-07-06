# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed
- Replaced the Arc-specific cookie importer and browser setup command with `import-browser-profile`, which exports cookies from an explicitly selected Chromium/Playwright user-data directory.
- Removed the `chromedp` dependency from the helper CLI; Playwright is used only as an external helper for browser-profile cookie export.
- `import-browser-profile` now opens headed Chromium by default for session renewal, supports `-headless` for automation, and normalizes the obvious headless browser fingerprint signals.
- The example fetcher no longer auto-discovers Arc-imported accounts.

### Fixed
- `import-browser-profile` now refuses to save cookies when the profile opens an Amazon sign-in page instead of an authenticated orders page.
- Printable order parsing now reads Amazon's image-overlay quantity marker, so repeated items are returned with the correct line total.

## [0.2.0] - 2026-06-27

### Added
- Added `cmd/amazon-go` helper CLI with commands for browser-assisted setup, Arc cookie import, and auth checking.
- Added macOS Arc profile discovery and cookie import, including multi-profile account storage like `cookies-arc-profile-1.json`.
- Added safer example flags for stdin/file cookie imports and account-specific fetching.
- Added CI quality gates, release workflow, pull request checklist, and documented release process.

### Changed
- Order scraping now uses Amazon's legacy no-JS order history and printable order detail pages instead of the encrypted modern `/your-orders` pages.
- A bare example fetch now tries all authenticated imported Arc accounts when no explicit account or cookie file is provided.
- README now documents the current setup, limitations, and release-ready usage flow.

### Fixed
- Fixed printable order detail fetching so the requested order ID is preserved.
- Improved parsing of legacy order cards, printable order detail pages, and Chromium cookie values with host digest prefixes.

## [0.1.0] - 2025-12-06

### Fixed
- **CRITICAL:** Fixed `ExtractFromCurl()` regex to handle cookies with quotes in values
  - Cookies like `x-main="..."` were being truncated because the regex `[^'"]+` stopped at the first quote inside the cookie string
  - Now uses separate regexes for single-quoted and double-quoted cookie strings, allowing the opposite quote type inside values
  - This was causing authentication failures when importing cookies from browser curl commands

- **HealthCheck()** now correctly detects expired cookies
  - Previously only checked for HTTP 200 status, but Amazon returns 200 with a login page when cookies are expired
  - Now checks for login form fields (`ap_email`/`ap_password`) and verifies order content is present
  - Fixed false positive that was checking for `/ap/signin` which appears in navigation on all Amazon pages

## [0.0.1] - 2025-11-28

### Added
- Initial release
- Cookie-based authentication with persistence to `~/.amazon-go/cookies.json`
- Multi-account support via `WithAccount()` option
- Order fetching with `FetchOrders()` and `FetchOrder()`
- Transaction fetching with `FetchTransactions()`
- HTML parsing for order lists, order details, and payment transactions
- Rate limiting and retry logic
- Cookie import from curl commands via `ImportCookiesFromCurl()`
