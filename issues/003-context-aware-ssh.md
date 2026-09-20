# Issue 003: context-aware system SSH backend

## Goal

system `ssh` backendに明示的な接続・コマンドtimeoutを追加し、HTTP request contextのキャンセルでSSH処理を終了できるようにする。

## Scope

- `sshRemoteFS`の実行処理にtimeoutを追加する。
- OpenSSHの接続timeoutを明示する。
- command factoryを差し替え可能にしてbackendの挙動をテストできるようにする。
- Go SSHへの移行は行わない。

## Relevant files

- `cmd/remote-preview/remote.go`
- `cmd/remote-preview/main.go`
- `cmd/remote-preview/*_test.go`

## Acceptance criteria

- SSH接続・remote command・file readがcontextキャンセルで終了する。
- 接続・コマンドに明示的なtimeoutがある。
- system `ssh` backendをfake commandまたはfake `RemoteFS`に差し替えてテストできる。

## Verification

- timeout / cancellation unit test
- `go test ./...`
- `go vet ./...`
- `go build ./...`

## Current state / blocker

- 完了。context連動、command timeout、OpenSSH ConnectTimeout、差し替え可能なcommand factoryを実装済み。
- `go test ./...`、`go vet ./...`、`go build ./...`で検証済み。
