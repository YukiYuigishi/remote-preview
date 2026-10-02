# Issue 036: Merge origin/master

## Goal

ローカル`master`へ`origin/master`の更新を取り込み、双方の既存変更を保持した統合済みの状態にする。

## Scope

- `origin/master`をfetchし、ローカル`master`へmergeする。
- 競合が発生した場合は、双方の変更意図を確認して解消する。
- merge後のproject stateと検証結果をこのissueへ記録する。

## Relevant files

- `PLAN.md`
- mergeで競合または変更されるfiles
- `issues/036-merge-origin-master.md`

## Acceptance criteria

- `origin/master`がローカル`master`のancestorになっている。
- merge後に未解決の競合がない。
- ローカル側とorigin側の変更が意図どおり保持されている。
- repositoryの標準検証が成功する。

## Verification

- `git merge-base --is-ancestor origin/master master`
- `make check`
- `git status --short --branch`

## Current state

- `master`は`origin/master`に対して40 commits ahead、47 commits behind。
- 両系列には同等の実装commitが異なるhashで含まれ、origin側には追加のMarkdown修正とWindows対応がある。
- merge作業は未実施。
