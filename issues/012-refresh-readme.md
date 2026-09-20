# Issue 012: refresh README for current implementation

## Goal

READMEを現在のremote helper、Makefile、structured debug log、cache/fallbackの実装と一致させる。

## Scope

- build outputとMakefile targetを最新化する。
- remote-side helperのtemporary lifecycle、platform、fallbackを説明する。
- directory listing、filename fidelity、SSH/ProxyJump、高RTT時の制約を実装に合わせる。
- 実環境固有のhost、user、path、測定値をREADMEへ入れない。

## Relevant files

- `README.md`
- `Makefile`
- `internal/preview/remote.go`
- `internal/preview/remote_helpers.go`
- `issues/012-refresh-readme.md`

## Acceptance criteria

- READMEの起動例が`bin/ykview`とMakefileに一致する。
- helperがremoteへ一時配置され、batch listingを行い、終了時cleanupを試みることが説明されている。
- helper failure / unsupported platform時のshell batch fallbackが説明されている。
- `DEBUG=1`、`-v`、cache、background prefetchなしの方針が現行実装と一致する。
- 実環境固有情報が含まれていない。

## Verification

- READMEとMakefile/sourceの手順を目視照合する。
- `rg`で実環境固有情報がないことを確認する。
- `make clean`

## Current state / blocker

- READMEを現行のMakefile、remote helper、structured debug log、cache/fallback設計に合わせて更新した。
- `bin/ykview`を使う起動例、`make` target、helperのtemporary lifecycle、初回setup時の高RTT制約を記載した。
- 実環境固有のhost、user、path、測定値は含めていない。
- `make clean`で生成binaryを削除した。
