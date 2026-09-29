# 033: Keep Markdown files in the preview path

## Goal

Markdown fileを開いたときに意図せずraw responseとなり、browserがdownloadする挙動を直す。

## Scope

- `.md`など既知のMarkdown拡張子は、先頭byteのtext sniffingでrawへ切り替えない。
- 未知拡張子のtext sniffingと明示的なRaw表示は維持する。
- 実環境のhost名、user名、file path、内容は記録しない。

## Relevant files

- `internal/preview/handler.go`
- `internal/preview/handler_file_test.go`

## Acceptance criteria

- Markdownへの通常GETは、先頭に制御byteや無効なUTF-8があってもHTML viewerを返す。
- 大きいMarkdownも同じviewer経路を使う。
- `?raw=1`は従来どおり元のbytesを返す。
- 未知拡張子のbinary判定は維持する。

## Verification

- handler regression testで通常GET、Raw、未知拡張子を確認する。
- `go test ./...`、`go vet ./...`、`go build ./...`を実行する。
- 実remoteでの再確認は接続可能なら実施する。

## Current state

完了。再接続で対象の先頭1 KiBがUTF-8文字の途中で終わることを確認した。text sniffingがrawと誤判定していたため、Markdown拡張子をtext sniffingから除外した。

## Verification results

- UTF-8文字が1 KiB境界で分割される匿名のMarkdown fixtureで、修正前に`application/octet-stream`を再現し、修正後はHTML viewerになることを確認した。
- 同fixtureの`?raw=1`は元のbytesを返し、未知拡張子のbinaryはrawのままとなる。
- `make check`（test、race、vet、CLI build）: passed。
- 実remoteの通常GET: HTTP 200、`text/html; charset=utf-8`、`Content-Disposition`なし。`?raw=1`: HTTP 200、元のfile sizeのbytesを返す。
