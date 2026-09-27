# Issue 031: HTML linked resources and file targets

## Goal

`dev1:~/workspace/read-linux-kernel/docs/kvm-svm/index.html`などのHTMLを開いた際に、別ファイルのJS/CSSとその依存resourceをブラウザで利用できるようにする。

## Scope

- HTMLからscript、stylesheet、module、fetch等として要求されたfileを、viewer HTMLではなく元のbytesと適切なContent-Typeでstream配信する。
- ブラウザのrequest destinationを使ってresource取得とdocument navigationを区別する。JS/CSSを一覧から開いた場合は既存のsource viewerを維持する。
- CLIのfile targetではparent directoryを配信rootとし、対象fileのescaped URLを初期表示する。directory targetの初期URLは維持する。
- 既存のURL path normalization、root boundary、raw query、GET/HEAD/Range、local/remote共通backendを維持する。
- HTMLの書き換え、外部HTTP proxy、root外参照、自動index.html表示は追加しない。
- README.mdとdocs/には開始前からuserの未commit変更があるため、変更・stage・commitしない。

## Relevant files

- `internal/preview/handler.go`
- `internal/preview/handler_file_test.go`（または専用test file）
- `internal/preview/main.go`
- `internal/preview/main_test.go`
- `internal/preview/target.go`
- `internal/preview/local.go`、`remote.go`、`cache.go`（interface参照のみ）
- `PLAN.md`（primary agent担当）

## Acceptance criteria

- 相対URLでJS/CSSを参照するHTMLが、local/remote共通handlerで元のresource bytesを取得できる。
- module import、CSS import、fetchによるJSON等もsource viewerへ変換されない。
- document navigation（直接アクセス、一覧からのclick、iframe/frame）では既存preview behaviorを維持する。
- explicit raw query、HEAD（contentをread/openしない）、Rangeはresource取得でも動作する。
- local/remoteのfile targetはparent rootと対象file URLへ解決される。remote home-relative pathと特殊文字のfilenameを扱う。
- directory target、target解決error、missing targetの挙動が明確である。
- linked resourceがtarget root外へ解決されない。
- `go test ./...`、`go test -race ./...`、`go vet ./...`、`go build ./...`が通る。

## Verification

- request destinationを付けたhandler regression tests（GET/HEAD/Range、navigationとの比較）。
- local fixtureのHTMLと相対resourceによるHTTP integration test。
- file/directory target normalizationとinitial URLのunit tests。
- project regression checks。
- 可能ならbrowserでCSS適用・JS実行・module/fetchを確認する。

## Current state / blocker

- 調査済み。serveFileは`.js`/`.css`をrequest用途に関係なくsource viewerへ変換している。
- CLIはfile path自体をhandler rootとして`/`を開くため、相対resourceが`file/path`へ解決される。
- 実装予定。README.mdとdocs/の既存変更は作業対象外。
