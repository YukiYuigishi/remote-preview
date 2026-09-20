# AGENTS.md

## Roles

* Primary agentは **GPT-5.6 Luna Extra High** を使用する。
* Subagentは **GPT-5.6 Luna Extra High** を使用する。
* Primary agentは、計画、issue作成、subagentへの委譲、review、integrationを担当する。
* Subagentは、割り当てられたissueのscope内だけを担当する。

## Source of Truth

作業開始時に`PLAN.md`を確認する。

* `PLAN.md`: project全体の現在地、優先順位、依存関係
* `issues/`: 個別作業のscope、acceptance criteria、進捗、検証結果
* Git history / diff: 実際に行われた変更

会話履歴をproject stateのsource of truthとして扱わない。

重要な状態、判断、残作業はrepository内へ記録する。

## Issue-driven Work

実装作業は原則として`issues/`へ起票してから行う。

Issueには、作業に必要な情報だけを記載する。

最低限、次を含める。

* Goal
* Scope
* Relevant files
* Acceptance criteria
* Verification
* Current state / blocker（必要な場合）

完了した経緯や詳細を`PLAN.md`へ蓄積しない。個別作業の詳細はissueへ置き、`PLAN.md`は短く保つ。
commitは適切な粒度で行うこと。commitメッセージにはconventional commitを採用する。

## Subagent Context

Subagentへ渡すcontextは、作業を完了するために必要な最小限とする。

原則として次だけを渡す。

1. 対象issue
2. 必要な`PLAN.md`のsection
3. relevant files / directories
4. 必要な追加制約

repository全体、無関係なissue、過去の長い会話履歴を渡さない。

Subagentは、割り当てられたscope外を不必要に探索しない。

無関係なdirectory、Git history、documentを読む必要はない。追加contextが必要になった場合だけ探索範囲を広げる。

## Delegation

Primary agentはsubagentへ委譲するとき、少なくとも次を明示する。

* Goal
* Scope
* Files / directories
* Acceptance criteria
* Verification

独立して実行できる作業だけを並列化する。

同じfileや強く依存する変更を複数subagentへ同時に割り当てない。

小さな作業を、単にsubagentを使うためだけに分割しない。

## Subagent Output

Subagentの完了報告は簡潔にする。

原則として次だけを報告する。

* Changed files
* Verification results
* Commits（commitした場合）
* Unresolved issues / blockers

長い作業経緯、詳細な思考過程、既知情報の再説明は不要。

必要な判断や状態はissueへ記録する。

## Context Management

Agentのcontextをproject stateの保存場所として使わない。

長時間の作業では、必要に応じてissueへ次を更新する。

* Current state
* Completed work
* Next step
* Blockers

新しいagentやcontextから作業を再開するときは、原則として次だけから状態を復元する。

1. `PLAN.md`
2. active issue
3. `git status`
4. relevant diff / recent commits

完了済みissueや無関係なhistoryを、必要がない限り再読しない。

## General Rules

* Userによる既存の未commit変更を無断で変更、破棄、commitしない。
* Agentによる変更は人間が読みやすい適切な粒度でcommitする。
* Scope外の変更を混ぜない。
* 実装後はacceptance criteriaに対応する検証を行う。
* 検証できなかった項目やblockerは隠さずissueまたは完了報告へ記録する。
* 将来必要になるかもしれないという理由だけで、不要な抽象化や追加作業を行わない。

