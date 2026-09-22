# Issue 027: avoid modifying embedded helpers during normal builds

## Goal

通常のbuild・検証で、Go toolchainや実行環境の差によってtrackedなremote helper artifactが変更されないようにする。

## Scope

- `make build`と`make check`から自動的な`go generate`を外す。
- `make generate`をembedded helper artifactの明示的な更新操作として残す。
- README、PLANへ生成物のversion管理と更新手順を記録する。

## Relevant files

- `Makefile`
- `README.md`
- `PLAN.md`
- `internal/preview/remote_helpers/*.gz`

## Acceptance criteria

- clean checkoutで`make build`を実行しても`internal/preview/remote_helpers/*.gz`がmodifiedにならない。
- `make check`が既存のtracked helper artifactを使ってtest、race、vet、buildを実行する。
- helper artifactを更新する方法は`make generate`として明示されている。
- `go build ./...`と`go test ./...`の既存動作を維持する。

## Verification

- `make build`
- `make check`
- `go test ./...`
- `go build ./...`
- build後の`git status --short`

## Current state / blocker

- 完了。通常のbuild/checkはhelper artifactを再生成せず、`make generate`だけがtracked artifactを更新する。
