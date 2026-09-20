# Issue 016: compress remote helper upload over SSH

## Goal

helper cache miss時のbinary転送量を減らし、高RTT/低帯域のremoteへの初回setupを短縮する。

## Scope

- helper uploadでsystem `ssh`のcompressionを有効化する。
- remote側にはgzip commandを要求せず、展開済みbinaryを従来どおりcacheへ保存する。
- 圧縮uploadが利用できない場合は非圧縮uploadへfallbackする。
- SSH commandのdebug logでcompression利用を判別できるようにする。

## Relevant files

- `internal/preview/remote.go`
- `internal/preview/remote_test.go`
- `issues/015-persistent-remote-helper-cache.md`
- `README.md`

## Acceptance criteria

- helper upload時のSSH commandに`-C`を付けてchannel compressionを有効化する。
- directory listing、read、probeなどhelper upload以外のSSH commandには不要なcompressionを付けない。
- remote側に`gzip`や追加のdecompressorを要求しない。
- compression commandが失敗した場合、既存のraw uploadへfallbackできる。
- cache hit時はcompression有無に関係なくupload自体を行わない。
- debug logからcompressed uploadとraw fallbackを追跡できる。
- `go test ./...`、`go test -race ./...`、`go vet ./...`、`go build ./...`が通る。

## Design decisions

- gzip payloadをremote shellで展開する方式ではなく、SSH transport compressionを採用する。macOS、BusyBox、gzip非搭載POSIX remoteとの互換性を維持するため。
- compressionはhelper uploadだけに限定する。SSH commandの小さいtext outputまで圧縮して複雑化しない。
- helper cache filename/hashとremoteに保存するbinary形式はIssue 015の設計を維持する。

## Verification

- SSH argumentの`-C`付与テスト
- raw commandに`-C`を付けないテスト
- compressed upload失敗からraw uploadへのfallback test
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go build ./...`

## Current state / blocker

- 完了。
- helper uploadのSSH commandだけ`-C`を有効化し、失敗時は非圧縮uploadへretryするようにした。
- remote側の保存形式とcache keyはIssue 015のまま維持した。

## Changed files

- `internal/preview/remote.go`
- `internal/preview/remote_test.go`
- `README.md`
- `issues/016-compress-helper-upload.md`

## Verification

- compressed/raw SSH argument test
- compressed upload failureからraw uploadへのfallback test
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go build ./...`

## Remaining limitations

- `ssh -C`はtransport compressionなので、RTT数は減らさない。cache miss時の転送bytes削減が目的。
