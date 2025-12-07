# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
