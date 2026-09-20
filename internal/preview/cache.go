package preview

import (
	"context"
	"path"
	"sync"
	"time"
)

const (
	defaultListingCacheTTL        = 10 * time.Second
	defaultListingCacheMaxEntries = 256
	defaultPrefetchMaxDirectories = 4
	defaultPrefetchConcurrency    = 1
	defaultPrefetchTimeout        = 15 * time.Second
)

type listingCacheOptions struct {
	TTL                    time.Duration
	MaxEntries             int
	PrefetchMaxDirectories int
	PrefetchConcurrency    int
	PrefetchTimeout        time.Duration
	Now                    func() time.Time
}

func defaultListingCacheOptions() listingCacheOptions {
	return listingCacheOptions{
		TTL:                    defaultListingCacheTTL,
		MaxEntries:             defaultListingCacheMaxEntries,
		PrefetchMaxDirectories: defaultPrefetchMaxDirectories,
		PrefetchConcurrency:    defaultPrefetchConcurrency,
		PrefetchTimeout:        defaultPrefetchTimeout,
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
	done       chan struct{}
	entries    []remoteEntry
	err        error
	background bool
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
	return c.getMode(ctx, remotePath, false, fetch)
}

func (c *listingCache) getPrefetch(ctx context.Context, remotePath string, fetch func(context.Context) ([]remoteEntry, error)) ([]remoteEntry, error) {
	return c.getMode(ctx, remotePath, true, fetch)
}

func (c *listingCache) getMode(ctx context.Context, remotePath string, background bool, fetch func(context.Context) ([]remoteEntry, error)) ([]remoteEntry, error) {
	key := c.key(remotePath)

	for {
		c.mu.Lock()
		if entry, ok := c.entries[key]; ok {
			if c.ttl > 0 && c.now().Before(entry.expiresAt) {
				entries := cloneRemoteEntries(entry.entries)
				c.mu.Unlock()
				return entries, nil
			}
			delete(c.entries, key)
		}
		if load, ok := c.loading[key]; ok {
			c.mu.Unlock()
			select {
			case <-load.done:
				if !background && load.background && load.err != nil && ctx.Err() == nil {
					continue
				}
				return cloneRemoteEntries(load.entries), load.err
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		load := &listingLoad{done: make(chan struct{}), background: background}
		c.loading[key] = load
		c.mu.Unlock()

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

		return entries, err
	}
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
	backend    RemoteFS
	cache      *listingCache
	sem        chan struct{}
	options    listingCacheOptions
	prefetches prefetchRegistry
}

type prefetchRegistry struct {
	mu     sync.Mutex
	nextID uint64
	active map[uint64]context.CancelFunc
}

func newCachedRemoteFS(backend RemoteFS, host string) *cachedRemoteFS {
	return newCachedRemoteFSWithOptions(backend, host, defaultListingCacheOptions())
}

func newCachedRemoteFSWithOptions(backend RemoteFS, host string, options listingCacheOptions) *cachedRemoteFS {
	concurrency := options.PrefetchConcurrency
	if concurrency <= 0 {
		concurrency = 1
	}
	if options.PrefetchMaxDirectories < 0 {
		options.PrefetchMaxDirectories = 0
	}
	if options.PrefetchTimeout <= 0 {
		options.PrefetchTimeout = defaultPrefetchTimeout
	}
	return &cachedRemoteFS{
		backend: backend,
		cache:   newListingCache(host, options),
		sem:     make(chan struct{}, concurrency),
		options: options,
		prefetches: prefetchRegistry{
			active: make(map[uint64]context.CancelFunc),
		},
	}
}

func (c *cachedRemoteFS) Home(ctx context.Context) (string, error) {
	return c.backend.Home(ctx)
}

func (c *cachedRemoteFS) Kind(ctx context.Context, remotePath string) (string, error) {
	// Kind is the first operation for every foreground HTTP request, including
	// file previews. Stop warming work before doing any user-visible remote I/O.
	c.prefetches.cancelAll()
	remotePath = path.Clean(remotePath)
	parent := path.Dir(remotePath)
	base := path.Base(remotePath)
	if entries, ok := c.cache.peek(parent); ok {
		for _, entry := range entries {
			if entry.Name == base && (entry.Kind == "dir" || entry.Kind == "file") {
				return entry.Kind, nil
			}
		}
	}
	return c.backend.Kind(ctx, remotePath)
}

func (c *cachedRemoteFS) List(ctx context.Context, remotePath string) ([]remoteEntry, error) {
	// A foreground navigation has priority over background warming. Cancelling
	// all prefetches also releases SSH/process and remote-shell contention.
	c.prefetches.cancelAll()
	remotePath = path.Clean(remotePath)
	entries, err := c.cache.get(ctx, remotePath, func(fetchCtx context.Context) ([]remoteEntry, error) {
		return c.backend.List(fetchCtx, remotePath)
	})
	if err == nil {
		c.prefetch(remotePath, entries)
	}
	return entries, err
}

func (c *cachedRemoteFS) Read(ctx context.Context, remotePath string) ([]byte, error) {
	c.prefetches.cancelAll()
	return c.backend.Read(ctx, remotePath)
}

func (c *cachedRemoteFS) prefetch(current string, entries []remoteEntry) {
	if c.options.PrefetchMaxDirectories == 0 {
		return
	}

	candidates := make([]string, 0, c.options.PrefetchMaxDirectories+1)
	seen := make(map[string]struct{})
	add := func(remotePath string) bool {
		remotePath = path.Clean(remotePath)
		if _, ok := seen[remotePath]; ok || len(candidates) >= c.options.PrefetchMaxDirectories+1 {
			return false
		}
		seen[remotePath] = struct{}{}
		candidates = append(candidates, remotePath)
		return true
	}
	add(path.Dir(current))
	for _, entry := range entries {
		if entry.Kind != "dir" {
			continue
		}
		if !add(path.Join(current, entry.Name)) || len(candidates) >= c.options.PrefetchMaxDirectories+1 {
			break
		}
	}
	if len(candidates) == 0 {
		return
	}

	prefetchCtx, cancel := context.WithTimeout(context.Background(), c.options.PrefetchTimeout)
	prefetchID := c.prefetches.add(cancel)
	var wg sync.WaitGroup
	wg.Add(len(candidates))
	for _, candidate := range candidates {
		candidate := candidate
		go func() {
			defer wg.Done()
			select {
			case c.sem <- struct{}{}:
			case <-prefetchCtx.Done():
				return
			}
			defer func() { <-c.sem }()
			_, _ = c.cache.getPrefetch(prefetchCtx, candidate, func(fetchCtx context.Context) ([]remoteEntry, error) {
				return c.backend.List(fetchCtx, candidate)
			})
		}()
	}
	go func() {
		wg.Wait()
		cancel()
		c.prefetches.remove(prefetchID)
	}()
}

func (r *prefetchRegistry) add(cancel context.CancelFunc) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	r.active[r.nextID] = cancel
	return r.nextID
}

func (r *prefetchRegistry) remove(id uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.active, id)
}

func (r *prefetchRegistry) cancelAll() {
	r.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(r.active))
	for _, cancel := range r.active {
		cancels = append(cancels, cancel)
	}
	r.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func cloneRemoteEntries(entries []remoteEntry) []remoteEntry {
	if entries == nil {
		return nil
	}
	return append([]remoteEntry(nil), entries...)
}
