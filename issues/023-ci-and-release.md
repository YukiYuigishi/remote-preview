# Issue 023: add CI and GitHub Release workflows

## Goal

GitHub Actionsでpush/PR時の検証を自動化し、`v*` tag push時に配布用CLI artifactをGitHub Releaseへ公開する。

## Scope

- `.github/workflows/ci.yml`を追加する。
- CIで`make check`を実行し、生成artifactの差分も検査する。
- `.github/workflows/release.yml`を追加する。
- `v*` tag pushでLinux/Darwinのamd64/arm64向けCLIをcross buildする。
- README、LICENSE、THIRD_PARTY_NOTICESを含むtar.gzとSHA256SUMSを作成する。
- GitHub Actionsの`GITHUB_TOKEN`でGitHub Releaseを作成し、artifactをuploadする。
- workflowのpermissionsをjob用途に限定する。

## Relevant files

- `.github/workflows/ci.yml`
- `.github/workflows/release.yml`
- `Makefile`
- `README.md`
- `issues/023-ci-and-release.md`

## Acceptance criteria

- pull requestとmain branchへのpushでCI workflowが実行される。
- CIがGo test、race test、vet、build、remote helper generationを検証する。
- `v*` tag pushでLinux/Darwin amd64/arm64の4 artifactを作成する。
- 各archiveに`ykview`、README、LICENSE、THIRD_PARTY_NOTICES.mdが含まれる。
- `SHA256SUMS`をReleaseへ添付する。
- Release workflowが既存tagの検証とGitHub Release作成を行う。
- CIはcontents read、Releaseはcontents writeに限定する。
- workflow YAMLのsyntax、shell script、`git diff --check`をローカルで検証できる。

## Verification

- `make check`
- CI/Release workflowのYAML parse
- release packaging scriptのlocal dry run
- `git diff --check`

## Current state / blocker

- 実装完了。
- `make check`を実行するCI workflowと、`v*` tagから4 platform archiveを作成してGitHub Releaseへuploadするworkflowを追加した。
- release packagingを`scripts/package-release.sh`へ切り出し、localでも同じarchiveを作成できるようにした。
- Release workflowはpackage前に`make check`を実行し、手動実行時も指定tagのcommitをcheckoutする。

## Changed files

- `.github/workflows/ci.yml`
- `.github/workflows/release.yml`
- `scripts/package-release.sh`
- `README.md`
- `PLAN.md`
- `issues/023-ci-and-release.md`

## Verification results

- `make check`: passed
- workflow YAML parse: passed
- release packaging dry run: passed
- `git diff --check`: passed
