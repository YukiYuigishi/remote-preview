# Issue 029: structured data preview

## Goal

CSV / TSVを表として閲覧できるようにし、JSON Linesを行単位のJSONとして読みやすく表示する。

## Scope

- `.csv`と`.tsv`に表形式のpreviewを追加する。
- 表形式とsource表示を切り替えられるようにする。
- `.jsonl`と`.ndjson`をtext fileとして明示的に分類し、JSONとしてsyntax highlightする。
- 表示する行数、列数、field sizeに上限を設け、上限を超えた場合もsourceを表示できるようにする。
- directory listingでCSV / TSVとJSON Linesの種別を識別できるようにする。

## Relevant files

- `internal/preview/handler.go`
- `internal/preview/templates.go`
- `internal/preview/handler_file_test.go`
- `internal/preview/main_test.go`
- `README.md`
- `PLAN.md`

## Acceptance criteria

- `.csv`と`.tsv`への通常のGETで、headerとrecordを表として表示できる。
- quoted field、separatorを含むfield、改行を含むCSV fieldを正しく解析する。
- 空のfile、headerだけのfile、行ごとに列数が異なるfileでもhandlerが失敗しない。
- 表示上限を超えた場合は省略を明示し、source表示を選べる。
- `.jsonl`と`.ndjson`をtext viewerで表示し、JSONのsyntax highlightを試行する。
- parseまたはbrowser-side renderingに失敗してもsourceを表示できる。
- 既存のMarkdown、HTML、SVG、text、binary previewを壊さない。

## Verification

- CSV / TSV parserと表示上限のunit test
- CSV / TSV / JSON Linesのhandler test
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go build ./...`

## Current state

- Planned.

