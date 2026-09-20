# Issue 002: target shorthand、remote home、URL encoding

## Goal

`host:/path`を維持しつつ、`host`だけでリモートhomeを閲覧できるようにし、特殊文字を含むremote pathを正しく辿れるようにする。

## Scope

- `host`をremote home targetとして扱う。
- remote `$HOME`を一度解決する。
- path segment単位でURLを生成する。
- breadcrumb、Parent、directory entryのリンク生成を共通化する。
- `-addr :0`で実際のlisten addressを表示・`-open`に渡す。

## Relevant files

- `cmd/remote-preview/*.go`
- `README.md`
- `cmd/remote-preview/main_test.go`

## Acceptance criteria

- `./remote-preview remote-host:/path/to/dir`が動く。
- `./remote-preview remote-host`でリモートhomeを開ける。
- `?`、`#`、空白、日本語、`%`を含む名前を一覧から辿れる。
- pathの`..`でtarget rootを越えない。
- `-addr :0`で実際のURLが表示される。

## Verification

- target parser / URL helper unit test
- fake `RemoteFS`によるhome解決とHTTP handler test
- `go test ./...`
- `go vet ./...`
- `go build ./...`

## Current state / blocker

- 完了。host shorthand、remote home解決、segment単位URL、実listen address表示を実装済み。
- `go test ./...`、`go vet ./...`、`go build ./...`で検証済み。
