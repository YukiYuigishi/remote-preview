package preview

import (
	"context"
	"fmt"
	"path"
	"sync"
	"testing"
	"time"
)

// The benchmark backend is deliberately small, but models the two properties
// that dominate directory navigation over system ssh:
//   - every List/ListBatch call is one remote invocation; and
//   - each invocation has a configurable round-trip delay.
//
// A zero delay measures local traversal, parsing, and cache work. Non-zero
// delays make the same workload representative of SSH command latency without
// requiring a network host. The fixture is immutable while a benchmark runs,
// so concurrent prefetches observe the same remote tree as the other methods.
type directoryBenchmarkBackend struct {
	fixture *directoryBenchmarkFixture
	latency time.Duration

	mu          sync.Mutex
	invocations int
	singleCalls int
	batchCalls  int
}

type directoryBenchmarkSingleBackend struct {
	backend *directoryBenchmarkBackend
}

type directoryBenchmarkFixture struct {
	root     string
	children []string
	lists    map[string][]remoteEntry
}

type directoryBenchmarkStats struct {
	invocations int
	singleCalls int
	batchCalls  int
}

func newDirectoryBenchmarkFixture(childDirs, filesPerDirectory int) *directoryBenchmarkFixture {
	const root = "/benchmark-root"
	fixture := &directoryBenchmarkFixture{
		root:     root,
		children: make([]string, 0, childDirs),
		lists:    make(map[string][]remoteEntry, childDirs+1),
	}

	rootEntries := make([]remoteEntry, 0, childDirs+filesPerDirectory)
	for i := 0; i < childDirs; i++ {
		name := fmt.Sprintf("child-%02d", i)
		childPath := path.Join(root, name)
		fixture.children = append(fixture.children, childPath)
		rootEntries = append(rootEntries, remoteEntry{Name: name, Kind: "dir"})

		childEntries := make([]remoteEntry, 0, filesPerDirectory)
		for file := 0; file < filesPerDirectory; file++ {
			childEntries = append(childEntries, remoteEntry{
				Name: fmt.Sprintf("file-%02d.txt", file),
				Kind: "file",
			})
		}
		fixture.lists[childPath] = childEntries
	}
	for file := 0; file < filesPerDirectory; file++ {
		rootEntries = append(rootEntries, remoteEntry{
			Name: fmt.Sprintf("root-file-%02d.txt", file),
			Kind: "file",
		})
	}
	fixture.lists[root] = rootEntries
	return fixture
}

func newDirectoryBenchmarkBackend(fixture *directoryBenchmarkFixture, latency time.Duration) *directoryBenchmarkBackend {
	return &directoryBenchmarkBackend{fixture: fixture, latency: latency}
}

func (b *directoryBenchmarkBackend) Home(context.Context) (string, error) {
	return "/", nil
}

func (b *directoryBenchmarkBackend) Kind(ctx context.Context, remotePath string) (string, error) {
	if err := b.invoke(ctx, false); err != nil {
		return "", err
	}
	if remotePath == b.fixture.root {
		return "dir", nil
	}
	if _, ok := b.fixture.lists[path.Clean(remotePath)]; ok {
		return "dir", nil
	}
	return "missing", nil
}

func (b *directoryBenchmarkBackend) List(ctx context.Context, remotePath string) ([]remoteEntry, error) {
	if err := b.invoke(ctx, false); err != nil {
		return nil, err
	}
	return cloneRemoteEntries(b.fixture.lists[path.Clean(remotePath)]), nil
}

func (b *directoryBenchmarkBackend) ListBatch(ctx context.Context, remotePath string) (batchListingResult, error) {
	if err := b.invoke(ctx, true); err != nil {
		return batchListingResult{}, err
	}
	cleanPath := path.Clean(remotePath)
	if cleanPath != b.fixture.root {
		entries, ok := b.fixture.lists[cleanPath]
		if !ok {
			return batchListingResult{RootKind: "missing"}, nil
		}
		return batchListingResult{
			RootKind: "dir",
			Listings: []remoteListing{{Path: cleanPath, Entries: cloneRemoteEntries(entries)}},
		}, nil
	}

	listings := make([]remoteListing, 0, len(b.fixture.children)+1)
	listings = append(listings, remoteListing{
		Path:    cleanPath,
		Entries: cloneRemoteEntries(b.fixture.lists[cleanPath]),
	})
	for _, childPath := range b.fixture.children {
		listings = append(listings, remoteListing{
			Path:    childPath,
			Entries: cloneRemoteEntries(b.fixture.lists[childPath]),
		})
	}
	return batchListingResult{RootKind: "dir", Listings: listings}, nil
}

func (b *directoryBenchmarkBackend) Read(context.Context, string) ([]byte, error) {
	return nil, nil
}

