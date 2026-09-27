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

- 実装済み。`Sec-Fetch-Dest`が`document`/`frame`/`iframe`以外のfile requestを既存のraw streamingへ渡し、空fileを含むscript、style、module、fetch resourceを元bytesで返す。destination headerなしのsource viewerとdocument navigationは維持する。
- `.js`/`.mjs`/`.cjs`、CSS、JSONのContent-Typeを明示した。Range streamingとHEAD metadataは既存のraw response pathを利用する。
- 起動時にlocal/remote targetのkindを解決し、directoryは`/`、fileはparentをrootにしてescaped filenameを初期URLにする。remote home-relative file targetにも対応し、missing targetとKind errorを起動時に返す。
- Focused verification: `go test ./internal/preview -run 'TestHandler(UsesFetchDestination|ServesKnownTextFile)|TestLocalHTMLLinkedResources|TestResolvePreviewTarget'` passed. Tests cover local HTTP HTML-linked classic/module scripts, module imports, CSS imports, fetch JSON, empty files, resource HEAD/Range, source navigation, directory/file targets, special filenames, remote home-relative paths, and target errors.
- Primary review/integration完了。README.mdとdocs/の既存変更は変更・stage・commitしていない。

## Verification results

- Focused testsと`git diff --check`: passed。
- `make check`: passed（`go test ./...`、`go test -race ./...`、`go vet ./...`、CLI binary build）。
- `go build ./...`: passed。
- local CLIと実HTTP serverでも確認した。修正前はscript/style/empty destinationのJS/CSS/JSONがviewer HTMLになっていた。修正後はclassic JS、CSSとimport先、moduleとimport先、JSONを元bytesと適切なContent-Typeで返す。
- `index #日本語.html`をfile targetとして起動し、parent rootとescaped初期URLを確認した。document/frame/iframe/headerなしのsource navigation、resource HEADの空bodyとContent-Length、Rangeの206と元bytesも確認した。
- root boundaryは既存の`cleanRelativeURLPath`とhandlerのroot joinを維持している。parent root外への相対参照は配信対象にしない。Fetch Metadata headerがないclientは既存preview behaviorを維持し、必要なら`?raw=1`を指定する。
- 実ページの検証制限: `ssh -o BatchMode=yes -o ConnectTimeout=8 dev1`はconnection timeoutとなり、対象HTMLを読めなかった。
- Browser検証制限: browser skillの接続とdocumentation取得は成功したが、actionが`Unable to load browser request-header policy`で失敗し、CSS適用とJS実行の目視確認はできなかった。HTTPのresource配信は検証済み。
