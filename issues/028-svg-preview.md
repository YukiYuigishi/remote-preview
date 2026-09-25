# Issue 028: direct SVG preview

## Goal

SVGファイルをtext viewerではなく、通常のファイルリンクからブラウザで直接previewできるようにする。

## Scope

- `.svg` fileをSVG imageとして直接配信する。
- 通常表示と`?raw=1`のresponseをtestで固定する。
- directory listingの既存image分類を維持する。

## Relevant files

- `internal/preview/handler.go`
- `internal/preview/handler_file_test.go`
- `PLAN.md`

## Acceptance criteria

- `.svg`への通常のGETがSVG bytesをそのまま返す。
- responseの`Content-Type`が`image/svg+xml`で、text viewer templateを含まない。
- `.SVG`のような大文字拡張子でも同じ挙動になる。
- `?raw=1`でもSVG bytesをそのまま返す。
- 既存のHTML、Markdown、text、binary previewを壊さない。

## Verification

- SVG handler unit test
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go build ./...`

## Current state

- Completed.
- `.svg`をtext判定より先に直接配信し、大文字小文字を問わず`image/svg+xml`を返す。通常GETと`?raw=1`を含むhandler testを追加した。
- Verification: `go test ./internal/preview`、`go test ./...`、`go test -race ./...`、`go vet ./...`、`go build ./...`。
