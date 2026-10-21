# Performance Benchmark — Baseline (2026-10-21)

## Environment

| Component | Value |
|-----------|-------|
| Server | FireflyIII-AI-Categorizer (Go 1.22.12) |
| Port | 3999 (localhost) |
| Firefly III | https://firefly.asaezs.com (v6.7.9) |
| AI Provider | Gemini (gemini-2.5-flash) |
| Total withdrawals in Firefly | 7,199 |
| Network | Local VPS → Firefly III (same datacenter) |

## API Endpoint Benchmarks

### /api/review — MAIN TARGET (CRITICAL)

| Metric | Run 1 | Run 2 | Run 3 | Average |
|--------|-------|-------|-------|---------|
| Response time | 54.99s | 54.90s | 55.87s | **55.25s** |
| TTFB | 54.99s | 54.90s | 55.87s | 55.25s |
| Payload size | 5,737 B | 5,737 B | 5,737 B | 5,737 B |
| HTTP status | 200 | 200 | 200 | 200 |

**Result:** 17 review groups, 19 transactions (9 NEEDS_REVIEW, 8 ASSUMED, 0 DEST_ASSUMED, 0 TRANSFER_CATEGORY)

**Firefly III API calls made:** 144 sequential GET requests (all withdrawal pages, limit=50)
**Data transferred from Firefly:** ~17 MB (144 pages × ~120 KB/page)
**Efficiency:** 19 useful transactions / 7,199 fetched = **0.26%**

### /api/transactions (paginated, default)

| Metric | Run 1 | Run 2 | Run 3 | Average |
|--------|-------|-------|-------|---------|
| Response time | 0.360s | 0.404s | 0.411s | **0.392s** |
| TTFB | 0.360s | 0.404s | 0.411s | 0.392s |
| Payload size | 16,046 B | 16,046 B | 16,046 B | 16,046 B |

**Result:** Page 1 of 144, 49 rows, total 7,199 transactions

### /api/transactions?ids_only=true (fetches ALL pages)

| Metric | Run 1 | Run 2 | Run 3 | Average |
|--------|-------|-------|-------|---------|
| Response time | 17.79s | 19.17s | 17.66s | **18.21s** |
| Payload size | 52,352 B | 52,352 B | 52,352 B | 52,352 B |

**Result:** 7,184 IDs returned (144 Firefly API calls)

### /api/transactions?missing_category=true (filtered, all pages)

| Metric | Run 1 | Run 2 | Run 3 | Average |
|--------|-------|-------|-------|---------|
| Response time | 19.51s | 18.32s | 18.45s | **18.76s** |
| Payload size | 17,383 B | 17,383 B | 17,383 B | 17,383 B |

**Result:** 2,479 IDs with missing category (144 Firefly API calls)

### /api/categories

| Metric | Run 1 | Run 2 | Run 3 | Average |
|--------|-------|-------|-------|---------|
| Response time | 0.249s | 0.202s | 0.157s | **0.203s** |
| Payload size | 2,092 B | 2,092 B | 2,092 B | 2,092 B |

**Result:** 62 categories

### /api/accounts?type=asset

| Metric | Run 1 | Run 2 | Run 3 | Average |
|--------|-------|-------|-------|---------|
| Response time | 0.141s | 0.193s | 0.128s | **0.154s** |
| Payload size | 380 B | 380 B | 380 B | 380 B |

**Result:** 10 asset accounts

### /api/accounts?type=expense (default)

| Metric | Run 1 | Run 2 | Run 3 | Average |
|--------|-------|-------|-------|---------|
| Response time | 5.84s | 5.48s | 5.63s | **5.65s** |
| Payload size | 86,504 B | 86,504 B | 86,504 B | 86,504 B |

**Result:** 1,686 expense accounts (paginates through all pages)

### /api/theme

