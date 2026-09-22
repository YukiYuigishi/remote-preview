# Issue 026: ignore local generated files

## Goal

ローカル開発で生成されるbinary、release archive、coverage・editor・Codex設定をGit statusから除外し、意図的にversion管理するembedded remote helper artifactは追跡し続ける。

## Scope

- `/bin/`、`/dist/`、root build binaryをignoreする。
- Go test/profile/coverageの生成物をignoreする。
- `.codex/`、OS・editorのローカルファイルをignoreする。
- `internal/preview/remote_helpers/*.gz`は`go:embed`の入力のためignoreしない。

## Relevant files

- `.gitignore`
- `PLAN.md`
- `internal/preview/remote_helpers/`

## Acceptance criteria

- `internal/preview/remote_helpers/*.gz`の4ファイルがGit管理下にある。
- `bin/ykview`、`dist/`配下、`.codex/`が未追跡として表示されない。
- Goのtest/profile/coverage成果物と一般的なOS・editor一時ファイルがignoreされる。
- 既存のbuild・testに必要なtracked fileをignore対象にしない。

## Verification

- `git ls-files internal/preview/remote_helpers`
- `git check-ignore -v bin/ykview .codex/config.toml dist/example.tar.gz coverage.out`
- `go test ./...`
- `go build ./...`

## Current state / blocker

- 完了。remote helper archiveはtrackedのまま、ローカル生成物だけをignoreする。
