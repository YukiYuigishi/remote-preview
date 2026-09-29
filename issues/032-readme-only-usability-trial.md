# 032: README-only usability trial

## Goal

README.mdだけを説明として受け取ったAIが、ykviewでSSH先を閲覧できるか確認する。

## Scope

- 匿名化したSSH aliasを対象とする。
- 2つの独立した一時directoryへ、現時点のREADME.mdと同じsourceからbuildした`ykview` binaryだけを配置する。
- エージェントにはrepositoryのsource、他の文書、Webを読ませず、書き込み機能も使わせない。
- READMEや実装の変更は含めない。

## Relevant files

- `README.md`
- `cmd/ykview`

## Acceptance criteria

- READMEの説明をもとに起動してURLを特定できる。
- SSH先のdirectoryを辿り、file previewを開ける。
- file downloadを確認できる。
- 結果とつまずきを記録する。

## Verification

2026-09-29に独立して実施。指定候補のGPT-5.5 / GPT-5.6 Lunaは利用可能なモデルにないため、GPT-5.6 Sol（xhigh）とGPT-6 Luna（xhigh）を使用した。どちらもREADME.mdだけを説明資料として使用した。事前build済みbinaryを渡したため、READMEのbuild / install手順は評価対象外。以下のhost名とpathは一般化している。

| Model | 起動 | Directory / preview | Download | 結果 |
| --- | --- | --- | --- | --- |
| GPT-5.6 Sol | `./ykview -open=false -addr 127.0.0.1:0 example-host` | ブラウザでremoteのMarkdown fileを表示 | 画面のDownload linkからHTTP 200、保存内容とpreviewの一致を確認 | 成功 |
| GPT-6 Luna | `DEBUG=1 TMPDIR=<isolated-dir> ./ykview -open=false -addr 127.0.0.1:0 example-host` | HTTPでremoteのtext fileを表示 | download endpointでHTTP 200、attachmentを確認 | 成功 |

両方とも起動時に表示されたURLを使用し、終了後はserver停止を確認した。`-write`は使用せず、remote fileは変更していない。

## Current state

完了。READMEだけで既存binaryを使った基本的なremote閲覧とdownloadはできた。GPT-5.6 Solは、READMEにsource build用の`./bin/ykview`とrelease例の`./ykview-darwin-arm64`が混在し、単体binaryの名前を少し判断しにくいと報告した。またfile preview時のbrowser titleにfile名が出ないと報告した。いずれも今回の操作を妨げていない。
