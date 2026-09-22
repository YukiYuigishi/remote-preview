# Issue 025: download and upload transfers

## Goal

Webファイラーからファイルを個別または複数選択でダウンロードでき、ファイル・ディレクトリをドラッグ&ドロップまたはファイル選択でlocal/remote targetへアップロードできるようにする。既存のread-only利用を壊さず、rsyncの考え方を参考にした安全で再実行可能な転送を目指す。

## Scope

- directory listingに選択UIを追加する。
  - ファイル単位の個別download
  - 複数選択したファイル・ディレクトリの単一ZIP download
  - ディレクトリ選択時は配下を再帰的に含め、relative pathを保持する
- upload UIを追加する。
  - current directoryへのファイルdrag & drop
  - file pickerによるファイル選択
  - directory picker（`webkitdirectory`等）による階層付き選択
  - 選択ファイルのrelative pathを保持して再帰的に配置する
- local backendとSSH remote backendの両方でdownload/uploadを扱う。
- `RemoteFS`とは分離した転送用のread/write abstractionを設け、既存のdirectory cache・target root制約と統合する。
- downloadはストリーミングを基本とし、複数選択のZIPもサーバのメモリへ全量展開しない。
- uploadは一時ファイルへの書き込み後にrenameするなど、転送途中のpartial fileが公開されにくい方式にする。
- 大きなfile向けにchunk uploadを追加する。offset付きchunk送信、upload sessionのoffset確認・再送、browser UIからのpause / resumeを扱う。
- write操作は既存のread-only起動と明確に区別し、初期案では明示的なwrite opt-inなしにuploadを受け付けない。
- rsync本体やremote側の常駐daemonを必須にしない。rsyncを参考に、次の挙動を採用する。
  - directoryのrelative pathを維持したrecursive sync
  - destination側の同一内容ファイルを可能な範囲でskip
  - 中断後に同じ操作を再実行しやすいidempotentな処理
- rename、delete、任意のfile editはこのissueの対象外とする。

## Relevant files

- `internal/preview/handler.go`
- `internal/preview/templates.go`
- `internal/preview/remote.go`
- `internal/preview/local.go`
- `internal/preview/cache.go`
- `internal/preview/main.go`
- `internal/preview/target.go`
- `internal/preview/*_test.go`
- `internal/remotehelper/helper.go`
- `README.md`
- `PLAN.md`
- `issues/025-download-upload-transfers.md`

必要に応じて、転送用の新規ファイル、remote helper protocol、browser-side JavaScriptを追加する。

## Acceptance criteria

- directory listingからファイル1件をdownloadでき、適切なfilenameとContent-Dispositionが設定される。
- directory listingで複数のファイル・ディレクトリを選択し、1つのZIPとしてdownloadできる。
- ZIP内のpathはtarget rootを起点とした相対pathになり、`..`や絶対pathによるarchive entryを作らない。
- ZIP生成時に選択対象の全ファイルを一度にメモリへ保持せず、remote/localのread errorをHTTP errorとして扱える。
- file pickerまたはdrag & dropで、current directoryへ単一ファイルをuploadできる。
- directory pickerまたはdirectory dropで、複数階層のファイルをrelative pathどおりにuploadできる。
- local targetではOS filesystemへ、remote targetではSSH経由で同じupload操作を実行できる。
- upload先の親directoryを必要に応じて作成し、転送途中の中途半端なdestination fileを通常の一覧から見せない。
- 同名destination fileの上書き、skip、エラーの方針がUI・README・テストで一貫している。
- remote metadata/hashが利用できる場合の同一内容skip、または利用できない場合のfull-file fallbackを選択し、転送失敗後の再実行で既存の正常なファイルを壊さない。
- 大きなfileを8 MiB単位のchunkでuploadでき、offsetを確認してpause後にresumeまたは再送できる。全chunk受信前にdestinationへ公開せず、完了時だけatomic commitする。
- write opt-inがない起動ではupload UIまたはupload endpointが無効化され、既存のread-only利用が維持される。
- upload/downloadの全path入力について、target root外へのpath traversal、意図しないsymlink経由の書き込み、HTTP methodの悪用を防ぐ。
- request contextのcancel、サイズ上限、timeout、SSH error、archive/upload中の部分失敗をテストする。
- `go test ./...`、`go test -race ./...`、`go vet ./...`、`go build ./...`が通る。

