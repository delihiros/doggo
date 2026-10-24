# Doggo Release Governance

## 1. 目的

この文書は、Doggo のリリース運用を「互換性」「セキュリティ」「配布再現性」の観点で標準化するための基準です。

## 2. バージョニングと互換性ポリシー

### 2.1 SemVer

Doggo は `MAJOR.MINOR.PATCH` を採用します。

1. `MAJOR`: 破壊的変更（言語仕様、CLI 挙動、`doggo.mod`/`doggo.lock` 互換を壊す変更）
2. `MINOR`: 後方互換な機能追加
3. `PATCH`: 後方互換なバグ修正

### 2.2 互換性契約

1. 同一 `MAJOR` 内では、既存プロジェクトが追加修正なしで `doggo build`/`doggo test` を継続実行できることを目標とする。
2. `doggo.lock` の `:lock-version` を上げる変更は `MAJOR` 変更とする。
3. 既存コマンド/設定を削除する場合は、次の `MAJOR` まで移行導線をドキュメント化する。
4. 破壊的変更を含むリリースは、必ず「移行ガイド」を同時公開する。

## 3. セキュリティポリシー

### 3.1 依存取得

1. 依存は `doggo.mod` の `:deps` を唯一の宣言源とする。
2. 実行時は `doggo.lock` の整合（`mod-lock-inputs-sha256`）を必須とし、不一致時は実行しない。
3. `doggo mod sync` は現在の依存宣言全体を解決し、`doggo deps update` は選択した依存または全依存を再解決する。どちらも明示的に sync-only generation を公開し、Git 依存の commit revision、Go module 依存の exact version と内容の digest、path 依存の内容の digest を更新する。
4. lock の `module-id`/`go-module`/`revision`/`digest` と generation-relative な `install-dir` をもとに、Go module を exact version と digest で固定するなど、取得結果の再現性を担保する。

### 3.2 生成コード

1. `internal/interop/generated.go` と `internal/reify/generated_reify.go` は `mod sync` の生成物として扱う。
2. 手編集は行わず、変更は `doggo.mod`/依存更新 + 再生成で反映する。
3. 生成物差分はレビュー対象とし、意図しない API 露出増加を確認する。

`doggo new` は新しいプロジェクトの初期 sync-only generation を公開する。既存
プロジェクトでは、`doggo mod sync` と `doggo deps update` が実行ファイルを含まない
sync-only generation、`doggo build` が実行ファイルを含む built generation を
immutable な `.doggo/generations/<generation>/` として公開する。lock が指す内容の
欠落や digest 不一致は corruption error として停止し、検証・実行・release 処理は
読み込み中に download や修復を行わない。既存プロジェクトの generation の公開は
明示的な `doggo mod sync`、`doggo deps update`、`doggo build` だけが行う。

### 3.3 脆弱性対応

1. 高リスク脆弱性は最優先で `PATCH`（互換維持可能な場合）または `MAJOR`（互換破壊が必要な場合）で修正する。
2. セキュリティ修正リリースでは、影響範囲・回避策・アップグレード手順をリリースノートに明記する。
3. リリースゲートでは `govulncheck` を必須とし、スキャナが利用できない場合もリリースを停止する。

## 4. 配布ポリシー

### 4.1 成果物

1. Linux CLI: amd64 / arm64 の `.tar.gz`
2. macOS CLI: amd64 / arm64 の `.tar.gz`
3. Windows CLI: amd64 の `.zip`
4. VS Code 拡張: `doggo-vscode.vsix`
5. release bundle（`SHA256SUMS`、SBOM、`SHA256SUMS` の署名、公開鍵とその SHA-256 fingerprint）

CLI の `version` は固定値ではない。タグ付きリリースではビルド時の
`-ldflags "-X main.version=<tag>"`、`go install` 形式のモジュールビルドでは
Go build info、ローカル開発ビルドでは `dev` を使用する。GitHub Actions は
release bundle に入る generation にも同じタグを埋め込み、公開前にその
`doggo version` 出力を検証する。

### 4.2 リリース手順（標準）

1. 作業ツリーがクリーンであることを確認する。
2. 署名鍵を準備する（`doggo release keygen` または既存鍵の配置）。`keygen` は
   既定で新しい `dist/keys/` ディレクトリを一括作成し、既存ディレクトリは変更しない。
3. 成果物と release bundle を生成する（`make release-bundle`）。
4. 品質ゲートを実行する（`make release-gate`）。
5. 必要に応じて独立検証を実行する（`make release-verify`）。
6. リリースノート（互換性影響/移行手順/既知制約）を添付する。

`release-bundle`、`release-gate`、`release-verify` は同じ `RELEASE_OUT_DIR` を使う。
既定出力がすでに存在する場合は、たとえば
`make RELEASE_OUT_DIR=dist/release-v1.2.0 release-bundle release-gate` のように新しい
versioned directoryを指定する。

### 4.3 GitHub Actions による公開

`.github/workflows/release.yml` は `v*` タグ、または既存タグを指定した
手動実行で次を行う。

1. Go テスト、vet、race、Doggo stdlib テスト、`govulncheck`
2. Linux、macOS、Windows向けの再現可能なクロスビルド
3. VS Code拡張のテストとVSIX生成
4. release bundle生成、成果物チェックサム・署名の検証、SBOMファイルの存在確認
5. GitHub Releaseへの成果物、チェックサム、SBOM、署名の公開

