# Issue 024: remove production dead code

## Goal

静的解析で確定したproduction未到達の関数を削除し、test-onlyの依存を実際のproduction経路を検証するtestへ整理する。

## Scope

- `resolveTarget`、`remoteHelperPlatform`、`(*sshRemoteFS).run`を削除する。
- 削除対象だけを参照するtestを削除または現行production経路のtestへ置き換える。
- helper/shell fallback、target解決、SSH cancellationのproduction挙動は維持する。

## Relevant files

- `internal/preview/target.go`
- `internal/preview/remote.go`
- `internal/preview/main_test.go`
- `internal/preview/remote_test.go`

## Acceptance criteria

- production sourceから上記3関数がなくなる。
- `deadcode`と`staticcheck -tests=false`でproduction dead codeが報告されない。
- target home解決とSSH cancellationの既存production挙動をtestする。

## Verification

- `go test ./...`
- `go vet ./...`
- `go build ./...`
- `deadcode ./...`
- `staticcheck -tests=false ./...`

## Current state / blocker

- 未着手。削除対象はdeadcode/staticcheckの両方で確認済み。