func (b *directoryBenchmarkBackend) invoke(ctx context.Context, batch bool) error {
	b.mu.Lock()
	b.invocations++
	if batch {
		b.batchCalls++
	} else {
		b.singleCalls++
	}
	latency := b.latency
	b.mu.Unlock()

	if latency <= 0 {
		return nil
	}
	timer := time.NewTimer(latency)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *directoryBenchmarkBackend) stats() directoryBenchmarkStats {
	b.mu.Lock()
	defer b.mu.Unlock()
	return directoryBenchmarkStats{
		invocations: b.invocations,
		singleCalls: b.singleCalls,
		batchCalls:  b.batchCalls,
	}
}

// directoryBenchmarkSingleBackend intentionally does not expose ListBatch.
// This lets the regular cachedRemoteFS exercise the single-directory path
// without changing production interfaces or adding a benchmark-only switch.
func (b *directoryBenchmarkSingleBackend) Home(ctx context.Context) (string, error) {
	return b.backend.Home(ctx)
}

func (b *directoryBenchmarkSingleBackend) Kind(ctx context.Context, remotePath string) (string, error) {
	return b.backend.Kind(ctx, remotePath)
}

func (b *directoryBenchmarkSingleBackend) List(ctx context.Context, remotePath string) ([]remoteEntry, error) {
	return b.backend.List(ctx, remotePath)
}

func (b *directoryBenchmarkSingleBackend) Read(ctx context.Context, remotePath string) ([]byte, error) {
	return b.backend.Read(ctx, remotePath)
}

type directoryBenchmarkStrategy string

const (
	directoryStrategyOnDemand directoryBenchmarkStrategy = "single_on_demand"
	directoryStrategyBatch    directoryBenchmarkStrategy = "foreground_batch"
	directoryStrategyPrefetch directoryBenchmarkStrategy = "legacy_parallel_prefetch"
)

type directoryBenchmarkWorkload string

const (
	directoryWorkloadFirstView  directoryBenchmarkWorkload = "first_view"
	directoryWorkloadOneChild   directoryBenchmarkWorkload = "one_child_navigation"
	directoryWorkloadSequential directoryBenchmarkWorkload = "sequential_navigation"
)

func TestDirectoryStrategyBenchmarkCommandCounts(t *testing.T) {
	fixture := newDirectoryBenchmarkFixture(4, 4)
	ctx := context.Background()

	for _, strategy := range []directoryBenchmarkStrategy{
		directoryStrategyOnDemand,
		directoryStrategyBatch,
		directoryStrategyPrefetch,
	} {
		for _, workload := range []directoryBenchmarkWorkload{
			directoryWorkloadFirstView,
			directoryWorkloadOneChild,
			directoryWorkloadSequential,
		} {
			backend := newDirectoryBenchmarkBackend(fixture, 0)
			if err := runDirectoryBenchmarkWorkload(ctx, strategy, workload, fixture, backend); err != nil {
				t.Fatalf("strategy=%s workload=%s: %v", strategy, workload, err)
			}
			got := backend.stats()
			want := expectedDirectoryBenchmarkStats(strategy, workload, len(fixture.children))
			if got != want {
				t.Errorf("strategy=%s workload=%s stats=%+v, want %+v", strategy, workload, got, want)
			}
		}
	}
}

func expectedDirectoryBenchmarkStats(strategy directoryBenchmarkStrategy, workload directoryBenchmarkWorkload, childCount int) directoryBenchmarkStats {
	switch strategy {
	case directoryStrategyOnDemand:
		calls := 1
		if workload == directoryWorkloadOneChild {
			calls = 2
		} else if workload == directoryWorkloadSequential {
			calls += childCount
		}
		return directoryBenchmarkStats{invocations: calls, singleCalls: calls}
	case directoryStrategyBatch:
		return directoryBenchmarkStats{invocations: 1, batchCalls: 1}
	case directoryStrategyPrefetch:
		calls := 1 + childCount
		return directoryBenchmarkStats{invocations: calls, singleCalls: calls}
	default:
		panic("unknown directory benchmark strategy")
	}
}

