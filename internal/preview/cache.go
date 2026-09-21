package preview

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path"
	"sync"
	"time"
)

const (
	defaultListingCacheTTL        = 10 * time.Second
	defaultListingCacheMaxEntries = 256
)

type listingCacheOptions struct {
	TTL        time.Duration
	MaxEntries int
	Now        func() time.Time
}

func defaultListingCacheOptions() listingCacheOptions {
	return listingCacheOptions{
		TTL:        defaultListingCacheTTL,
		MaxEntries: defaultListingCacheMaxEntries,
	}
}

type listingCache struct {
	host string
	ttl  time.Duration
	max  int
	now  func() time.Time

	mu       sync.Mutex
	entries  map[string]listingCacheEntry
	loading  map[string]*listingLoad
	sequence uint64
}

type listingCacheEntry struct {
	entries   []remoteEntry
	expiresAt time.Time
	sequence  uint64
}

type listingLoad struct {
	done    chan struct{}
	entries []remoteEntry
	err     error
}

func newListingCache(host string, options listingCacheOptions) *listingCache {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	maxEntries := options.MaxEntries
	if maxEntries < 0 {
		maxEntries = 0
	}
	return &listingCache{
		host:    host,
		ttl:     options.TTL,
		max:     maxEntries,
		now:     now,
		entries: make(map[string]listingCacheEntry),
		loading: make(map[string]*listingLoad),
	}
}

func (c *listingCache) key(remotePath string) string {
	return c.host + "\x00" + path.Clean(remotePath)
}

