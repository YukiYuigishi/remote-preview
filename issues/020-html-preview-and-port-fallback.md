# Issue 020: direct HTML preview and automatic port fallback

## Goal

HTMLファイルをRawリンク経由ではなく通常のファイルリンクからブラウザで直接表示し、指定listen portが使用中の場合は次の空きportへ自動的に移動する。

## Scope

- `.html` / `.htm` fileをtext viewerではなく、`text/html`として直接配信する。
- HTML fileの通常リンクとRawリンクの挙動をテストで固定する。
- 指定portが`EADDRINUSE`の場合、同じlisten hostでportを順に増やしてbindを試す。
- `:0`、IPv4、IPv6、hostnameを含むlisten addressの既存挙動を維持する。
- 指定port以外のlisten errorは自動fallbackせず、そのまま返す。
- READMEとPLANの仕様を更新する。

## Relevant files

- `internal/preview/handler.go`
- `internal/preview/handler_file_test.go`
- `internal/preview/main.go`
- `internal/preview/main_test.go`
- `README.md`
- `PLAN.md`

## Acceptance criteria

- HTML fileへの通常のGETで、HTML documentがそのままresponse bodyとして返る。
- HTML fileのContent-Typeが`text/html`で、text viewer templateやdownload attachmentにならない。
- `?raw=1`でもHTML bytesをそのまま返す。
- 指定portが使用中なら、同じhostの後続portでserverを起動できる。
- 使用中でない指定port、`-addr :0`、IPv6 addressは従来どおり動作する。
- permission errorなど`EADDRINUSE`以外のlisten errorは隠さない。
- `go test ./...`、`go test -race ./...`、`go vet ./...`、`go build ./...`が通る。

## Verification

- HTML handler test
- occupied-port fallback test
- listen address parsing / fallback boundary test
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go build ./...`

## Current state / blocker

- 実装完了。
- HTML fileをtext viewerより先に直接配信し、通常リンクからブラウザで表示できるようにした。
- 数値listen portが`EADDRINUSE`の場合、同じhostの後続portへ順にfallbackするようにした。`:0`と他のlisten errorは従来どおり扱う。

## Changed files

- `internal/preview/handler.go`
- `internal/preview/handler_file_test.go`
- `internal/preview/main.go`
- `internal/preview/main_test.go`
- `README.md`
- `PLAN.md`
- `issues/020-html-preview-and-port-fallback.md`

## Verification

- HTML direct response test
- HTML `?raw=1` response test
- occupied/free/ephemeral port behavior tests
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go build ./...`

## Verification results

- 上記のHTML、port fallback unit testを追加・通過。
- `go test ./...`: passed
- `go test -race ./...`: passed
- `go vet ./...`: passed
- `go build ./...`: passed
