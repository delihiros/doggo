# 開発ツール

ここでは、プロジェクト作成からエディター連携までを順番に進めます。先に [はじめに](getting-started.md) を終えていることを前提にします。

## 1. プロジェクトを作る

```bash
doggo new app my-app
```

成功すると `Created .../my-app` に続いて、作成したファイルが表示されます。`my-app/` の中には次のファイルができます。

- `doggo.mod`
- `doggo.lock`
- `.gitignore`
- `README.md`
- `src/my_app/core.dog`
- `test/my_app/core_test.dog`

以後のコマンドはプロジェクト内で実行します。

```bash
cd my-app
```

ライブラリーには `lib`、コマンドライン用のプログラムには `cli` テンプレートも使えます。最初に失敗したら、プロジェクト名と、同名の空でないディレクトリがないかを確認します。

## 2. ファイルまたは REPL を実行する

```bash
doggo build
doggo exec -- path/to/script.dog
```

`doggo new` が作る最初の generation は依存関係と生成コードだけを持つ sync-only generation です。`doggo build` は実行ファイルを持つ新しい built generation を作り、`doggo exec --` はその検証済み実行ファイルへ後続の引数を渡します。成功すると、`println` などで明示した出力が端末に表示されます。ファイル末尾の式の値は自動では表示されず、ソースファイルも変更されません。

プロジェクトの built generation で REPL を始めるには次を実行します。

```bash
doggo exec -- repl
```

`doggo> ` に式を入力すると、その場で結果を確認できます。REPL の履歴は通常 `~/.doggo/repl_history` に保存されます。終了は `exit` または `Ctrl+C` です。

最初に失敗したら、ファイルの場所を確認します。`doggo.mod` や `doggo.lock` がないというエラーなら、プロジェクト内で実行しているかも確認します。

## 3. 整形する

```bash
doggo fmt
```

成功すると、整形が必要な `.dog` ファイルが書き換えられ、`formatted ファイル名` と表示されます。変更がなければ何も表示されません。書き換えずに確認するには次を使います。

```bash
doggo fmt --check
```

この二つのコマンドは引数を省略すると、現在のディレクトリ以下を対象にします。`--check` が失敗したら、最初の `ファイル名:行:列: source is not formatted` を確認し、`doggo fmt` を実行します。

## 4. 静的検査を実行する

```bash
doggo lint
```

成功時は何も表示せず、ファイルも変更しません。引数を省略すると現在のディレクトリ以下の `.dog` ファイルを検査します。構文、`require`、`import` の問題があると `ファイル名:行:列` と内容が表示されるので、最初の一件から直します。

## 5. テストする

```bash
doggo test
```

成功すると `Ran 件数 tests. 成功数 passed, 0 failed, 0 errors.` と表示されます。既定ではプロジェクト内の `*_test.dog` を探し、ファイルは変更しません。

失敗時は集計の後に `FAIL 名前空間/テスト名` または `ERROR 名前空間/テスト名` が表示されます。まず最初の `expected`、`actual`、`error` を確認します。特定の場所だけを実行する場合は `doggo test test/` のようにパスを渡せます。

## 6. ビルドする

```bash
doggo build
```

成功すると、現在の generation を基に実行ファイルを含む immutable built generation が確定され、`doggo.lock` がその generation を参照します。built generation の `:build :binary` は Unix 系では `.doggo/generations/<generation>/bin/doggo`、Windows では `.doggo/generations/<generation>/bin/doggo.exe` という project-relative path です。sync-only generation では `:binary` と `:sha256` が空です。

`doggo.lock is stale or corrupt` が出た場合、読み込み処理は欠落した内容や digest 不一致を修復しません。`doggo.mod` や依存関係を変更したなら `doggo mod sync`、正しい sync-only generation から実行ファイルが必要なら `doggo build` を明示的に実行します。両方を一度に行う場合だけ `doggo build --sync` を使います。

## 7. 依存関係を同期する

```bash
doggo mod sync
```

成功すると依存関係を snapshot し、Go 連携コードとともに `.doggo/generations/<generation>/` へ新しい sync-only generation として確定してから、`doggo.lock` を切り替えます。lock 内の各 `:install-dir` は選択された generation を基準にした `deps/<dependency-key>` です。既存世代は変更せず、実行ファイルも作りません。

登録済みの依存元は次で確認できます。

```bash
doggo deps list
```

既存依存を再解決するには、対象の依存元か `--all` を指定します。このコマンドも新しい sync-only generation を公開し、Git 依存なら commit revision、Go module 依存なら exact version と内容の digest、path 依存なら内容の digest を更新します。

```bash
doggo deps update --all
```

対象を指定する場合は、たとえば `doggo deps update ../greeter` を実行します。対象外の依存とその間接依存は、検証済みの旧スナップショットを保持します。保持する依存の対応を旧 lock とスナップショットから証明できない相対パスや、更新先と保持中の共有依存が衝突する場合は、lock を変更せずエラーにします。旧 lock に経路が記録されていない外部・兄弟ディレクトリの間接依存も、この制限に該当することがあります。全依存を再解決する意図がある場合に `--all` を使ってください。

共有された path 依存は、module 名を変えても同じパスの旧 snapshot と置き換えできるかを検査します。Windows の大文字・小文字や短縮名もディレクトリの同一性で照合します。対象外のソースが消失していても旧 snapshot は保持できますが、現在のパスがシンボリックリンク・ディレクトリ以外に変わった場合や、同一パスの対応が衝突する場合は、選択更新を拒否します。

ローカルの Go モジュールを追加する例です。追加時は `doggo.mod` が変わるため、その後に同期します。

```bash
doggo deps add ../greeter
doggo mod sync
```

最初に失敗したら、`doggo.mod` の `:deps` と、依存先のパスを確認します。Go モジュールなら依存先の `go.mod` も確認します。削除には `doggo deps remove ../greeter` を使います。

## 8. LSP と VS Code を使う

```bash
doggo build
doggo exec -- lsp
```

`doggo build` で現在の generation を用意した後、LSP は標準入出力で接続を待ちます。通常はエディターから起動します。

`doggo exec` は実行中の generation に read lease を保持します。`build`、`mod init/sync`、`deps add/remove/update` を `exec --` 経由で実行すると、書き込み lease を待ち続けることを防ぐため即座にエラーになります。これらは `doggo mod sync` のように直接実行してください。

このリポジトリには [VS Code 拡張](../editors/vscode/README.md) があります。`.dog` ファイルを開くと、拡張は `PATH` 上の `doggo` で `doggo exec -- lsp` を起動します。

起動できない場合は、まず `doggo build` の後に `doggo exec -- lsp` を端末で実行できるか確認します。次に VS Code の `Doggo: Show Language Server Output` を開きます。別の実行ファイルを使う場合は `doggo.lsp.command` を設定します。

LSP の名前変更は、選択したトークンがグローバル宣言またはその評価される参照である場合に実行します。ローカル変数や引用データ上からは編集を返しません。Var 参照と quasiquote 内で評価される unquote 参照も、対応する宣言と同時に変更します。