func runDirectoryBenchmarkWorkload(
	ctx context.Context,
	strategy directoryBenchmarkStrategy,
	workload directoryBenchmarkWorkload,
	fixture *directoryBenchmarkFixture,
	backend *directoryBenchmarkBackend,
) error {
	var remote RemoteFS
	switch strategy {
	case directoryStrategyOnDemand:
		remote = newCachedRemoteFSWithOptions(&directoryBenchmarkSingleBackend{backend: backend}, "benchmark-host", directoryBenchmarkCacheOptions(fixture))
	case directoryStrategyBatch:
		remote = newCachedRemoteFSWithOptions(backend, "benchmark-host", directoryBenchmarkCacheOptions(fixture))
	case directoryStrategyPrefetch:
		remote = newCachedRemoteFSWithOptions(&directoryBenchmarkSingleBackend{backend: backend}, "benchmark-host", directoryBenchmarkCacheOptions(fixture))
	default:
		return fmt.Errorf("unknown directory benchmark strategy %q", strategy)
	}

	if err := listDirectoryBenchmarkPath(ctx, remote, fixture.root); err != nil {
		return err
	}
	if strategy == directoryStrategyPrefetch {
		// The historical strategy started child listings in parallel after the
		// root response. Waiting here makes the benchmark deterministic and
		// measures the cost of warming the same cache before navigation.
		if err := prefetchDirectoryBenchmarkChildren(ctx, remote, fixture.children); err != nil {
			return err
		}
	}

	switch workload {
	case directoryWorkloadFirstView:
		return nil
	case directoryWorkloadOneChild:
		return listDirectoryBenchmarkPath(ctx, remote, fixture.children[0])
	case directoryWorkloadSequential:
		for _, childPath := range fixture.children {
			if err := listDirectoryBenchmarkPath(ctx, remote, childPath); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown directory benchmark workload %q", workload)
	}
}

func listDirectoryBenchmarkPath(ctx context.Context, remote RemoteFS, remotePath string) error {
	_, err := remote.List(ctx, remotePath)
	return err
}

func prefetchDirectoryBenchmarkChildren(ctx context.Context, remote RemoteFS, children []string) error {
	errs := make(chan error, len(children))
	// The original eager-prefetch implementation allowed at most four remote
	// listings at once. Keep that bound here so a wide fixture models multiple
	// waves of SSH work instead of an unrealistically unlimited fan-out.
	const concurrency = 4
	sem := make(chan struct{}, concurrency)
	var group sync.WaitGroup
	group.Add(len(children))
	for _, childPath := range children {
		childPath := childPath
		go func() {
			defer group.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			}
			defer func() { <-sem }()
			errs <- listDirectoryBenchmarkPath(ctx, remote, childPath)
		}()
	}
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func directoryBenchmarkCacheOptions(fixture *directoryBenchmarkFixture) listingCacheOptions {
	return listingCacheOptions{
		TTL:        time.Minute,
		MaxEntries: len(fixture.children) + 2,
	}
}

func BenchmarkDirectoryListingStrategies(b *testing.B) {
	latencies := []struct {
		name  string
		delay time.Duration
	}{
		{name: "ssh_0s", delay: 0},
		{name: "ssh_1ms", delay: time.Millisecond},
		{name: "ssh_5ms", delay: 5 * time.Millisecond},
	}
	fixtures := []struct {
		name      string
		childDirs int
		files     int
	}{
		{name: "small_dir_4x4", childDirs: 4, files: 4},
		{name: "large_dir_16x16", childDirs: 16, files: 16},
	}
	workloads := []directoryBenchmarkWorkload{
		directoryWorkloadFirstView,
		directoryWorkloadOneChild,
		directoryWorkloadSequential,
	}
	strategies := []directoryBenchmarkStrategy{
		directoryStrategyOnDemand,
		directoryStrategyBatch,
		directoryStrategyPrefetch,
	}

	for _, latency := range latencies {
		latency := latency
		b.Run(latency.name, func(b *testing.B) {
			for _, fixtureSpec := range fixtures {
				fixtureSpec := fixtureSpec
				fixture := newDirectoryBenchmarkFixture(fixtureSpec.childDirs, fixtureSpec.files)
				b.Run(fixtureSpec.name, func(b *testing.B) {
					for _, workload := range workloads {
						workload := workload
						b.Run(string(workload), func(b *testing.B) {
							for _, strategy := range strategies {
								strategy := strategy
								b.Run(string(strategy), func(b *testing.B) {
									var total directoryBenchmarkStats
									for i := 0; i < b.N; i++ {
										b.StopTimer()
										backend := newDirectoryBenchmarkBackend(fixture, latency.delay)
										b.StartTimer()
										err := runDirectoryBenchmarkWorkload(context.Background(), strategy, workload, fixture, backend)
										b.StopTimer()
										if err != nil {
											b.Fatal(err)
										}
										stats := backend.stats()
										total.invocations += stats.invocations
										total.singleCalls += stats.singleCalls
										total.batchCalls += stats.batchCalls
									}
									b.ReportMetric(float64(total.invocations)/float64(b.N), "ssh-calls/op")
									b.ReportMetric(float64(total.singleCalls)/float64(b.N), "single-calls/op")
									b.ReportMetric(float64(total.batchCalls)/float64(b.N), "batch-calls/op")
								})
							}
						})
					}
				})
			}
		})
	}
}
