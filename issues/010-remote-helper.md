# Issue 010: remote-side Go filesystem helper

## Goal

directory batch listingのfilesystem探索をremote-side Go helperへ移し、既存のsystem `ssh`、cache、fallback構造を維持したままshell依存を減らす。

## Scope

- `cmd/remote-preview-helper`に標準ライブラリだけの短命CLIを追加する。
- helperへcurrent directoryと最大16個の直下directoryのlistingを実行させる。
- 既存のNUL区切りbatch protocolをhelperから出力し、既存parser/cacheへ渡す。
- `sshRemoteFS`がhelper binaryをremote temporary directoryへ配置し、SSH経由で起動する。
- helper利用失敗時は既存shell batch、さらにsingle-directory listingへfallbackする。
- helperのplatform選択、protocol version、cache lifecycleをtransport層に閉じ込める。
- background prefetchは復活させない。

## Relevant files

- `cmd/remote-preview-helper/main.go`
- `internal/preview/remote.go`
- `internal/preview/remote_batch_test.go`
- `internal/preview/remote_test.go`
- `internal/preview/cache.go`
- `internal/preview/main.go`
- `README.md`
- `PLAN.md`

## Acceptance criteria

- helper単体でroot kind、current listing、bounded child listingを既存protocolで出力できる。
- space、tab、newlineを含むfilenameを壊さない。
- helper pathではfilesystem traversalにshell scriptを使わない。
- helperのplatform検出、upload、起動、cleanupを`sshRemoteFS`から利用できる。
- helper failure、unsupported platform、protocol errorで既存shell batchへfallbackする。
- system `ssh`、ProxyJump、ssh config、ControlMasterの利用を維持する。
- `RemoteFS`、cache、foreground batch、cached entry kind、background prefetchなしの方針を維持する。
- debug logでhelper、shell fallback、single listingの選択とdurationを追跡できる。
- README、PLAN、issueへ設計判断と制約を記録する。
- `go test ./...`、`go test -race ./...`、`go vet ./...`、`go build ./...`が通る。

## Verification

- helper unit test
- helper protocol / filename fidelity test
- local helper integration test
- helper failureからshell fallbackへのtest
- platform selection test
- persistent cache hit / invalidate test
- 上記4つのGo regression command

## Current state / blocker

- `internal/remotehelper`と`cmd/remote-preview-helper`を追加し、既存NUL protocolをhelperから出力するようにした。
- `sshRemoteFS`は初回batch時にremote platformとversion/hash付きhelper cacheをprobeし、cache miss時だけ対応するcompressed helper artifactをremote `TMPDIR`へatomic uploadして実行する。
- helperの実行失敗、platform未対応、upload失敗、protocol errorは既存shell batchへfallbackし、さらに既存single-directory listingへfallbackできる。
- Linux amd64/arm64、darwin amd64/arm64のhelper artifactをembedし、`go generate ./internal/preview`で再生成できる。
- `go test ./...`、`go test -race ./...`、`go vet ./...`、`go build ./...`を通過した。
- 実SSH smoke testでplatform判定、helper upload、helper batch、終了時cleanupまで確認した。persistent cache化後の実remote cache hitはIssue 015のverification対象とする。通常終了時のcleanupはpersistent cache化に伴い行わない。
- helper初回setupはplatform判定とcache miss時のbinary uploadを伴うため、高RTT/ProxyJump環境ではcache miss時の初回表示がshell batchより遅くなり得る。同じhelper binaryがremoteに残っていれば、別のremote-preview processからも再利用する。
- Windows remote helperは未対応で、現在はplatform unsupportedとしてshell fallbackを試みる。
