# Issue 001: 責務分割と RemoteFS interface

## Goal

HTTP handlerからSSH実装を切り離し、target、remote access、handler、templateを読みやすいファイルへ分割する。

## Scope

- `main.go` の責務を分割する。
- `RemoteFS` interfaceを追加する。
- 現行のsystem `ssh` backendの挙動は維持する。
- このissueではGo SSHへの移行、timeout、cacheは扱わない。

## Relevant files

- `cmd/ykview/main.go`
- `cmd/ykview/main_test.go`
- `cmd/ykview/*.go`

## Acceptance criteria

- handlerは`RemoteFS`経由でremote accessする。
- fake `RemoteFS`を使うhandler testを追加できる構造になる。
- 既存のCLIとpreview挙動を維持する。
- `go test ./...`、`go vet ./...`、`go build ./...`が通る。

## Verification

- Unit test
- `go test ./...`
- `go vet ./...`
- `go build ./...`

## Current state / blocker

- 完了。RemoteFS境界、handler、target、remote、browser、templateを分離済み。
- `go test ./...`、`go vet ./...`、`go build ./...`で検証済み。
