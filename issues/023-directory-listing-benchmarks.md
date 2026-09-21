# Issue 023: benchmark and select directory listing strategy

## Goal

複数のdirectory listing方式を再現可能なworkloadで比較し、初回表示とdirectory移動の待ち時間が最も良い方式を採用する。

## Scope

- single-directory on-demand、portable shell batch、Go helper batch、旧parallel prefetch相当を比較する。
- connection/session setup latency、directory規模、連続したchild navigationを変えたscenarioを測る。
- 初回helper probe/uploadは定常時と分けて評価する。
- benchmark結果と測定環境・制約をrepositoryへ記録する。
- 結果に基づき、既存のcache、fallback、OpenSSH互換性を壊さない範囲で最良の方式を採用する。

## Relevant files

- `internal/preview/remote.go`
- `internal/preview/cache.go`
- `internal/preview/*_test.go`
- `internal/remotehelper/`
- `README.md`
- `PLAN.md`

## Acceptance criteria

- 少なくとも3方式を、同じfixtureとnavigation workloadで比較できる。
- SSH invocation相当のlatencyを含むscenarioとremote traversal単体の結果を分けて示す。
- 初回表示、1回のchild移動、連続navigationについて結果を示す。
- 採用方式と理由が測定結果から説明できる。
- 採用方式に必要な実装・回帰test・documentationが完了する。

## Verification

- benchmarkを複数回実行し、中央値または`benchstat`相当で比較する。
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go build ./...`

## Current state / blocker

- 調査開始。実SSH hostが利用できない場合は、latency注入可能なfake transportとlocal filesystem fixtureで比較し、実network未計測であることを結果へ明記する。
