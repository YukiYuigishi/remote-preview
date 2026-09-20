# Issue 014: open the preview browser by default

## Goal

remote-preview起動時に、生成したpreview URLをデフォルトで既定ブラウザへ開く。

## Scope

- `-open`の既定値を有効にする。
- 自動起動を抑制するための`-open=false`を維持する。
- READMEとPLANのCLI仕様を更新する。
- 既定値と無効化方法をテストで固定する。

## Relevant files

- `internal/preview/main.go`
- `internal/preview/main_test.go`
- `README.md`
- `PLAN.md`

## Acceptance criteria

- 引数なしで起動した場合、preview URLを既定ブラウザで開く。
- `-open=false`でブラウザ自動起動を無効にできる。
- `-open`を明示した既存の呼び出しも動作する。
- READMEにデフォルト動作と無効化方法が記載されている。
- `go test ./...`、`go test -race ./...`、`go vet ./...`、`go build ./...`が通る。

## Verification

- open flagの既定値・無効化値のunit test
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go build ./...`

## Current state / blocker

- 完了。
- `-open`の既定値を`true`に変更し、`-open=false`で自動起動を抑制できるようにした。
- READMEとPLANをデフォルト動作に合わせて更新した。

## Changed files

- `internal/preview/main.go`
- `internal/preview/main_test.go`
- `README.md`
- `PLAN.md`
- `issues/014-default-open-browser.md`

## Design decisions

- 既存の`-open` flagは互換性のため残し、明示指定なしでも同じ自動起動処理を実行する。
- GUIがない環境やターミナルのみで使う場合は、`-open=false`で明示的に無効化する。

## Verification

- open flagの既定値・`-open=false`をunit testで確認
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go build ./...`
