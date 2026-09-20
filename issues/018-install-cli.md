# Issue 018: install the remote-preview CLI

## Goal

通常のGo toolchainからremote-previewをユーザーの`GOPATH/bin`へinstallし、毎回repository内のbinary pathを指定せずに起動できるようにする。

## Scope

- Makefileに`install` targetを追加する。
- `go install -trimpath ./cmd/remote-preview`を使い、`GOBIN`未指定時はGoの既定の`GOPATH/bin`（通常`~/go/bin`）へ配置する。
- READMEへinstall方法と配置先の確認方法を追加する。
- clean checkoutでinstall targetをdry-run確認する。

## Relevant files

- `Makefile`
- `README.md`
- `issues/018-install-cli.md`

## Acceptance criteria

- `make install`が成功する。
- `GOBIN`未指定時に`go env GOPATH`のbin directoryへ`remote-preview`をinstallする。
- `GOBIN`指定時はGo toolchainの既存挙動に従う。
- helper embedded artifactの開発build構造を壊さない。
- READMEに`make install`と`go env GOPATH`/`GOBIN`の確認方法がある。
- `go test ./...`、`go vet ./...`、`go build ./...`が通る。

## Design decisions

- install先をMakefileで`~/go/bin`へ固定せず、`go install`に委譲する。これにより`GOBIN`、複数`GOPATH`、platform差を尊重する。
- install対象は利用者が実行する`remote-preview` CLIのみとし、remote-side helperはembedded artifactとしてCLIへ含める。

## Verification

- `make -n install`
- `make install`
- `go test ./...`
- `go vet ./...`
- `go build ./...`

## Current state / blocker

- 完了。
- Makefileに`install` targetを追加し、Go toolchainの既定install先へCLIを配置できるようにした。
- READMEへ`make install`、`GOPATH`、`GOBIN`の確認方法を追加した。

## Changed files

- `Makefile`
- `README.md`
- `issues/018-install-cli.md`

## Verification

- `make -n install`
- `make install`
- `go test ./...`
- `go vet ./...`
- `go build ./...`