| Metric | Run 1 | Run 2 | Run 3 | Average |
|--------|-------|-------|-------|---------|
| Response time | 0.068s | 0.057s | 0.074s | **0.066s** |
| Payload size | 30 B | 30 B | 30 B | 30 B |

### /api/config

| Metric | Run 1 | Run 2 | Run 3 | Average |
|--------|-------|-------|-------|---------|
| Response time | 0.001s | 0.001s | 0.001s | **0.001s** |
| Payload size | 503 B | 503 B | 503 B | 503 B |

### /health

| Metric | Run 1 | Run 2 | Run 3 | Average |
|--------|-------|-------|-------|---------|
| Response time | 0.006s | 0.001s | 0.003s | **0.003s** |
| Payload size | 16 B | 16 B | 16 B | 16 B |

## Frontend (Static Assets)

| Resource | Size | Response Time | Notes |
|----------|------|---------------|-------|
| / (index.html) | 76,912 B | 0.015s | SPA shell |
| /app.js | 85,069 B | 0.003s | 1,785 lines |
| /app.css | 12,775 B | 0.004s | |
| /darkmode.css | 14,894 B | 0.003s | |
| /lightmode.css | 4,996 B | 0.002s | |
| /favicon.svg | 2,089 B | 0.003s | |
| **Total frontend** | **196,735 B** | | ~192 KB uncompressed |

## Review Page Load Sequence (Simulated)

The Review tab in the SPA makes these API calls on load (from `loadReview()` in app.js:576):

| Step | Resource | Time | Size | Parallel? |
|------|----------|------|------|-----------|
| 1 | GET / (HTML) | 0.001s | 77 KB | — |
| 2 | GET /app.css | 0.002s | 13 KB | Parallel with 3 |
| 3 | GET /lightmode.css | 0.001s | 5 KB | Parallel with 2 |
| 4 | GET /app.js | 0.001s | 85 KB | After HTML |
| 5 | GET /api/theme | 0.045s | 30 B | On JS init |
| 6 | GET /api/config | 0.001s | 503 B | On JS init |
| 7 | GET /api/categories | 0.166s | 2 KB | Parallel with 8,9 |
| 8 | GET /api/accounts | 0.169s | 380 B | Parallel with 7,9 |
| 9 | **GET /api/review** | **56.3s** | **5.7 KB** | **Parallel with 7,8 — BOTTLENECK** |

**Estimated TTI (Time to Interactive):** ~56.5s (dominated by /api/review)
**Estimated LCP (Largest Contentful Paint):** ~0.1s (HTML loads fast, but content is hidden behind spinner until review loads)

Note: The frontend fires `/api/categories`, `/api/review`, and `/api/accounts` in parallel (app.js:588-590).
The review call blocks rendering because the UI shows a spinner until all three complete.

Also: `fetchReviewBadge()` (app.js:692) polls `/api/review` every 30 seconds when NOT on the review tab.

## Firefly III API Characteristics

| Metric | Value |
|--------|-------|
| Total withdrawals (all time) | 7,199 |
| Pages at limit=50 | 144 |
| Pages at limit=200 | 36 |
| Avg response time per page (limit=50) | ~0.38s |
| Avg response time per page (limit=200) | ~0.55s |
| Avg page size (limit=50) | ~120 KB |
| Avg page size (limit=200) | ~430 KB |
| Total data for full scan (limit=50) | ~17 MB |
| Total data for full scan (limit=200) | ~15.4 MB |
| Time for full scan (limit=50) | ~55s |
| Time for full scan (limit=200) | ~19.8s |

### Review-tagged transactions by date window

| Window | Total withdrawals | Pages (limit=200) | Review-tagged | Transfer-category |
|--------|-------------------|-------------------|---------------|-------------------|
| 90 days | 162 | 1 | 9 | 0 |
| 180 days | 299 | 2 | 11 | 0 |
| 365 days | 598 | 3 | 14 | 0 |
| Full history | 7,199 | 36 | 19 | 0 |

