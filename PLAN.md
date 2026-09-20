# remote-preview implementation plan

## Current state

- MVPのread-only SSHファイルプレビューは動作している。
- Phase 1–4（責務分割、target改善、context-aware system SSH、portable batch/on-demand directory listing cache）を実装済み。
- `go test ./...`、`go test -race ./...`、`go vet ./...`、`go build ./...` は成功している。
- 次の主要課題は、ファイル全量読み込みとpreview fallback/HTTP品質（Phase 5）。
- `cmd/remote-preview`は薄いentrypointで、アプリケーション実装は`internal/preview`に配置されている。
- directory listingはforegroundで取得し、必要に応じてportable batch commandで直下分も同じSSHにまとめ、TTL cacheとsingleflightで再利用する。
- debug logは標準ライブラリの`log/slog`でHTTP、cache、batch、SSHの処理境界を追跡できる。
- remote-side Go helperはIssue 010/015で実装済み。remote `TMPDIR`のversion/hash付きcacheを再利用し、既存shell batchをfallbackとして維持する。
- helper cache miss時のbinary uploadはSSH compressionを使い、失敗時はraw uploadへfallbackする。
- text-like fileはbrowser内viewerへ送り、対応source codeはbrowser-side syntax highlightを利用する。CDN unavailable時はplain textへfallbackする。
- MakefileはIssue 011でbuild、helper生成、test、race、vet、check、cleanを再現する。

## Product decisions

- 対象ユーザーは、対象ホストへSSH接続できる本人。read-only用途を維持する。
- `host:/absolute/path` は継続サポートする。
- `host` だけを指定した場合は、リモートのホームディレクトリを対象にする。
- ブラウザは起動時にデフォルトで開く。自動起動を無効にする場合は`-open=false`を指定する。local listen address (`-addr`) と remote target は別概念として扱う。
- `~` や相対パスをローカル側で推測せず、リモート側でホームを解決する。
- シンボリックリンクによるroot外参照は、個人向けread-onlyツールとしては最優先の阻害要因にしない。ただし挙動を明文化し、将来strict root confinementを追加できる構造にする。
- system `ssh` は `~/.ssh/config`、Host alias、ProxyJump、ssh-agent、ControlMasterを利用できる強みがあるため、Go SSHへ即時置換しない。

## Implementation phases

### Phase 1: 構造整理とテスト可能な境界

同じ `package main` のまま、まずファイルを責務ごとに分割する。

- `main.go`: flag、server起動、signal/shutdown
- `target.go`: target parser、home targetの表現、URL/path helper
- `remote.go`: remote filesystem interfaceとtransport実装
- `cache.go`: directory listing cache、TTL、singleflight
- `handler.go`: HTTP routingとresponse生成
- `templates.go`: HTML/CSS/Markdown template

`handler` が直接 `exec.Command` を呼ばないよう、最低限次の境界を作る。

- `RemoteFS.Kind(ctx, path)`
- `RemoteFS.List(ctx, path)`
- `RemoteFS.Read(ctx, path)`
- 必要なら `RemoteFS.Home(ctx)`

Acceptance criteria:

- 既存のCLIと表示挙動を維持する。
- handlerをfake `RemoteFS` でテストできる。
- `go test ./...`、`go vet ./...`、`go build ./...` が通る。

### Phase 2: target CLIとURLの改善

- `host:/path` を既存どおり受け付ける。
- `host` を `host:~` 相当のtargetとして受け付ける。
- リモートの `$HOME` をSSH経由で一度だけ解決し、以降は絶対パスとして扱う。
- ファイル名・ディレクトリ名をURLの各path segmentとして一貫してescapeする。
- breadcrumb、Parent、directory entryのリンク生成を共通化する。
- `-addr :0` の実際のlisten addressを取得して表示・`-open`に使う。

Acceptance criteria:

- `./remote-preview remote-host:/path/to/dir` が動く。
- `./remote-preview remote-host` でリモートhomeを開ける。
- `?`、`#`、空白、日本語、`%`を含む名前を一覧から辿れる。
- pathの`..`でtarget rootを越えない。

### Phase 3: SSH transportの整理と制御

