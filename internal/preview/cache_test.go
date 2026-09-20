package preview

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

type fakeRemoteFS struct {
	mu sync.Mutex

	home  string
	kinds map[string]string
	lists map[string][]remoteEntry

	listCalls map[string]int
	kindCalls map[string]int
	listErr   map[string]error
	listBlock map[string]<-chan struct{}
}

func newFakeRemoteFS() *fakeRemoteFS {
	return &fakeRemoteFS{
		home:      "/home/test",
		kinds:     make(map[string]string),
		lists:     make(map[string][]remoteEntry),
		listCalls: make(map[string]int),
		kindCalls: make(map[string]int),
		listErr:   make(map[string]error),
		listBlock: make(map[string]<-chan struct{}),
	}
}

func (f *fakeRemoteFS) Home(context.Context) (string, error) {
	return f.home, nil
}

func (f *fakeRemoteFS) Kind(_ context.Context, remotePath string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.kindCalls[remotePath]++
	kind, ok := f.kinds[remotePath]
	if !ok {
		return "missing", nil
	}
	return kind, nil
}

func (f *fakeRemoteFS) List(ctx context.Context, remotePath string) ([]remoteEntry, error) {
	f.mu.Lock()
	f.listCalls[remotePath]++
	block := f.listBlock[remotePath]
	err := f.listErr[remotePath]
	entries := cloneRemoteEntries(f.lists[remotePath])
	f.mu.Unlock()

	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err != nil {
		return nil, err
	}
	return entries, nil
}

func (f *fakeRemoteFS) Read(context.Context, string) ([]byte, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeRemoteFS) calls(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.listCalls[path]
}

func (f *fakeRemoteFS) kindCallCount(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.kindCalls[path]
}

func TestCachedRemoteFSReusesListingAndEntryKind(t *testing.T) {
	backend := newFakeRemoteFS()
	backend.lists["/root"] = []remoteEntry{{Name: "docs", Kind: "dir"}, {Name: "readme.txt", Kind: "file"}}
	options := listingCacheOptions{TTL: time.Minute, MaxEntries: 10}
	cached := newCachedRemoteFSWithOptions(backend, "remote-host", options)

	if _, err := cached.List(context.Background(), "/root"); err != nil {
		t.Fatal(err)
	}
	if _, err := cached.List(context.Background(), "/root/"); err != nil {
		t.Fatal(err)
	}
	if got := backend.calls("/root"); got != 1 {
		t.Fatalf("listing calls=%d, want 1", got)
	}

	kind, err := cached.Kind(context.Background(), "/root/docs")
	if err != nil {
		t.Fatal(err)
	}
	if kind != "dir" {
		t.Fatalf("kind=%q, want dir", kind)
	}
	if got := backend.kindCallCount("/root/docs"); got != 0 {
		t.Fatalf("kind calls=%d, want 0", got)
	}
}