成果物は draft Release にアップロードし、配布予定のファイル名がすべて揃うことと、
ダウンロードした内容がローカルの検証済み bundle と一致することを確認してから公開する。
アップロードや照合が失敗した場合は draft のまま残す。再実行は draft に対してのみ許可し、
想定外の asset がある場合は停止する。公開済み Release の asset は上書きしない。
同じタグの workflow は直列に実行する。

workflow は `doggo-release-public.pem` と `doggo-release-public.pem.sha256` も
追加成果物として bundle にコピーし、チェックサムと署名の対象にする。秘密鍵は
一時ファイル以外へ出力せず、公開成果物へ含めない。

リポジトリには次のActions secretsを事前登録する。

- `DOGGO_RELEASE_PRIVATE_KEY`
- `DOGGO_RELEASE_PUBLIC_KEY`

どちらかがない場合、公開ジョブはfail-closedで停止する。手動実行も指定タグの
コミットをcheckoutし、存在しないタグや版名と一致しない成果物は拒否する。

初回公開前に、保護されたブランチ上でCIの成功履歴を確認し、レビュー済みの
コミットへSemVerタグを付けてpushする。タグ作成とリポジトリ公開範囲の変更は
保守担当者の明示的なリリース操作であり、自動修正では行わない。

### 4.4 Release Bundle

`doggo release bundle` は、検証済み generation の実行ファイルと `--artifacts`
で指定した追加成果物を、配布時の basename で新しい `dist/release/` にコピーする。
同じディレクトリへ以下の完全性メタデータも生成する。

1. `doggo`
2. `--artifacts` で指定した追加成果物
3. `SHA256SUMS`
4. `SHA256SUMS.sig`（署名鍵指定時）
5. `sbom-cli-go-modules.json`
6. `sbom-vscode-npm-packages.json`
7. `release-bundle.json`

`SHA256SUMS` は配布ディレクトリ直下の basename だけを記録し、`doggo`、追加
成果物、両 SBOM、manifest をすべて対象にする。署名はこの `SHA256SUMS` を
対象とする。bundle 全体を private sibling directory で完成・同期してから、出力
ディレクトリとして一括公開する。既存の `--out-dir` は内容を問わず変更せず拒否する
ため、再生成時は削除ではなくバージョンを含む新しい出力先を指定する。検証時も
bundle 外のファイルがあれば拒否する。

既定では署名を必須とし、未署名 bundle を許可する場合のみ `--allow-unsigned` を明示する。

署名に使う環境変数:

1. `DOGGO_RELEASE_PRIVATE_KEY`（署名時）
2. `DOGGO_RELEASE_PUBLIC_KEY`（検証時）
3. 未指定時は `dist/keys/doggo-release-{private,public}.pem` を自動使用

`doggo release keygen [--key-dir <new-directory>]` は固定名
`doggo-release-private.pem` と `doggo-release-public.pem` を単一の private staging
directory 内で生成・同期し、directory 単位で一括公開する。既存の key directory は
変更しないため、鍵をローテーションするときは `dist/keys-v2` のような新しい場所を
指定する。

## 5. リリースゲート（必須チェック）

### 5.1 Core

1. `go env GOTOOLCHAIN GOVERSION GOTOOLDIR` が `go1.26.10` に揃っていること
2. `go test ./...`
3. `make test-coverage`
4. `doggo lint .`
5. `doggo fmt --check .`
6. `doggo test`
7. `doggo build --sync`
8. `doggo release gate`
9. `git diff --exit-code`

### 5.2 VS Code Extension

1. `cd editors/vscode`
2. `node --version` が `v22.12.0` 以上であること
3. `npm ci`
4. `npm run verify`
5. `npm run package`

### 5.3 成果物検証

1. `sha256sum dist/release/doggo`
2. `sha256sum dist/release/doggo-vscode.vsix`
3. 主要プラットフォームで起動確認（最低: Linux/WSL + VS Code Remote）
4. `doggo release verify --public-key <path>`（署名検証は既定で必須）

## 6. 利用者側のインストール

GitHub ReleaseからOS/CPUに合うアーカイブと`SHA256SUMS`を取得し、展開前に
チェックサムを検証する。Unix系では展開した`doggo`をPATH上のディレクトリへ
配置し、Windowsでは`doggo.exe`を配置する。

```bash
sha256sum --check SHA256SUMS --ignore-missing
tar -xzf doggo-vX.Y.Z-linux-amd64.tar.gz
install -m 0755 doggo "$HOME/.local/bin/"
doggo version
```

成果物のチェックサムと署名を検証し、SBOMファイルの存在を確認する場合は、
同じReleaseに含まれるrelease bundleと公開鍵を用いて
`doggo release verify`を実行する。

同じ Release に含まれる公開鍵だけでは、その Release 自体と独立した信頼の起点には
ならない。初回検証では `doggo-release-public.pem.sha256` の値を、保護された
リポジトリ文書や保守担当者が提示する別経路などの信頼済みチャネルに掲載された
fingerprint と比較して固定する。以後のリリースでも固定済み fingerprint と一致する
公開鍵だけを使用する。
