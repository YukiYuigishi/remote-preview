# Issue 017: improve text preview and syntax highlighting

## Goal

text系ファイルをdownload扱いにせず、ykviewのブラウザ上で常に読みやすく表示する。可能な場合はsyntax highlightも提供する。

## Scope

- 既知のtext/code extensionを拡張し、`?view=1`なしでtext viewerを表示する。
- 未知のextensionでも、UTF-8かつNUL byteを含まない内容をtextとしてbrowser previewする。
- binaryやHTMLなど既存のraw表示対象は壊さない。
- text viewerへhighlight.jsのbrowser-side syntax highlightingを追加する。
- CDNが利用できない場合はplain text表示へfallbackする。
- READMEとPLANへtext preview・CDN依存を記録する。

## Relevant files

- `internal/preview/handler.go`
- `internal/preview/templates.go`
- `internal/preview/cache_test.go`
- `internal/preview/handler_file_test.go`
- `internal/preview/main_test.go`
- `README.md`
- `PLAN.md`

## Acceptance criteria

- `.txt`、source code、JSON/YAML等を通常のfile linkからbrowser内で表示できる。
- 既知のtext fileに`Content-Disposition: attachment`や`application/octet-stream`を設定しない。
- 未知extensionのUTF-8 text fileもbrowser内で表示できる。
- NUL byteを含むbinaryはtext previewへ誤判定しない。
- `Raw` linkは従来どおり取得できる。
- syntax languageを拡張子から決定し、highlight.js CDN失敗時もsource textが表示される。
- special characterを含むsourceがHTML/JavaScript injectionにならない。
- `go test ./...`、`go test -race ./...`、`go vet ./...`、`go build ./...`が通る。

## Design decisions

- text判定はfilename extensionを優先し、未知extensionはUTF-8 validかつNULなしのcontent sniffingで補助する。
- syntax highlightingは既存のMarkdown/Mermaidと同じbrowser-side CDN方式にし、Go module dependencyを増やさない。
- CDN失敗時はsyntax highlightだけを諦め、plain text viewerは維持する。
- HTMLは既存どおりraw HTML previewとし、今回のtext fallbackでHTML sourceへ変更しない。

## Verification

- extension/contentによるtext判定unit test
- handler responseのContent-Typeとtext viewer test
- syntax language mapping test
- HTML escaping / source JSON embedding test
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go build ./...`

## Current state / blocker

- 完了。
- 既知extensionのtext判定を拡張し、未知extensionもUTF-8/NUL判定でbrowser内previewするようにした。
- text viewerへhighlight.js CDNを追加し、CDN失敗時はplain textを維持する。

## Changed files

- `internal/preview/handler.go`
- `internal/preview/templates.go`
- `internal/preview/cache_test.go`
- `internal/preview/handler_file_test.go`
- `README.md`
- `issues/017-text-preview-and-syntax-highlight.md`
- `PLAN.md`

## Verification

- known/unknown text、binary、handler Content-Type、syntax language、source safetyのunit test
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go build ./...`

## Remaining limitations

- syntax highlightはbrowserからjsDelivrへ接続できる場合だけ有効になる。
- file contentは従来どおり全量をmemoryへ読み込むため、巨大file対応はPhase 5の残課題。
