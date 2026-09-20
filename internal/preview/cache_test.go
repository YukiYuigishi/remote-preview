package preview

import (
	"context"
	"errors"
	"fmt"
	"path"
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

func TestCachedRemoteFSListsOnlyRequestedDirectory(t *testing.T) {
	backend := newFakeRemoteFS()
	backend.lists["/root"] = []remoteEntry{{Name: "workspace", Kind: "dir"}}
	cached := newCachedRemoteFSWithOptions(backend, "remote-host", listingCacheOptions{
		TTL:        time.Minute,
		MaxEntries: 10,
	})

	if _, err := cached.List(context.Background(), "/root"); err != nil {
		t.Fatal(err)
	}
	if got := backend.calls("/root"); got != 1 {
		t.Fatalf("root listing calls=%d, want 1", got)
	}
	if got := backend.calls("/root/workspace"); got != 0 {
		t.Fatalf("workspace listing calls=%d, want 0 before navigation", got)
	}
}

type fakeBatchRemoteFS struct {
	*fakeRemoteFS
	batchCalls int
	batchErr   error
}

func (f *fakeBatchRemoteFS) ListBatch(_ context.Context, remotePath string) (batchListingResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.batchCalls++
	if f.batchErr != nil {
		return batchListingResult{}, f.batchErr
	}
	return batchListingResult{
		RootKind: "dir",
		Listings: []remoteListing{
			{Path: remotePath, Entries: cloneRemoteEntries(f.lists[remotePath])},
			{Path: path.Join(remotePath, "workspace"), Entries: []remoteEntry{{Name: "notes.md", Kind: "file"}}},
		},
	}, nil
}

func TestCachedRemoteFSDistributesBatchListings(t *testing.T) {
	backend := &fakeBatchRemoteFS{fakeRemoteFS: newFakeRemoteFS()}
	backend.lists["/root"] = []remoteEntry{{Name: "workspace", Kind: "dir"}}
	cached := newCachedRemoteFSWithOptions(backend, "remote-host", listingCacheOptions{TTL: time.Minute, MaxEntries: 10})

	if _, err := cached.List(context.Background(), "/root"); err != nil {
		t.Fatal(err)
	}
	if backend.batchCalls != 1 || backend.calls("/root") != 0 {
		t.Fatalf("batch calls=%d, regular root calls=%d; want batch=1 regular=0", backend.batchCalls, backend.calls("/root"))
	}

	entries, err := cached.List(context.Background(), "/root/workspace")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "notes.md" {
		t.Fatalf("unexpected cached child listing: %#v", entries)
	}
	if backend.batchCalls != 1 || backend.calls("/root/workspace") != 0 {
		t.Fatalf("child listing caused another remote call: batch=%d regular=%d", backend.batchCalls, backend.calls("/root/workspace"))
	}
}

func TestCachedRemoteFSBatchKindAvoidsSecondSSH(t *testing.T) {
	backend := &fakeBatchRemoteFS{fakeRemoteFS: newFakeRemoteFS()}
	backend.lists["/root"] = []remoteEntry{{Name: "workspace", Kind: "dir"}}
	cached := newCachedRemoteFSWithOptions(backend, "remote-host", listingCacheOptions{TTL: time.Minute, MaxEntries: 10})

	kind, err := cached.Kind(context.Background(), "/root")
	if err != nil {
		t.Fatal(err)
	}
	if kind != "dir" || backend.batchCalls != 1 || backend.kindCallCount("/root") != 0 {
		t.Fatalf("kind lookup: kind=%q batch=%d regular=%d; want dir/1/0", kind, backend.batchCalls, backend.kindCallCount("/root"))
	}
	if _, err := cached.List(context.Background(), "/root"); err != nil {
		t.Fatal(err)
	}
	if backend.batchCalls != 1 || backend.calls("/root") != 0 {
		t.Fatalf("root list caused another SSH: batch=%d regular=%d", backend.batchCalls, backend.calls("/root"))
	}
}

func TestCachedRemoteFSFallsBackWhenBatchListingFails(t *testing.T) {
	backend := &fakeBatchRemoteFS{
		fakeRemoteFS: newFakeRemoteFS(),
		batchErr:     errors.New("batch unavailable"),
	}
	backend.lists["/root"] = []remoteEntry{{Name: "file.txt", Kind: "file"}}
	cached := newCachedRemoteFSWithOptions(backend, "remote-host", listingCacheOptions{TTL: time.Minute, MaxEntries: 10})

	if _, err := cached.List(context.Background(), "/root"); err != nil {
		t.Fatal(err)
	}
	if backend.batchCalls != 1 || backend.calls("/root") != 1 {
		t.Fatalf("fallback calls: batch=%d regular=%d; want 1/1", backend.batchCalls, backend.calls("/root"))
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
	options := listingCacheOptions{TTL: time.Minute, MaxEntries: 1}
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
		TTL:        time.Minute,
		MaxEntries: 10,
	})
	entries, _ := cached.List(context.Background(), "/root")
	fmt.Println(entries[0].Name)
	// Output: notes
}