最初の実装ではsystem `ssh` を残し、`exec.CommandContext` とcontext deadlineを導入する。これにより、既存のOpenSSH設定互換性を維持しながら、キャンセル・タイムアウト・テスト差し替えを可能にする。

Go SSHへの移行は別途prototypeとして評価する。`golang.org/x/crypto/ssh`へ単純移行すると、OpenSSHのconfig、ProxyJump、agent、ControlMasterの挙動を失うため、次を確認してから決める。

- `~/.ssh/config` のaliasとuser/port/identity設定
- ssh-agentとknown_hosts検証
- ProxyJump相当
- 接続再利用
- リモートのSFTP/ファイル名処理

Acceptance criteria:

- SSH接続、remote command、file readがcontextキャンセルで終了する。
- 接続・コマンドに明示的なtimeoutがある。
- system `ssh` backendをfake backendに差し替えてテストできる。
- Go SSH prototypeが既存機能を満たせない場合は、system `ssh` 継続を正式判断として記録する。

### Phase 4: directory listing cache

対象はまずdirectory listingだけとし、ファイル内容のキャッシュは行わない。

- cache keyはremote hostと正規化済みremote path。
- TTLと最大エントリ数を設ける。
- 同一pathへの同時アクセスはsingleflight相当で重複SSHを抑える。
- 初回表示では、現在のdirectoryの一覧だけを取得する。
- background prefetchは行わず、cache miss時のforeground batch commandにcurrentと直下directoryをまとめる。
- read-only前提でも、TTL満了後は再取得できるようにする。
- cached listingのentry kindを使い、一覧から辿ったdirectory/fileでは不要な`remoteKind`呼び出しを減らす。

Acceptance criteria:

- 同じdirectoryの再表示でSSH listingが発生しない。
- directory移動時に不要なbackground SSHが発生しない。
- batch対応環境では、currentと直下directoryのlistingを1回のSSH commandで取得できる。
- TTL切れ、同時アクセス、SSH失敗時の挙動がテストされている。

### Phase 5: preview fallbackとHTTPの品質改善

- Mermaidの動的importまたは描画失敗時は、rendered previewを隠してMarkdown sourceを再表示する。
- Markdown rendererのCDN失敗時も同じfallback経路に統一する。
- HEADで不要なファイル全量読み込みを避けられる設計にする。
- サイズ上限、Range/streaming対応の要否を決める。少なくとも巨大ファイルを無制限に`[]byte`へ読み込まない。
- HTML/SVGの同一origin実行について、read-only個人用途のリスクと運用前提をREADMEに明記する。必要ならsandboxed previewまたはdownload扱いを追加する。

Acceptance criteria:

- CDN、Mermaid、Markdown parseの各失敗でsourceが表示される。
- fallback時の画面文言と実際の表示が一致する。
- 大きなファイルでサーバが無制限にメモリを消費しない。

### Phase 6: root confinement policyの明文化と追加検討

- 現行のsymlink追跡挙動をREADMEに明記する。
- strict modeが必要になった場合に、remote realpathを検証する方式を検討する。
- strict modeでは、解決後のpathがroot配下かを検査し、root外へのsymlinkは表示またはアクセスを拒否する。
- remote側の`realpath`可用性や権限差を考慮し、通常modeとの互換性を壊さない。

## Verification

- Unit: target parser、URL segment、parent/breadcrumb、cache TTL、singleflight、fallback。
- Handler: fake `RemoteFS` によるGET/HEAD、directory/file/Markdown/error response。
- Integration: fake SSH executableまたはtest transportによる引数・context・stderr処理。
- Regression: `go test ./...`、`go test -race ./...`、`go vet ./...`、`go build ./...`。
- Manual: home target、ProxyJump/alias、特殊ファイル名、巨大ファイル、CDN/Mermaid失敗。

## Candidate implementation issues

実装開始時は、次のissueへ分ける。同じfileを同時に変更しない。

1. 責務分割とRemoteFS interface
2. target `host` shorthand / remote home / URL encoding
3. context-aware system SSH backend
4. directory listing cache / portable batch listing
5. Markdown fallbackとHTTP read path
6. Go SSH prototypeとtransport選択
7. root confinement policyとドキュメント
8. remote-side Go filesystem helper
