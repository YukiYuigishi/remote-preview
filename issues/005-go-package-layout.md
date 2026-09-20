# Issue 005: Go package layout

## Goal

CLI entrypointとアプリケーション実装を分離し、Goの標準的な`cmd` / `internal`構成にする。

## Scope

- `cmd/ykview`には薄い`main.go`だけを残す。
- 実装とテストを`internal/preview` packageへ移す。
- CLIの公開entrypointを`preview.Run`にする。
- 既存の動作、package内テスト、build commandを維持する。

## Relevant files

- `cmd/ykview/main.go`
- `internal/preview/*.go`
- `go.mod`

## Acceptance criteria

- `cmd/ykview`がflag引数を受け取り`internal/preview.Run`へ委譲する。
- アプリケーション実装とテストが`internal/preview`に配置される。
- `go test ./...`、`go test -race ./...`、`go vet ./...`、`go build ./...`が通る。
- 既存のCLI挙動を維持する。

## Verification

- package layout確認
- regression test
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go build ./...`

## Current state / blocker

- 完了。`cmd/ykview`を薄いentrypointにし、実装とテストを`internal/preview`へ移動済み。
- `go test ./...`、`go test -race ./...`、`go vet ./...`、`go build ./...`で検証済み。
