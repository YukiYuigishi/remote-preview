# Issue 007: remove eager directory prefetch

## Goal

directory移動直後のSSH競合を避け、必要なdirectoryだけをon-demandでlistingする。

## Scope

- background directory prefetchを削除する。
- foregroundのbatch listingは許可する。
- directory listingのTTL cacheとsingleflight相当は維持する。
- prefetch用のcontext、semaphore、設定、テストを削除する。
- README、PLAN、関連issueに現在の設計を反映する。

## Relevant files

- `internal/preview/cache.go`
- `internal/preview/cache_test.go`
- `internal/preview/handler_cache_test.go`
- `PLAN.md`
- `README.md`
- `issues/004-directory-cache-prefetch.md`
- `issues/006-prefetch-priority.md`

## Acceptance criteria

- directory一覧表示後にbackgroundの親/子directory SSH listingが発生しない。
- ユーザーが移動したdirectoryだけをlistingする。
- TTL cache、最大エントリ数、同時アクセスのsingleflight、cached entry kindは維持する。
- `go test ./...`、`go test -race ./...`、`go vet ./...`、`go build ./...`が通る。

## Verification

- fake `RemoteFS`でon-demand listingとcache再利用を確認する。
- prefetch関連の設定・コード・テストが残っていないことを確認する。
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go build ./...`

## Current state / blocker

- 完了。background directory prefetchを削除し、foreground batch/on-demand listing、TTL cache、singleflight、cached entry kindを維持した。
- `go test ./...`、`go test -race ./...`、`go vet ./...`、`go build ./...`で検証済み。
