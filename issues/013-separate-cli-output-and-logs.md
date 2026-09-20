# Issue 013: separate user output from structured logs

## Goal

ユーザー向けの起動案内とdiagnostic logを分離し、terminal上のURLをそのままクリックできる状態を維持する。

## Scope

- server起動時の`browsing` / `open`案内を通常の文字列としてstdoutへ出力する。
- `slog`はHTTP、SSH、cache、helper、verbose request、errorなどのdiagnostic logに限定する。
- URL案内のformatをテストで固定する。
- READMEの起動例とログ説明を必要に応じて更新する。

## Relevant files

- `internal/preview/main.go`
- `internal/preview/main_test.go`
- `cmd/remote-preview/main.go`
- `internal/preview/logging.go`
- `README.md`

## Acceptance criteria

- 起動時に`open: http://.../`形式のplain text URLがstdoutへ出る。
- 起動案内に`slog`の`time=... level=...`等を混ぜない。
- `DEBUG=1`を指定しても通常出力のURL formatは変わらない。
- diagnostic logは引き続きstructured `slog` outputとしてstderrへ出る。
- `-open`の自動ブラウザ起動を維持する。
- `go test ./...`、`go test -race ./...`、`go vet ./...`、`go build ./...`が通る。

## Verification

- startup output unit test
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go build ./...`

## Current state / blocker

- 完了。
- `writeStartupInfo`を追加し、起動時の`browsing` / `open`案内をstdoutへplain textで出力するよう変更した。
- `slog`のstartup logを削除し、既存のdiagnostic logは`stderr`のまま維持した。
- `DEBUG=1`でもURL案内のformatが変わらないことをunit testで固定した。

## Changed files

- `internal/preview/main.go`
- `internal/preview/main_test.go`
- `README.md`
- `issues/013-separate-cli-output-and-logs.md`

## Design decisions

- terminalのURL検出に依存する起動案内はstructured logへ混ぜず、従来の`open: <URL>`形式を維持する。
- HTTP、SSH、cache、helper、verbose request、errorのdiagnostic logは既存の`log/slog`を使い、`logging.go`のstderr出力を維持する。
- `-open`の自動ブラウザ起動処理と、background prefetchなしの方針は変更しない。

## Verification

- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go build ./...`