**Key finding:** 19 review-tagged txns exist in full history, but only 9 in the last 90 days.
A 365-day window captures 14/19 (74%). Adding a date filter would miss 5 older items.

## Root Cause Analysis

**File:** `internal/firefly/client.go:278` — `GetReviewGroups()`

```go
func (c *Client) GetReviewGroups(ctx context.Context) (*ReviewGroups, error) {
    ...
    params := url.Values{"type": {"withdrawal"}}
    for page := 1; ; page++ {  // <-- NO date filter, iterates ALL 144 pages
        ...
    }
}
```

The function fetches **all** withdrawal transaction pages (144 pages, 7,199 transactions, ~17 MB)
to find transactions with specific AI tags (`ai:needs-review`, `ai:assumed`, `ai:dest-assumed`)
or category "Transfers". Only 19 transactions (0.26%) match.

**Secondary issue:** `internal/api/handler.go:903` — `getTransactions()` with filters also
iterates all pages when `missing_category`, `missing_destination`, or category/destination
filters are active (~18-19s).

**Tertiary issue:** `internal/firefly/client.go:44` — `GetExpenseAccounts()` paginates
through 1,686 accounts (~5.6s). This is called on every Review page load.

## Optimization Targets

| Priority | Target | Current | Potential | Approach |
|----------|--------|---------|-----------|----------|
| P0 | /api/review | 55.3s | <2s | Add date filter (start=90d ago) + limit=200 |
| P0 | /api/review (full) | 55.3s | <20s | Use limit=200 instead of default 50 |
| P1 | /api/review (new txns) | 55.3s | <1s | Cache review groups, invalidate on webhook |
| P1 | /api/transactions (filtered) | 18.8s | <3s | Add date range filter (default 90 days) |
| P1 | /api/accounts (expense) | 5.6s | <3s | Paginate with limit=200, or lazy-load |
| P2 | Review page TTI | ~56.5s | <3s | All of the above combined |
| P2 | Review badge polling | 55s every 30s | Cached | Use lightweight count endpoint |

## Database Query Analysis

The Go server makes **no direct database queries** — all data comes from the Firefly III REST API.
Each Firefly API page request triggers database queries on the Firefly III server side.

**Firefly API calls per Review page load:**
1. `/api/v1/transactions?type=withdrawal&page=1..144&limit=50` → 144 calls (from GetReviewGroups)
2. `/api/v1/categories?page=1` → 1 call (from GetCategories, parallel)
3. `/api/v1/accounts?type=expense&page=1..N` → ~4 calls (from GetExpenseAccounts, parallel)

**Total: ~149 Firefly API calls per Review page load**

## Summary Table

| Endpoint | Avg Time | Payload | Firefly Calls | Status |
|----------|----------|---------|---------------|--------|
| /health | 0.003s | 16 B | 0 | OK |
| /api/config | 0.001s | 503 B | 0 | OK |
| /api/theme | 0.066s | 30 B | 1 | OK |
| /api/categories | 0.203s | 2 KB | 1 | OK |
| /api/accounts?type=asset | 0.154s | 380 B | 1 | OK |
| /api/accounts (expense) | 5.65s | 87 KB | ~4 | SLOW |
| /api/transactions (1 page) | 0.392s | 16 KB | 1 | OK |
| /api/transactions?ids_only | 18.2s | 52 KB | 144 | SLOW |
| /api/transactions (filtered) | 18.8s | 17 KB | 144 | SLOW |
| **/api/review** | **55.3s** | **5.7 KB** | **144** | **CRITICAL** |

## Benchmark Methodology

- 3 runs per endpoint, reporting all values
- Server running locally on port 3999 with real Firefly III backend
- curl with `-w` for timing metrics (TTFB, total time, download size)
- Firefly III API calls measured directly for page-level analysis
- Frontend analysis from source code inspection (app.js, index.html)
- Date: 2026-10-21 ~11:00 UTC
