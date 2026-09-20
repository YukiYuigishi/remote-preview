# Issue 011: Makefile and development commands

## Goal

よく使うbuild、helper生成、test、race、vet、cleanの手順をMakefileへ集約し、clean checkoutから同じ開発コマンドを再現できるようにする。

## Scope

- `make build`でembedded remote helper artifactを生成・更新してCLIを`bin/`へbuildする。
- `make build-helper`でstandalone helperを`bin/`へbuildする。
- `make generate`、`make test`、`make test-race`、`make vet`、`make check`、`make clean`を提供する。
- READMEにMakefileの主要targetと出力先を記載する。

## Relevant files

- `Makefile`
- `README.md`
- `scripts/generate-remote-helpers.sh`
- `issues/011-makefile.md`

## Acceptance criteria

- `make build`がclean checkoutで成功し、`bin/remote-preview`を生成する。
- `make build-helper`が`bin/remote-preview-helper`を生成する。
- `make generate`がhelper artifactを再生成する。
- `make test`、`make test-race`、`make vet`、`make check`が対応するGo commandを実行する。
- `make clean`がMakefileの生成物だけを削除する。
- Makefileがbash固有機能や外部build toolへ依存しない。
- READMEとPLANのbuild手順がMakefileと一致する。

## Verification

- `make generate`
- `make build`
- `make build-helper`
- `make test`
- `make test-race`
- `make vet`
- `make check`
- `make clean`

## Current state / blocker

- `Makefile`を追加し、`build`、`build-helper`、`generate`、`test`、`test-race`、`vet`、`check`、`clean`を提供した。
- CLIとstandalone helperは`bin/`へ出力し、`clean`はその生成物だけを削除する。
- `make build`、`make build-helper`、`make generate`、`make test`、`make test-race`、`make vet`、`make check`、`make clean`を実行して成功した。
- helper artifact生成は`-trimpath`、`-buildvcs=false`、固定gzip metadataを使い、連続実行で同一artifactになることを確認した。
