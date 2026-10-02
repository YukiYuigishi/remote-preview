# Windows client support

## Goal

Windows上でykviewを起動し、ローカルファイルとPOSIX SSHホストをブラウザから閲覧・転送できるようにする。

## Scope

- Windowsのdrive/UNC path、backslash区切りの相対pathをlocal targetとして扱う。
- HTTP pathをWindowsのlocal filesystem pathへ正しく結合する。
- Windows上では利用できないSSH ControlMaster設定を適用せず、通常のsystem sshを使用する。
- Windows binaryをreleaseに含め、Windowsでの起動方法を記載する。

## Relevant files

- `internal/preview/target.go`, `handler.go`, `remote.go` と関連tests
- `.github/workflows/`, `scripts/package-release.sh`, `README.md`

## Acceptance criteria

- Windows clientでdrive/UNC/local relative targetを解決でき、file preview・download・uploadがroot配下の実ファイルへ届く。
- Windows clientからPOSIX SSH targetへ通常のsystem ssh経由で接続できる。
- Windows amd64/arm64 release zipを生成する。
- 既存のLinux/macOS挙動とtestsを維持する。

## Verification

- `go test ./...`, `go vet ./...`, `go build ./...`
- Windows向けcross buildとWindows CI上のtarget/local handler tests
- release packagingのarchive内容を確認する。

## Current state

実装済み。Windows native testsはGitHub ActionsのWindows jobで実行する。手元にはWindows runtimeがないため、native実行結果は未確認。

## Verification results

- `go test ./...`, `go test -race ./...`, `go vet ./...`, `go build ./...`: 成功
- Windows amd64/arm64 cross build、Windows test binaryのcompile: 成功
- release packagingを実行し、両Windows ZIPに`ykview.exe`と文書が含まれること、`SHA256SUMS`に両ZIPが含まれることを確認
