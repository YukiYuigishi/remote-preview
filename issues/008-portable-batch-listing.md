# Issue 008: portable batch directory listing

## Goal

directory移動ごとのSSH latencyを減らし、macOSとBusyBoxを含むリモート環境で、1回のSSH commandから複数directoryのlistingを取得してcacheへ分配する。

## Scope

- current directoryと直下directoryのlistingを1回のremote shell commandで取得する。
- GNU `find -printf`やBSD固有の`find` optionに依存しない。
- NUL区切りのrecord protocolでtab/newlineを含む名前を壊さない。
- 直下directory数に上限を設け、巨大な再帰走査は行わない。
- batch対応backendで失敗した場合も通常のsingle-directory listingへfallbackできる構造にする。

## Relevant files

- `internal/preview/remote.go`
- `internal/preview/cache.go`
- `internal/preview/*_test.go`
- `README.md`

## Acceptance criteria

- batch listingが1回のSSH commandでcurrentと直下directoryのlistingを取得する。
- macOS `/bin/sh`とBusyBox `sh`で使えるPOSIX shell syntaxだけを使う。
- tab/newlineを含むentry nameをbatch parserが保持する。
- batch結果のchild listingがcacheされ、childへの移動で追加SSH listingが発生しない。
- `go test ./...`、`go test -race ./...`、`go vet ./...`、`go build ./...`が通る。

## Verification

- batch protocol parser unit test
- fake backendによるcache分配・child navigation test
- local POSIX shell command smoke test
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go build ./...`

## Current state / blocker

- 完了。POSIX shellとNUL区切りprotocolによるbatch listing、cache分配、batch失敗時の通常listing fallbackを実装した。
- `go test ./...`、`go test -race ./...`、`go vet ./...`、`go build ./...`で検証済み。
