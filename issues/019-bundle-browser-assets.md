# Issue 019: bundle browser preview assets

## Goal

remote-previewをインターネット接続なしでもMarkdown、Mermaid、syntax highlightまで利用できる自己完結したCLIにする。

## Scope

- marked、Mermaid、highlight.jsのbrowser bundleをversion固定してrepositoryへ同梱する。
- Go `embed`でremote-preview binaryへassetを含める。
- local HTTP endpointからbrowserへassetを配信する。
- templateからjsDelivrなど外部CDNへのruntime requestを削除する。
- third-party versionとlicenseをrepositoryへ記録する。
- CDN unavailable時のfallbackは維持し、asset取得失敗時もsource/plain textを表示する。

## Relevant files

- `internal/preview/assets.go`
- `internal/preview/assets/*`
- `internal/preview/handler.go`
- `internal/preview/templates.go`
- `internal/preview/handler_file_test.go`
- `README.md`
- `PLAN.md`
- `THIRD_PARTY_NOTICES.md`

## Acceptance criteria

- clean checkoutの`remote-preview` binaryだけでMarkdown viewerが動作する。
- Mermaidとsyntax highlightのJS/CSSをlocalhostから配信する。
- templateに外部CDN URLを残さない。
- asset endpointがGET/HEADに対応し、未知assetや不正pathを404にする。
- Markdown/Mermaid/highlight asset取得失敗時もsource/plain textへfallbackする。
- bundled assetsのversion、source、licenseが記録されている。
- `go test ./...`、`go test -race ./...`、`go vet ./...`、`go build ./...`が通る。

## Design decisions

- browser assetは`go:embed`し、remote filesystem pathとは別のreserved local HTTP pathで配信する。
- runtime CDN依存を削除する。これによりProxyJumpやremote側の環境に加え、local browserの外部network依存も減らす。
- dependency管理を複雑化しないため、npm build pipelineではなくupstreamのbrowser bundleをversion固定して同梱する。
- third-party license noticeをrootの`THIRD_PARTY_NOTICES.md`へ置く。

## Verification

- asset embedとGET/HEAD/404 test
- templateが外部CDN URLを含まないtest
- local browser assetのcontent type test
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go build ./...`

## Current state / blocker

- 実装完了。

## Changed files

- `internal/preview/assets.go`
- `internal/preview/assets_test.go`
- `internal/preview/assets/marked.js`
- `internal/preview/assets/mermaid.js`
- `internal/preview/assets/highlight.js`
- `internal/preview/assets/highlight.css`
- `internal/preview/handler.go`
- `internal/preview/templates.go`
- `README.md`
- `PLAN.md`
- `THIRD_PARTY_NOTICES.md`

## Design decisions

- browser assetは`go:embed`でremote-preview binaryへ同梱し、versionedな`/_remote-preview/assets/v1/`からGET/HEAD配信する。asset versionを更新した場合はURL versionも更新し、immutable cacheを安全に利用する。
- npm build pipelineは導入せず、upstream browser bundleをversion固定して同梱した。
- runtime CDN依存を削除し、asset取得やrich renderingに失敗した場合は既存のsource/plain text fallbackを使う。
- Mermaid bundleによりbinary sizeは増えるが、初期実装では圧縮展開などの複雑な仕組みを導入しない。

## Verification result

- `go test ./...`: passed
- `go test -race ./...`: passed
- `go vet ./...`: passed
- `go build ./...`: passed
- `make check`: passed
- `make install`: passed; installed `/Users/yuki/go/bin/remote-preview`
- asset endpointのGET/HEAD/404をunit testで確認。
- templateにruntime CDN URLがないことをunit testで確認。
- installed binaryからversioned asset endpointをcurlし、embedded JavaScriptの配信を確認。
- browserでlocalhostのasset表示を試行したが、接続中のChrome extensionがloopback URLを`ERR_BLOCKED_BY_CLIENT`として遮断したため、ブラウザ上の実表示確認は完了できなかった。

## Remaining limitations

- bundled Mermaidにより実行ファイルサイズが増える。
- Windows remote helperは今回のscope外で、現状は既存shell fallbackを試みる。
