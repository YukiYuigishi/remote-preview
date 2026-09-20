# Issue 024: rename project and CLI to ykview

## Goal

local/remote filesystem viewerとしての実態に合わせ、project、CLI、module、release artifactの名前を`ykview`へ統一する。

## Scope

- Go module pathを`ykview`へ変更する。
- CLI entrypointを`cmd/ykview`、remote helper entrypointを`cmd/ykview-helper`へ変更する。
- binary、Makefile target、install command、release archiveを`ykview`へ変更する。
- README、PLAN、workflow、issue/documentationの現行参照を更新する。
- browser asset endpointを`/_ykview/assets/v1/`へ変更する。
- remote helper cache/upload pathは既存利用者のcache互換性のため`remote-preview` prefixを維持する。
- GitHub remote URL・repository slugは外部状態のため変更しない。

## Relevant files

- `go.mod`
- `cmd/remote-preview`
- `cmd/remote-preview-helper`
- `internal/preview`
- `Makefile`
- `scripts/*`
- `.github/workflows/*`
- `README.md`
- `PLAN.md`
- `THIRD_PARTY_NOTICES.md`
- `issues/024-rename-to-ykview.md`

## Acceptance criteria

- `go test ./...`、`go test -race ./...`、`go vet ./...`、`go build ./...`が通る。
- `make build`が`bin/ykview`を生成する。
- `make build-helper`が`bin/ykview-helper`を生成する。
- `make install`が`ykview`をinstallする。
- `scripts/package-release.sh`が`ykview-<version>-<platform>.tar.gz`と`ykview` binaryを生成する。
- CLI起動、local/remote target、browser asset配信、remote helper生成が動作する。
- READMEとworkflowの実行例・artifact名が`ykview`になっている。
- 既存remote helper cache pathは互換性のため変更しない。
- GitHub remote URLは変更しない。

## Verification

- stale reference search
- `make check`
- `make build-helper`
- `make install`
- release package dry run
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go build ./...`

## Current state / blocker

- 実装完了。
- Go module、CLI、helper entrypoint、Makefile、release archive、README、workflow、browser asset endpointを`ykview`へ統一した。
- 既存remote helper cacheの`.remote-preview-helper-*` prefixとGitHub remote URLは互換性・外部状態のため維持した。

## Changed files

- `go.mod`
- `cmd/ykview`
- `cmd/ykview-helper`
- `internal/preview`
- `internal/remotehelper`
- `Makefile`
- `scripts/generate-remote-helpers.sh`
- `scripts/package-release.sh`
- `.github/workflows/*`
- `README.md`
- `PLAN.md`
- `THIRD_PARTY_NOTICES.md`
- `issues/001-023`
- `issues/024-rename-to-ykview.md`

## Verification results

- `go test ./...`: passed
- `go test -race ./...`: passed
- `go vet ./...`: passed
- `go build ./...`: passed
- `make check`: passed
- `make build-helper`: passed
- `make install`: passed
- release package dry run: passed
- stale active reference search: passed except intentional `.remote-preview-helper-*` compatibility prefix
- GitHub remote URL unchanged
