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
- `.github/workflows/ci.yml`

## Acceptance criteria

- production sourceから上記3関数がなくなる。
- `deadcode`と`staticcheck -tests=false`でproduction dead codeが報告されない。
- target home解決とSSH cancellationの既存production挙動をtestする。
- GitHub Actionsがtest、race、vet、build、deadcode、staticcheckを実行できる。

## Verification

- Go 1.23: `go test ./...`、`go vet ./...`、`go build ./...`
- Go 1.26: `go run golang.org/x/tools/cmd/deadcode@v0.50.0 ./...`、`go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 -tests=false ./...`
- GitHub Actions workflow: `go test -race ./...` を含むmatrix全項目

## Current state / blocker

- 完了。`resolveTarget`、`remoteHelperPlatform`、`(*sshRemoteFS).run`と、それらだけに依存するtestを削除した。
- home-relative targetの解決testは`setResolvedHome`を直接検証し、SSH cancellation testは実運用の`runNamed`境界を検証するよう整理した。
- `.github/workflows/ci.yml`を追加し、GitHub Actionsのubuntu runnerでGo 1.23/1.26のtest、race、vet、buildを実行する。deadcodeとstaticcheckはGo 1.26 jobで実行する。
- Go 1.23での`go test ./...`、`go vet ./...`、`go build ./...`、Go 1.26での`go run golang.org/x/tools/cmd/deadcode@v0.50.0 ./...`、`go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 -tests=false ./...`、`actionlint`は成功した。静的解析toolはGo 1.26を必要とするため固定versionにした。localのrace testはgcc未導入のため実行不可だが、Actionsは`CGO_ENABLED=1`のubuntu runnerで実行する。