func TestListingCacheExpires(t *testing.T) {
	backend := newFakeRemoteFS()
	backend.lists["/root"] = []remoteEntry{{Name: "a", Kind: "file"}}
	now := time.Unix(100, 0)
	options := listingCacheOptions{
		TTL:        time.Second,
		MaxEntries: 10,
		Now:        func() time.Time { return now },
	}
	cached := newCachedRemoteFSWithOptions(backend, "remote-host", options)

	if _, err := cached.List(context.Background(), "/root"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	if _, err := cached.List(context.Background(), "/root"); err != nil {
		t.Fatal(err)
	}
	if got := backend.calls("/root"); got != 2 {
		t.Fatalf("listing calls=%d, want 2 after TTL expiry", got)
	}
}

func TestListingCacheSingleflight(t *testing.T) {
	backend := newFakeRemoteFS()
	backend.lists["/root"] = []remoteEntry{{Name: "a", Kind: "file"}}
	release := make(chan struct{})
	started := make(chan struct{})
	backend.listBlock["/root"] = release

	options := listingCacheOptions{TTL: time.Minute, MaxEntries: 10}
	cached := newCachedRemoteFSWithOptions(backend, "remote-host", options)
	firstDone := make(chan error, 1)
	go func() {
		_, err := cached.List(context.Background(), "/root")
		firstDone <- err
	}()

	deadline := time.After(time.Second)
	for backend.calls("/root") == 0 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for first listing")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	close(started)

	const waiters = 7
	results := make(chan error, waiters)
	for range waiters {
		go func() {
			_, err := cached.List(context.Background(), "/root")
			results <- err
		}()
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	for range waiters {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if got := backend.calls("/root"); got != 1 {
		t.Fatalf("listing calls=%d, want 1", got)
	}
}

func TestListingCacheDoesNotCacheErrors(t *testing.T) {
	backend := newFakeRemoteFS()
	backend.listErr["/root"] = errors.New("temporary failure")
	options := listingCacheOptions{TTL: time.Minute, MaxEntries: 10}
	cached := newCachedRemoteFSWithOptions(backend, "remote-host", options)

	if _, err := cached.List(context.Background(), "/root"); err == nil {
		t.Fatal("expected first listing to fail")
	}
	backend.mu.Lock()
	backend.listErr["/root"] = nil
	backend.mu.Unlock()
	if _, err := cached.List(context.Background(), "/root"); err != nil {
		t.Fatal(err)
	}
	if got := backend.calls("/root"); got != 2 {
		t.Fatalf("listing calls=%d, want 2", got)
	}
}

func TestCachedRemoteFSPrefetchesParentAndChildDirectories(t *testing.T) {
	backend := newFakeRemoteFS()
	backend.lists["/root"] = []remoteEntry{{Name: "child", Kind: "dir"}, {Name: "file.txt", Kind: "file"}}
	backend.lists["/"] = nil
	backend.lists["/root/child"] = nil

	release := make(chan struct{})
	backend.listBlock["/"] = release
	backend.listBlock["/root/child"] = release
	options := listingCacheOptions{
		TTL:                    time.Minute,
		MaxEntries:             10,
		PrefetchMaxDirectories: 4,
		PrefetchConcurrency:    2,
		PrefetchTimeout:        time.Second,
	}
	cached := newCachedRemoteFSWithOptions(backend, "remote-host", options)

	start := time.Now()
	if _, err := cached.List(context.Background(), "/root"); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("initial listing waited for prefetch: %s", elapsed)
	}

	deadline := time.Now().Add(time.Second)
	for (backend.calls("/") == 0 || backend.calls("/root/child") == 0) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if backend.calls("/") == 0 || backend.calls("/root/child") == 0 {
		t.Fatalf("prefetch calls: parent=%d child=%d", backend.calls("/"), backend.calls("/root/child"))
	}
	close(release)

	deadline = time.Now().Add(time.Second)
	for (!hasCachedListing(cached.cache, "/") || !hasCachedListing(cached.cache, "/root/child")) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !hasCachedListing(cached.cache, "/") || !hasCachedListing(cached.cache, "/root/child") {
		t.Fatal("prefetched listings were not cached")
	}
}

type priorityRemoteFS struct {
	mu sync.Mutex

	workspaceStarted chan struct{}
	workspaceFirst   bool
	calls            map[string]int
}

func newPriorityRemoteFS() *priorityRemoteFS {
	return &priorityRemoteFS{
		workspaceStarted: make(chan struct{}),
		workspaceFirst:   true,
		calls:            make(map[string]int),
	}
}

func (f *priorityRemoteFS) Home(context.Context) (string, error) {
	return "/home/test", nil
}

func (f *priorityRemoteFS) Kind(context.Context, string) (string, error) {
	return "dir", nil
}

func (f *priorityRemoteFS) Read(context.Context, string) ([]byte, error) {
	return nil, errors.New("not implemented")
}

func (f *priorityRemoteFS) List(ctx context.Context, remotePath string) ([]remoteEntry, error) {
	f.mu.Lock()
	f.calls[remotePath]++
	blockWorkspace := remotePath == "/root/workspace" && f.workspaceFirst
	if blockWorkspace {
		f.workspaceFirst = false
		close(f.workspaceStarted)
	}
	f.mu.Unlock()

	if blockWorkspace {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if remotePath == "/root" {
		return []remoteEntry{{Name: "workspace", Kind: "dir"}}, nil
	}
	return nil, nil
}

func (f *priorityRemoteFS) callCount(remotePath string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[remotePath]
}

func TestCachedRemoteFSPrioritizesUserListingOverPrefetch(t *testing.T) {
	backend := newPriorityRemoteFS()
	cached := newCachedRemoteFSWithOptions(backend, "remote-host", listingCacheOptions{
		TTL:                    time.Minute,
		MaxEntries:             10,
		PrefetchMaxDirectories: 1,
		PrefetchConcurrency:    1,
		PrefetchTimeout:        time.Minute,
	})
	defer cached.prefetches.cancelAll()

	if _, err := cached.List(context.Background(), "/root"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-backend.workspaceStarted:
	case <-time.After(time.Second):
		t.Fatal("workspace prefetch did not start")
	}

	start := time.Now()
	if _, err := cached.List(context.Background(), "/root/workspace"); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("user listing waited for prefetch: %s", elapsed)
	}
	if got := backend.callCount("/root/workspace"); got != 2 {
		t.Fatalf("workspace listing calls=%d, want canceled prefetch plus user listing", got)
	}
}

func hasCachedListing(cache *listingCache, remotePath string) bool {
	_, ok := cache.peek(remotePath)
	return ok
}

func TestCachedRemoteFSUsesHostInCacheKey(t *testing.T) {
	backend := newFakeRemoteFS()
	backend.lists["/root"] = []remoteEntry{{Name: "a", Kind: "file"}}
	cacheA := newCachedRemoteFSWithOptions(backend, "remote-host", listingCacheOptions{TTL: time.Minute, MaxEntries: 10})
	cacheB := newCachedRemoteFSWithOptions(backend, "other-host", listingCacheOptions{TTL: time.Minute, MaxEntries: 10})
	if _, err := cacheA.List(context.Background(), "/root"); err != nil {
		t.Fatal(err)
	}
	if _, err := cacheB.List(context.Background(), "/root"); err != nil {
		t.Fatal(err)
	}
	if got := backend.calls("/root"); got != 2 {
		t.Fatalf("listing calls=%d, want 2 for distinct hosts", got)
	}
}

func TestListingCacheEvictsOldEntries(t *testing.T) {
	backend := newFakeRemoteFS()
	backend.lists["/one"] = []remoteEntry{}
	backend.lists["/two"] = []remoteEntry{}
	options := listingCacheOptions{TTL: time.Minute, MaxEntries: 1, PrefetchMaxDirectories: 0}
	cached := newCachedRemoteFSWithOptions(backend, "remote-host", options)
	for _, remotePath := range []string{"/one", "/two", "/one"} {
		if _, err := cached.List(context.Background(), remotePath); err != nil {
			t.Fatal(err)
		}
	}
	if got := backend.calls("/one"); got != 2 {
		t.Fatalf("/one listing calls=%d, want 2 after eviction", got)
	}
}

func Example_cachedRemoteFS() {
	backend := newFakeRemoteFS()
	backend.lists["/root"] = []remoteEntry{{Name: "notes", Kind: "dir"}}
	cached := newCachedRemoteFSWithOptions(backend, "remote-host", listingCacheOptions{
		TTL:                    time.Minute,
		MaxEntries:             10,
		PrefetchMaxDirectories: 0,
	})
	entries, _ := cached.List(context.Background(), "/root")
	fmt.Println(entries[0].Name)
	// Output: notes
}