func (c *listingCache) get(ctx context.Context, remotePath string, fetch func(context.Context) ([]remoteEntry, error)) ([]remoteEntry, error) {
	key := c.key(remotePath)

	c.mu.Lock()
	if entry, ok := c.entries[key]; ok {
		if c.ttl > 0 && c.now().Before(entry.expiresAt) {
			entries := cloneRemoteEntries(entry.entries)
			c.mu.Unlock()
			slog.Debug("listing cache hit", "host", c.host, "path", path.Clean(remotePath), "entries", len(entries))
			return entries, nil
		}
		delete(c.entries, key)
	}
	if load, ok := c.loading[key]; ok {
		c.mu.Unlock()
		slog.Debug("listing cache wait", "host", c.host, "path", path.Clean(remotePath))
		select {
		case <-load.done:
			slog.Debug("listing cache wait done", "host", c.host, "path", path.Clean(remotePath), "entries", len(load.entries), "error", load.err)
			return cloneRemoteEntries(load.entries), load.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	load := &listingLoad{done: make(chan struct{})}
	c.loading[key] = load
	c.mu.Unlock()

	started := time.Now()
	slog.Debug("listing cache miss", "host", c.host, "path", path.Clean(remotePath))
	entries, err := fetch(ctx)
	entries = cloneRemoteEntries(entries)

	c.mu.Lock()
	delete(c.loading, key)
	load.entries = entries
	load.err = err
	if err == nil && c.ttl > 0 && c.max > 0 {
		c.sequence++
		c.entries[key] = listingCacheEntry{
			entries:   cloneRemoteEntries(entries),
			expiresAt: c.now().Add(c.ttl),
			sequence:  c.sequence,
		}
		c.evictIfNeeded()
	}
	close(load.done)
	c.mu.Unlock()
	slog.Debug("listing cache fetch done", "host", c.host, "path", path.Clean(remotePath), "entries", len(entries), "duration", time.Since(started), "error", err)

	return entries, err
}

func (c *listingCache) store(remotePath string, entries []remoteEntry) {
	if c.ttl <= 0 || c.max <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sequence++
	c.entries[c.key(remotePath)] = listingCacheEntry{
		entries:   cloneRemoteEntries(entries),
		expiresAt: c.now().Add(c.ttl),
		sequence:  c.sequence,
	}
	c.evictIfNeeded()
	slog.Debug("listing cache store", "host", c.host, "path", path.Clean(remotePath), "entries", len(entries))
}

func (c *listingCache) peek(remotePath string) ([]remoteEntry, bool) {
	key := c.key(remotePath)
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	if c.ttl <= 0 || !c.now().Before(entry.expiresAt) {
		delete(c.entries, key)
		return nil, false
	}
	return cloneRemoteEntries(entry.entries), true
}

func (c *listingCache) invalidate(remotePath string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, c.key(remotePath))
}

func (c *listingCache) evictIfNeeded() {
	for len(c.entries) > c.max {
		var oldestKey string
		var oldestSequence uint64
		for key, entry := range c.entries {
			if oldestKey == "" || entry.sequence < oldestSequence {
				oldestKey = key
				oldestSequence = entry.sequence
			}
		}
		delete(c.entries, oldestKey)
	}
}

type cachedRemoteFS struct {
	backend RemoteFS
	cache   *listingCache
}

func newCachedRemoteFS(backend RemoteFS, host string) *cachedRemoteFS {
	return newCachedRemoteFSWithOptions(backend, host, defaultListingCacheOptions())
}

func newCachedRemoteFSWithOptions(backend RemoteFS, host string, options listingCacheOptions) *cachedRemoteFS {
	return &cachedRemoteFS{
		backend: backend,
		cache:   newListingCache(host, options),
	}
}

func (c *cachedRemoteFS) Home(ctx context.Context) (string, error) {
	return c.backend.Home(ctx)
}

func (c *cachedRemoteFS) Kind(ctx context.Context, remotePath string) (string, error) {
	remotePath = path.Clean(remotePath)
	parent := path.Dir(remotePath)
	base := path.Base(remotePath)
	if entries, ok := c.cache.peek(parent); ok {
		for _, entry := range entries {
			if entry.Name == base && (entry.Kind == "dir" || entry.Kind == "file") {
				slog.Debug("listing cache kind hit", "host", c.cache.host, "path", remotePath, "kind", entry.Kind)
				return entry.Kind, nil
			}
		}
	}
	if batch, ok := c.backend.(batchRemoteFS); ok {
		result, err := batch.ListBatch(ctx, remotePath)
		if err == nil {
			c.storeBatch(remotePath, result)
			slog.Debug("batch kind resolved", "path", remotePath, "kind", result.RootKind, "directories", len(result.Listings))
			return result.RootKind, nil
		}
		slog.Debug("batch kind fallback", "path", remotePath, "error", err)
	}
	return c.backend.Kind(ctx, remotePath)
}

func (c *cachedRemoteFS) List(ctx context.Context, remotePath string) ([]remoteEntry, error) {
	remotePath = path.Clean(remotePath)
	return c.cache.get(ctx, remotePath, func(fetchCtx context.Context) ([]remoteEntry, error) {
		if batch, ok := c.backend.(batchRemoteFS); ok {
			result, err := batch.ListBatch(fetchCtx, remotePath)
			if err == nil {
				current, foundCurrent := c.storeBatch(remotePath, result)
				if foundCurrent {
					slog.Debug("batch listing applied", "path", remotePath, "directories", len(result.Listings), "root_kind", result.RootKind)
					return current, nil
				}
				slog.Debug("batch listing missing current path", "path", remotePath, "directories", len(result.Listings))
			} else {
				slog.Debug("batch listing fallback", "path", remotePath, "error", err)
			}
		}
		return c.backend.List(fetchCtx, remotePath)
	})
}

func (c *cachedRemoteFS) storeBatch(remotePath string, result batchListingResult) ([]remoteEntry, bool) {
	var current []remoteEntry
	foundCurrent := false
	for _, listing := range result.Listings {
		listingPath := path.Clean(listing.Path)
		c.cache.store(listingPath, listing.Entries)
		if listingPath == path.Clean(remotePath) {
			current = cloneRemoteEntries(listing.Entries)
			foundCurrent = true
		}
	}
	slog.Debug("batch listings cached", "path", remotePath, "directories", len(result.Listings), "root_kind", result.RootKind)
	return current, foundCurrent
}

func (c *cachedRemoteFS) Read(ctx context.Context, remotePath string) ([]byte, error) {
	return c.backend.Read(ctx, remotePath)
}

func (c *cachedRemoteFS) Open(ctx context.Context, remotePath string) (io.ReadCloser, transferInfo, error) {
	backend, ok := c.backend.(transferFS)
	if !ok {
		return nil, transferInfo{}, fmt.Errorf("transfer backend is unavailable")
	}
	return backend.Open(ctx, remotePath)
}

func (c *cachedRemoteFS) MkdirAll(ctx context.Context, remotePath string) error {
	backend, ok := c.backend.(transferFS)
	if !ok {
		return fmt.Errorf("transfer backend is unavailable")
	}
	return backend.MkdirAll(ctx, remotePath)
}

func (c *cachedRemoteFS) WriteFile(ctx context.Context, remotePath string, src io.Reader) error {
	backend, ok := c.backend.(transferFS)
	if !ok {
		return fmt.Errorf("transfer backend is unavailable")
	}
	return backend.WriteFile(ctx, remotePath, src)
}

func (c *cachedRemoteFS) Invalidate(remotePath string) {
	c.cache.invalidate(remotePath)
}

func cloneRemoteEntries(entries []remoteEntry) []remoteEntry {
	if entries == nil {
		return nil
	}
	return append([]remoteEntry(nil), entries...)
}