## Design decisions

- download endpointは、個別fileと複数selectionのarchiveを別の明示的な操作として扱い、通常のpreview GETと混同しない。
- browserのdirectory uploadでは`File.webkitRelativePath`を入力として受け取るが、server側で各segmentを検証し、clientから送られたpathをそのままfilesystem pathとして使わない。
- ZIP entry名とupload relative pathはtarget rootからの相対pathに限定し、空segment、`.`、`..`、NUL、OS依存のseparatorを正規化・拒否する。
- remote uploadの実装は、まず既存のsystem `ssh` / remote helperを活用する。remote hostに`rsync`のインストールを要求しない。
- rsync風の差分判定に必要なremote metadataが不足する場合は、正確性を優先してfull-file transferへfallbackする。hash計算やdelta block transferは、初回実装の必須条件にしない。
- 書き込みflagは`-write`、上書きはbrowserの確認後に通常fileだけをatomic replace、転送の同時実行数はbrowserのchunk uploadで最大3 fileとした。remote metadata/hashがない場合はfull-file transferへfallbackする。chunk sessionのstagingはlocal temporary fileを使い、process再起動後のsession復元は行わない。

## Verification

- handlerの個別download、複数selection ZIP、HEAD/GET、invalid selection test
- ZIP entry path sanitizationと大きなfileのstreaming test
- local backendのwrite、mkdir、atomic replace、cancel/error cleanup test
- fake SSH / fake remote transportによるremote upload/downloadの引数・context・stderr test
- browser manual test: file picker、directory picker、drag & drop、進捗、cancel、error、特殊文字filename
- read-only起動時にwrite操作が無効であることのtest
- overwrite/skip/retryと同一内容skipのintegration test
- resumable chunk uploadのoffset確認、途中状態の非公開、最終chunk後のatomic commit、再送のhandler test
- browser manual test: chunk upload、pause / resume、複数fileの同時進捗
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go build ./...`

## Current state / blocker

- 実装完了。
- `transferFS`を`RemoteFS`から分離し、local backendはstreaming read、atomic temp file + rename、親directory作成を実装した。
- SSH backendはsystem `ssh`のstdin/stdout streamingとremote側temp file + renameを利用し、remoteにrsyncやdaemonを要求しない。
- `-write`を明示した起動だけuploadを有効にする。通常fileは確認後に置換し、symbolic link・directory宛ては拒否する。既定のupload requestサイズは無制限で、必要なら`-max-upload-size`でbytes単位の上限を設定できる。chunk uploadは8 MiB単位とする。
- UIはfile picker、directory picker、drag & drop、個別download、選択ZIP downloadに対応した。directory upload/dropではrelative pathを保持する。
- 8 MiB chunkの`upload-chunk` endpointとlocal staging sessionを追加し、chunk完了後のatomic commit、offset確認、browserのpause / resumeを実装した。sessionはprocess終了後に復元しない。
- rsyncのdelta block転送や同一内容skipは未実装。remote metadata/hashが不足する場合のfull-file fallbackとして扱い、今後の最適化候補に残す。

## Changed files

- `internal/preview/transfer.go`
- `internal/preview/handler_transfer.go`
- `internal/preview/handler.go`
- `internal/preview/templates.go`
- `internal/preview/local.go`
- `internal/preview/remote.go`
- `internal/preview/cache.go`
- `internal/preview/main.go`
- `internal/preview/transfer_test.go`
- `internal/preview/upload_session.go`
- `internal/preview/main_test.go`
- `README.md`
- `PLAN.md`
- `issues/025-download-upload-transfers.md`

## Verification results

- `go test ./...`: passed
- `go test -race ./...`: passed
- `go vet ./...`: passed
- `go build ./...`: passed
- resumable chunk uploadのlocal handler test: passed
