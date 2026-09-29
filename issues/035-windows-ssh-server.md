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

実装済み。Windows nativeのPowerShell実行テストはCI jobに追加した。手元にWindows runtimeがないため、CI実行結果は未確認。

## Verification results

- `go test ./...`, `go test -race ./...`, `go vet ./...`, `go build ./...`: 成功
- `server_linux`、`server_darwin`、`server_windows`の各tagでtestsとbuild: 成功
- Windows amd64のtest binaryをcross compileし、Windows amd64/arm64 build: 成功
- 手元のmacOS buildでは、既定19,395,554 bytesに対し`SERVER_OS=windows`は16,390,322 bytes
