# Issue 015: persist and reuse remote helper binaries

## Goal

remote-previewのプロセスをまたいでremote-side helperを再利用し、高RTT環境で毎回発生しているhelper binary uploadの転送コストを削減する。

## Scope

- remote `TMPDIR`配下に、version・platform・helper binary SHA-256を含むcache filenameでhelperを保存する。
- 起動時にremote platformとcache fileの有無を1回のSSH commandで確認する。
- cache hit時はhelper binaryをuploadせず、そのpathを再利用する。
- cache miss時は一時upload fileへ書き込み、`mv`でcache pathへatomic installする。
- helper実行失敗時はcache fileを削除してshell batchへfallbackする。
- `sshRemoteFS.Close`でpersistent helperを削除しない。
- README、Issue 010、PLANを新しいlifecycleへ更新する。

## Relevant files

- `internal/preview/remote.go`
- `internal/preview/remote_helpers.go`
- `internal/preview/remote_test.go`
- `internal/preview/logging.go`
- `README.md`
- `PLAN.md`
- `issues/010-remote-helper.md`

## Acceptance criteria

- 同じremote host・platform・同一helper binaryで2回目以降の起動時にbinary uploadを行わない。
- cache hitではremote helperを通常どおり起動してbatch listingできる。
- helper cache filenameにcache version、platform、helper binary SHA-256が含まれる。
- cache missのuploadはatomic installされ、途中upload fileを実行しない。
- helper実行失敗時はcacheを無効化し、既存shell batchへfallbackする。
- remote helperは通常のprocess終了時にもremoteへ残り、次回起動で再利用できる。
- remote側で`sha256sum`、`mktemp -d`などの追加コマンドを必須にしない。
- debug logからcache hit、cache miss、upload、invalidateを区別できる。
- background prefetch、system `ssh`、既存cache/fallback構造を変更しない。
- `go test ./...`、`go test -race ./...`、`go vet ./...`、`go build ./...`が通る。

## Design decisions

- cache keyは展開後helper binaryのSHA-256を使う。remote側でhash utilityを実行するのではなく、hashをfilenameへ含めて一致するcache pathだけを再利用する。
- remote環境差を増やさないため、存在確認はPOSIX shellの`-f`と`-x`だけで行う。
- cache hit/missの確認と`uname`によるplatform判定は1回のSSH commandへまとめ、再利用時の追加RTTを最小化する。
- stale cacheの自動GCは今回行わない。helper実行失敗時には該当cacheを削除し、次回起動時に再upload可能にする。

## Verification

- cache filename/hashのunit test
- cache probeのplatform/path parser test
- cache hitでuploadを呼ばないtest
- cache missのatomic upload script test
- helper failureからcache invalidateとshell fallbackへのtest
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go build ./...`

## Current state / blocker

- 完了。
- remote `TMPDIR`へversion/platform/SHA-256付きcache filenameでhelperを保存し、`uname`とcache存在確認を1回のprobe commandへまとめた。
- cache hit時はuploadせず、cache miss時はnonce付き一時ファイルからatomic `mv`でinstallする。
- helper実行失敗時は該当cacheを削除してshell batchへfallbackし、通常終了時はpersistent cacheを削除しない。

## Changed files

- `internal/preview/remote.go`
- `internal/preview/remote_test.go`
- `README.md`
- `PLAN.md`
- `issues/010-remote-helper.md`
- `issues/015-persistent-remote-helper-cache.md`

## Verification results

- cache filename/hash、probe parser、cache hit、atomic install、helper cache invalidationのunit testを追加・通過。
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go build ./...`

## Remaining limitations

- remote側でのbinary再hash検証は行わず、SHA-256を含むcache filenameと`-f`/`-x`確認で整合性を判定する。
- stale cacheの自動GCは未実装。
- 実remoteでのcache hitによる再起動間upload省略は、今回の検証では未実施。既存のSSH smoke testはcache導入前のもの。
