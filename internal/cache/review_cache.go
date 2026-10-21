package cache

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/openaccountants/firefly-iii-ai-categorize/internal/firefly"
)

// ReviewCache holds a TTL-backed, in-memory copy of review groups fetched
// from Firefly III. The /api/review endpoint is polled every 30 seconds by
// the frontend badge, so caching avoids repeated 144-page scans.
type ReviewCache struct {
	mu        sync.RWMutex
	groups    *firefly.ReviewGroups
	fetchedAt time.Time
	ttl       time.Duration
	lookback  int
	client    *firefly.Client
}

func NewReviewCache(client *firefly.Client, ttl time.Duration, lookbackDays int) *ReviewCache {
	return &ReviewCache{
		ttl:      ttl,
		lookback: lookbackDays,
		client:   client,
	}
}

// GetReviewGroups returns cached review groups, refreshing from Firefly III
// when the TTL has expired. Returns an error only on fetch failure.
func (c *ReviewCache) GetReviewGroups(ctx context.Context) (*firefly.ReviewGroups, error) {
	// Fast path: check under read lock.
	c.mu.RLock()
	fresh := c.ttl > 0 && c.groups != nil && time.Since(c.fetchedAt) < c.ttl
	c.mu.RUnlock()
	if fresh {
		c.mu.RLock()
		defer c.mu.RUnlock()
		return c.groups, nil
	}

	// Slow path: acquire write lock and double-check.
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ttl > 0 && c.groups != nil && time.Since(c.fetchedAt) < c.ttl {
		return c.groups, nil
	}

	slog.Info("refreshing review groups cache from Firefly III", "lookback_days", c.lookback)
	rg, err := c.client.GetReviewGroups(ctx, c.lookback)
	if err != nil {
		return nil, err
	}

	c.groups = rg
	c.fetchedAt = time.Now()
	slog.Info("review groups cache refreshed",
		"needs_review", len(rg.NeedsReview),
		"assumed", len(rg.Assumed),
		"dest_assumed", len(rg.DestAssumed),
		"transfer_category", len(rg.TransferCategory),
	)
	return rg, nil
}

// Invalidate forces a re-fetch on the next GetReviewGroups call.
// Call this after a webhook or job that mutates review-tagged transactions.
func (c *ReviewCache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fetchedAt = time.Time{}
	c.groups = nil
}
