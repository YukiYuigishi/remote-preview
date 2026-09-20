# remote-preview

SSH先のディレクトリを、ローカルブラウザから **read-only Webファイラー** として閲覧する小さなCLIです。

リモート側にHTTPサーバや恒久的なagentを入れず、system `ssh` で必要な処理だけを実行します。directory batch listingでは、対応platform向けのGo helperを一時配置して使います。

## Features

- `host:/absolute/path` を指定してWebファイラーとして閲覧
- `host` だけを指定すると、リモートのhome directoryを閲覧
- system `ssh` を利用
  - `~/.ssh/config`
  - Host alias
  - IdentityFile
  - ProxyJump
  - ssh-agent
  - ControlMaster
  などをそのまま利用
- remote-side Go helper（Linux/darwinのamd64/arm64）でcurrentと直下directoryをbatch listing
- helperが使えない場合はPOSIX `sh`（macOS / BusyBoxを含む）のbatch listingへfallback
- ディレクトリ一覧とbreadcrumb navigation
- HTML: そのままブラウザでpreview
- Markdown: GitHub Flavored Markdown (GFM) preview
  - tables
  - strikethrough
  - task lists
  - autolinks
- ` ```mermaid ` fenced code blockをMermaidとして描画
- PNG/JPEG/WebP/SVGなど: そのまま表示
- source code / JSON / YAML / textなど: text viewer
- `Raw` 表示
- directory listingのTTL cacheと同時アクセスの重複抑制
- macOS / BusyBox互換のforeground batch listing
- SSH command/connect timeoutとrequest context cancellation
- read-only

## Quick start (macOS Apple Silicon)

```bash
make build
./bin/remote-preview -open remote-host:/remote/path
```

ブラウザで:

```text
http://127.0.0.1:8080/
```

## Example

```bash
./remote-preview-darwin-arm64 -open remote-user@remote.example.com:/remote/path
```

普段 `~/.ssh/config` にaliasを書いているなら、そのaliasをそのまま使えます。

```sshconfig
Host remote-host
    HostName remote.example.com
    User remote-user
    ProxyJump bastion

Host bastion
    HostName bastion.example.com
    User jump-user
```

```bash
./remote-preview-darwin-arm64 -open remote-host:/remote/path
```

## Markdown / Mermaid

Markdown viewerはブラウザ側で `marked` のGFM modeを使います。Mermaid blockはMermaidで描画します。

````markdown
# Architecture

| component | role |
| --- | --- |
| browser | viewer |
| SSH | transport |

```mermaid
flowchart LR
    Browser --> LocalServer
    LocalServer --> SSH
    SSH --> RemoteFiles
```
````

Markdown renderer / Mermaidは現在jsDelivrからbrowser側で読み込むため、Markdownのrich previewにはインターネット接続が必要です。CDNを読めない場合でもMarkdown sourceは表示されます。

## Build

Go 1.23+のみ必要です。Go module dependencyはありません。通常のbuildと検証はMakefileから実行できます。

```bash
make build
./bin/remote-preview -open remote-host:/remote/path
```

remote-side helperの埋め込みartifactはLinux/darwinのamd64/arm64向けに同梱しています。artifactを再生成する場合は次を実行します。

```bash
make generate
```

主なdevelopment command:

```bash
make test       # go test ./...
make test-race  # go test -race ./...
make vet        # go vet ./...
make build-helper # standalone remote-preview-helperをbin/へbuild
make check      # generate + test + race + vet + build
make clean      # bin/のMakefile生成物を削除
```

## Project layout

- `cmd/remote-preview`: CLI entrypoint only
- `cmd/remote-preview-helper`: remote-side filesystem helper entrypoint
- `internal/preview`: target parsing、SSH transport、directory cache、HTTP handler、templateとそのtests
- `internal/remotehelper`: helperのfilesystem traversalとbatch protocol writer
- `scripts/generate-remote-helpers.sh`: helper artifactのcross buildと圧縮
- `Makefile`: build、test、verification command
- `issues/`: 実装scopeと検証結果

## Usage

```bash
./bin/remote-preview [options] host:/absolute/path
```

リモートhomeを開く場合はhostだけを指定できます。

```bash
./bin/remote-preview -open remote-host
```

ブラウザを自動で開く:

```bash
./bin/remote-preview -open remote-host:/remote/path
```

listen address変更:

```bash
./bin/remote-preview -addr 127.0.0.1:7391 remote-host:/remote/path
```

空いているportを使う場合は`-addr :0`を指定できます。実際にlistenしたURLがログに表示されます。

request log:

```bash
./bin/remote-preview -v remote-host:/remote/path
```

debug log:

```bash
DEBUG=1 ./bin/remote-preview -addr 127.0.0.1:7391 remote-host:/remote/path
```

debug logは標準ライブラリの`log/slog`によるkey-value形式で、HTTP request、cache hit/miss、batch listingのfallback、SSH commandの`operation`・`remote_path`・`duration`・`bytes`を出力します。`DEBUG`未設定時はdebug levelのログを出力しません。`-v`は通常のrequest logを有効にします。

## How it works

ブラウザから要求が来ると、ローカル側の `remote-preview` がsystem `ssh` を呼びます。directory listingはcache miss時にforegroundで取得し、対応platformでは一時配置したGo helperがcurrentと最大16個の直下directoryのlistingを1回で取得します。helperを利用できない場合はportable shell batchへ、さらに失敗した場合はsingle-directory listingへfallbackします。同じdirectoryへの同時アクセスは1回のSSH listingにまとめます。background prefetchは行いません。ファイル内容はcacheしません。

helperは最初のbatch listing時にremote platform判定とtemporary directoryへのuploadを行うため、ProxyJumpや高RTTの環境では最初の表示だけshell batchより遅くなる場合があります。helperはremote-previewのprocess中だけ利用し、終了時にcleanupを試みます。

```text
Browser
   |
   | HTTP localhost
   v
remote-preview
   |
   | system ssh
   | (~/.ssh/config / ProxyJump / agent ...)
   v
temporary helper or POSIX shell
   |
Remote filesystem
```

リモートにはHTTP serverや恒久的daemonを起動しません。helperは短命processとしてSSH commandから起動し、remote-preview終了時にcleanupを試みます。

## Limitations

- cacheされていないremote accessではSSH processを起動する
- directory listingはGo helperまたはportable remote shellを使う
- directory listing cacheの既定TTLは10秒、最大256エントリ
- batch listingのprotocolはtab/newlineを含むfilenameを保持する。single-directory shell fallbackではnewlineを含むfilenameを完全には扱えない
- Range request未対応
- live reload未対応
- file edit / upload / rename / deleteは未対応
- Markdown / Mermaidのrich renderingはCDN依存
- SSH URI形式やIPv6 literalのtarget parserは未対応
- Windows remote helperは未対応で、現状はshell fallbackを試みる
- helperの初回setupではplatform判定とbinary uploadが発生するため、高RTTやProxyJump環境では初回表示が遅くなる場合がある

SSH ControlMasterを有効にすると、requestごとのconnection overheadをかなり減らせます。

```sshconfig
Host *
    ControlMaster auto
    ControlPersist 5m
    ControlPath ~/.ssh/cm-%C
```
