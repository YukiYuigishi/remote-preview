# Issue 006: interactive listing priority over prefetch

## Goal

directory prefetchがユーザーのdirectory移動と競合して、移動先の表示を遅くしないようにする。

## Scope

- ユーザーの`List`要求が来たら実行中のprefetchをキャンセルする。
- キャンセルされたprefetchのsingleflight結果をユーザー要求へ誤って返さず、通常listingを再取得する。
- prefetchの既定件数と同時実行数を抑える。
- prefetch中のdirectoryへ移動する回帰テストを追加する。

## Relevant files

- `internal/preview/cache.go`
- `internal/preview/cache_test.go`
- `PLAN.md`
- `README.md`

## Acceptance criteria

- prefetch中にユーザーが同じdirectoryへ移動した場合、prefetch完了を待たずにユーザーlistingへ切り替わる。
- ユーザー要求によって他のprefetchも停止し、SSH競合を抑える。
- prefetchの既定負荷が現状より小さい。
- `go test ./...`、`go test -race ./...`、`go vet ./...`、`go build ./...`が通る。

## Verification

- slow fake `RemoteFS`によるinteractive priority test
- cache / prefetch regression test
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go build ./...`

## Current state / blocker

- 実装後、prefetch自体を維持するよりon-demand listingへ戻す方針に変更したため、Issue 007で置き換えた。
