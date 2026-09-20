# Issue 004: directory listing cache / prefetch

## Goal

directory listingだけをTTL付きでcacheし、同時アクセスの重複SSHを抑えながら、親と直下directoryをバックグラウンドでprefetchする。

## Scope

- remote hostと正規化済みremote pathを含むcache keyを追加する。
- TTL、最大エントリ数、singleflight相当の同時取得抑制を実装する。
- 初回のcurrent directory listingを同期取得し、親と直下directoryを非同期prefetchする。
- cached listingのentry kindで不要な`Kind`呼び出しを省略する。
- file contentはcacheしない。

## Relevant files

- `cmd/ykview/cache.go`
- `cmd/ykview/remote.go`
- `cmd/ykview/main.go`
- `cmd/ykview/handler.go`
- `cmd/ykview/*_test.go`

## Acceptance criteria

- 同じdirectoryの再表示でSSH listingが発生しない。
- 初回directoryの表示はprefetch完了を待たない。
- 親と直下directoryのcacheがバックグラウンドで温まる。
- TTL切れ、同時アクセス、SSH失敗時の挙動がテストされている。

## Verification

- cache / singleflight / prefetch unit test
- fake `RemoteFS`によるhandler test
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go build ./...`

## Current state / blocker

- 初期実装は完了したが、interactive latencyとの競合が確認されたため、prefetch部分はIssue 007で削除した。
- TTL/max entries/singleflight相当とcached kind lookupは維持している。
