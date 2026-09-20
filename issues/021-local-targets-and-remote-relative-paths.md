# Issue 021: local filesystem targets and remote relative paths

## Goal

`remote-preview`でlocal filesystemを直接閲覧できるようにし、remote targetでもhome基準の`~`・相対pathを利用できるようにする。

## Scope

- `.`, `./...`, `../...`、絶対path、local `~` / `~/...`をlocal targetとして扱う。
- 既存のbare host shorthand（`remote-host`）はremote home targetとして維持する。
- local `RemoteFS` backendでkind、directory listing、file readを実装する。
- `host:~`、`host:~/path`、`host:relative/path`、`host:../path`をremote home基準で解決する。
- local/remote targetに共通のhandler、HTML preview、text/image/raw表示、listing cacheを再利用する。
- README、PLAN、target/backendのテストを更新する。

## Relevant files

- `internal/preview/target.go`
- `internal/preview/target_test.go` または `internal/preview/main_test.go`
- `internal/preview/remote.go`
- `internal/preview/local.go`
- `internal/preview/local_test.go`
- `internal/preview/main.go`
- `README.md`
- `PLAN.md`

## Acceptance criteria

- `remote-preview .`でcurrent working directoryをlocal browserへ表示できる。
- `remote-preview ./subdir`、`remote-preview ../parent`、`remote-preview /absolute/path`で指定local directory/fileを表示できる。
- local directoryのlisting、local text/HTML/image/binary fileの表示が既存handlerで動作する。
- local targetでSSH processを起動しない。
- `remote-preview remote-host`のremote home shorthandが維持される。
- `remote-preview remote-host:/absolute/path`が維持される。
- `remote-preview remote-host:~`と`remote-host:~/path`がremote `$HOME`基準で解決される。
- `remote-preview remote-host:relative/path`もremote `$HOME`基準で解決される。
- remote targetの`..`は既存のpath cleaning方針に従い、解決後のabsolute pathとして扱われる。
- `go test ./...`、`go test -race ./...`、`go vet ./...`、`go build ./...`が通る。

## Design decisions

- bare name（例: `remote-host`）は既存互換のためremote host shorthandとし、local bare directoryは`./name`で指定する。
- local targetは`filepath.Abs`で起動時にabsolute pathへ解決し、以降は既存のpath-based handlerへ渡す。
- local backendは`os.Stat` / `os.ReadDir` / `os.ReadFile`を使い、RemoteFS interfaceを満たす。batch listingは実装せず既存のsingle-directory経路を使う。
- remote home-relative targetはSSHで`$HOME`を一度解決し、`path.Join(home, relative)`でabsolute pathへ変換する。

## Verification

- local target parse/absolute resolution tests
- remote home-relative target parse/resolve tests
- local backend kind/list/read tests
- local targetがSSH backendを使わない構成test
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go build ./...`

## Current state / blocker

- 実装完了。
- local targetを`filepath.Abs`で解決し、`os.Stat` / `os.ReadDir` / `os.ReadFile`を使うRemoteFS backendを追加した。
- remoteの`~`、`~/...`、相対pathをSSH `$HOME`解決後のabsolute pathへ変換するようにした。
- bare nameは既存互換のためremote host shorthandとして維持した。

## Changed files

- `internal/preview/target.go`
- `internal/preview/main.go`
- `internal/preview/local.go`
- `internal/preview/main_test.go`
- `internal/preview/local_test.go`
- `README.md`
- `PLAN.md`
- `issues/021-local-targets-and-remote-relative-paths.md`

## Verification

- local/remote target parse・resolve tests
- local backend kind/list/read/cancel tests
- `go test ./...`: passed
- `go test -race ./...`: passed
- `go vet ./...`: passed
- `go build ./...`: passed
