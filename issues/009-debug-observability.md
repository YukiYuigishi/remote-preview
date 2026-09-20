# Issue 009: DEBUG logging and browser latency verification

## Goal

人間が初回表示・directory移動の遅延原因を追えるdebug logを追加し、ローカルHTTP画面をブラウザ操作で検証できるようにする。

## Scope

- `DEBUG=1`でHTTP、cache、batch/fallback、SSHの開始・終了・durationを出力する。
- remote path、entry count、bytes、errorをログに含める。
- READMEにdebug起動方法を記載する。
- Browser操作で初回表示とdirectory移動を確認する。

## Relevant files

- `internal/preview/logging.go`
- `internal/preview/logging_test.go`
- `internal/preview/handler.go`
- `internal/preview/cache.go`
- `internal/preview/remote.go`
- `README.md`

## Acceptance criteria

- `DEBUG=1`で通常動作を壊さずdebug logが出る。
- cache hit/miss、batch結果、SSH command duration、HTTP request durationを区別できる。
- `DEBUG`未設定時は追加debug logを出さない。
- `go test ./...`、`go test -race ./...`、`go vet ./...`、`go build ./...`が通る。
- ブラウザで初回表示と子directory遷移を確認し、ログと体感を照合できる。

## Verification

- debug-enabled unit/log smoke test
- local browser interaction
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go build ./...`

## Current state / blocker

- `log/slog`のtext handlerを導入し、`DEBUG=1`でdebug levelを有効化した。
- HTTP、cache、batch/fallback、SSH commandにstructured key-value logを追加した。
- READMEに起動方法を追記した。
- `go test ./...`、race、vet、build、browser操作の検証を完了した。
- `go test -race ./...`、`go vet ./...`、`go build ./...`も通過した。
- DEBUG=1で実ホストをブラウザ操作し、初回root listingと子directory遷移を計測した。
- 初回rootのbatchで子directoryのlistingがcacheされ、遷移時にSSH commandが発生しないことを確認した。
- Chromeの自動`/favicon.ico` requestはmissing pathの判定で別SSHを発生させることが分かった。画面遷移の計測とは独立しており、不要なfavicon lookupの扱いは後続改善候補とする。
