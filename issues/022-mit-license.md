# Issue 022: add MIT license

## Goal

remote-previewのライセンスをMITとしてrepositoryへ明示する。

## Scope

- rootに標準MIT license textを`LICENSE`として追加する。
- READMEにproject licenseを記載する。
- bundled third-party assetのlicense noticeは既存の`THIRD_PARTY_NOTICES.md`で維持する。

## Relevant files

- `LICENSE`
- `README.md`
- `THIRD_PARTY_NOTICES.md`

## Acceptance criteria

- rootの`LICENSE`がMIT License本文を含む。
- copyright holderが`Nobuyuki Ishii`として記載される。
- READMEからproject licenseを確認できる。
- third-party bundled assetの個別license noticeを壊さない。

## Verification

- `LICENSE`のMIT本文とcopyright noticeを確認
- READMEのLicense sectionを確認
- `git diff --check`

## Current state / blocker

- 実装完了。
- rootへMIT License本文を追加し、READMEにproject licenseとthird-party asset noticeへの導線を追加した。

## Changed files

- `LICENSE`
- `README.md`
- `issues/022-mit-license.md`

## Verification results

- `LICENSE`のMIT本文とcopyright noticeを確認。
- READMEのLicense sectionを確認。
- `git diff --check`: passed
