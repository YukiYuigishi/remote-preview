# Issue 030: audio/video preview and range requests

## Goal

音声・動画をブラウザで再生し、seek時にファイル全体を読み直さず必要なbyte rangeだけを転送する。

## Scope

- `.mp3`、`.wav`、`.ogg`、`.m4a`、`.flac`を音声として分類する。
- `.mp4`、`.webm`、`.mov`、`.ogv`を動画として分類する。
- audio / video elementを使うmedia previewを追加する。
- local / remote targetのfile size取得とstreaming readをpreview handlerから利用できるようにする。
- 単一のHTTP byte range requestを処理し、`206 Partial Content`、`Content-Range`、`Accept-Ranges`を返す。
- HEAD requestではfile bodyを読み込まない。
- browserが再生できないcodecの場合は、Rawまたはdownloadを選べるようにする。

## Relevant files

- `internal/preview/handler.go`
- `internal/preview/templates.go`
- `internal/preview/remote.go`
- `internal/preview/local.go`
- `internal/preview/cache.go`
- `internal/preview/transfer.go`
- `internal/preview/handler_file_test.go`
- `internal/preview/remote_test.go`
- `internal/preview/local_test.go`
- `README.md`
- `PLAN.md`

## Acceptance criteria

- 対応拡張子への通常のGETでaudio / video viewerを表示する。
- media elementが参照するraw endpointは、RangeなしのGET、HEAD、単一のRange GETを処理する。
- `bytes=start-end`、open-ended range、suffix rangeを処理する。
- unsatisfiable rangeへ`416 Range Not Satisfiable`と`Content-Range: bytes */size`を返す。
- remote targetのRange GETでは、要求範囲より前のbyteをSSH経由で転送しない。
- large media fileを`[]byte`へ全量読み込みしない。
- request cancellationでlocal fileとSSH commandを終了する。
- 既存のdownload/upload、HTML、SVG、Markdown、text previewを壊さない。

## Verification

- Range parserとresponse headerのunit test
- local / fake remoteによるfull、HEAD、partial response test
- remote streaming commandのargumentとcancellation test
- audio / video handler test
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go build ./...`

## Current state

- Implemented.
- media extensionはfile bodyを読む前にaudio/video viewerへ分類する。codecに対応しないbrowser向けにRawとDownloadを表示する。
- local backendはstat + seek streamを使い、SSH backendはLinux/BSD statとremote-side tail/headによるoffset skipを使う。
- raw responseはsingle Range、open-ended/suffix range、416、HEADを処理する。streaming SSH commandは固定command timeoutを使わず、request contextで終了する。

## Verification

- `go test ./...` — pass
- `go test -race ./...` — pass
- `go vet ./...` — pass
- `go build ./...` — pass
- Range parser / response test covers normal, open-ended, suffix, clamped end, unsatisfiable and multi-range rejection.
- Local and fake remote tests cover full response, HEAD without `OpenRange`, partial response, media viewer without `Read`, and content MIME.
- Remote range script test confirms that only requested bytes are emitted; SSH stream test confirms request cancellation and no fixed command deadline.
