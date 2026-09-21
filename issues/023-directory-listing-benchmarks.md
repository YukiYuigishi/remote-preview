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

- 完了。専用SSH benchmark host上に一時fixtureを作り、各方式をwarmup後5回測定した。一時fixtureとlocal artifactは測定後に削除した。
- fresh SSH invocationのp50は、small fixtureでsingle current 316.748ms、shell batch 368.227ms、Go helper batch 311.950msだった。
- medium fixture（16 child × 100 files）ではsingle 354.695ms、shell batch 2,065.933ms、Go helper batch 311.288msだった。
- wide fixture（64 child × 20 files、batchは先頭16 child）ではsingle 430.115ms、shell batch 841.238ms、Go helper batch 304.580msだった。helperとshellは同じentry数・protocol bytesを処理した。
- SSH command単体をwarmup後12回測定すると、独立接続はp50 309.8ms、ControlMaster再利用後のsession channelはp50 9.8msだった。連続2 commandは601.5msから313.8ms、連続3 commandは913.3msから323.0msへ短縮した。
- latency注入benchmarkを追加し、single on-demandはfirst/one child/sequentialで1/2/最大17 SSH calls、foreground batchはすべて1 call、旧prefetchは最大17 callsになることを固定した。旧prefetchの同時実行数は当時と同じ4に制限した。
- 採用方式は、Go helperによるforeground batch、TTL cache、singleflightを維持し、process-localなOpenSSH ControlMasterを追加してcommand間でtransportを再利用する方式とした。旧parallel prefetchは採用しない。helper unavailable時のshell/single fallbackは維持する。
- SSH benchmark hostのHTTP smoke testでは、helper cache hit時の初回表示が719.9msから120.7msへ短縮した。helper probeは310.8msから14.8ms、helper listingは314.1msから13.6msになった。直下のcached child表示は0.2msだった。
- `go test ./...`、`go vet ./...`、`go build ./...`は成功した。`go test -race ./...`はこの環境に`gcc`がなく実行できなかった。
