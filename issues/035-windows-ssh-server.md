# Windows SSH server support

## Goal

ykviewからWindowsのOpenSSHサーバー上のファイルを閲覧・転送できるようにする。

## Scope

- SSH接続後にWindowsサーバーを判定し、PowerShellの標準機能でファイル操作を実行する。
- Windowsのdrive absolute pathとhome-relative pathを解決する。
- directory listing、file preview、Range streaming、download、uploadを対応させる。
- POSIXサーバー向けの既存コマンドとhelper動作を維持する。
- Windows CIでPowerShellコマンドを実行して検証する。
- `Makefile`の`SERVER_OS`で接続可能なサーバーOSを選び、不要なhelper artifactを埋め込まない。

## Relevant files

- `internal/preview/remote.go`, `target.go`, `main.go`, `handler_transfer.go`
- `internal/preview/*test.go`, `Makefile`, `.github/workflows/ci.yml`, `README.md`

## Acceptance criteria

- Windows OpenSSHサーバーの既定shellがcmd.exeまたはPowerShellでもコマンドを実行できる。
- `host`、`host:~/path`、`host:C:/path`でWindows上の対象を指定できる。
- directory/file preview、raw Range、download、file/directory uploadが動く。
- POSIX向けtestsとWindows client testsに回帰がない。
- `SERVER_OS=linux|darwin|windows|all`の各buildが通り、指定外OSへの接続を拒否する。

## Verification

- Windows CIでPowerShell操作をlocal fake SSH経由で実行する。
- `go test ./...`, `go test -race ./...`, `go vet ./...`, `go build ./...`。

## Current state

実装済み。CI run 36878862776でWindows、Linux Go 1.23、Linux Go 1.26の全jobが成功した。

## Verification results

- `go test ./...`, `go test -race ./...`, `go vet ./...`, `go build ./...`: 成功
- `server_linux`、`server_darwin`、`server_windows`の各tagでtestsとbuild: 成功
- Windows amd64のtest binaryをcross compileし、Windows amd64/arm64 build: 成功
- 手元のmacOS buildでは、既定19,395,554 bytesに対し`SERVER_OS=windows`は16,390,322 bytes
- CI失敗後の修正では`go test ./...`, `go test -race ./...`, `go vet ./...`, `go build ./...`, `go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 -tests=false ./...`: 成功
- CI run 36878108791: Linux Go 1.23/1.26 job成功。Windows jobは既存ファイルへの再uploadで失敗。
- 一時backupへの修正後、`go test ./...`、staticcheck、Windows amd64 test binaryのcross compile: 成功。
- CI run 36878862776: Windows native操作テスト、Windows server限定テストとbuild、Linux両jobを含む全job成功。
