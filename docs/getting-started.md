# 15 分ではじめる Doggo

このガイドでは、ターミナルからタスクを追加して一覧表示するコマンドラインアプリケーションを作ります。最後に、コードの整形、検査、テスト、ビルドまで確認します。

## 1. 準備とインストール

Doggo は Go で実装されているため、ソースからのビルドには Go 1.26.9 が必要です。ターミナルへ入力したコマンドを実行するプログラムをシェルと呼びます。以下のコマンドは、`export` を使えるシェルで実行します。

以下を Doggo のソースディレクトリで実行します。

```bash
go version
go mod download
go install ./cmd/doggo
export PATH="$(go env GOPATH)/bin:$PATH"
doggo version
```

各コマンドは次の処理を行います。

- `go version` は、使用している Go のバージョンを表示します。
- `go mod download` は、Doggo のビルドに必要な外部コードを取得します。
- `go install` は、ソースコードから `doggo` コマンドを Go のインストール先へ作ります。
- `export PATH="$(go env GOPATH)/bin:$PATH"` は、現在のシェルから `doggo` コマンドを見つけられるようにします。`PATH` はコマンドを探すディレクトリの一覧です。
- `doggo version` は、作成したコマンドを実行できることを確認します。

`go version` が Go 1.26.9 を示し、最後のコマンドが `doggo version` に続けてバージョン番号を表示すれば準備完了です。`export` の設定は現在のシェルだけで有効です。新しいターミナルでは、現在のディレクトリに関係なく同じ `export` をもう一度実行してください。

## 2. プロジェクトを作る

Doggo のソースディレクトリの外へ移動し、`todo` という名前のプロジェクトを作ります。

```bash
cd ..
doggo new cli todo
cd todo
```

`cd ..` は一つ上のディレクトリへ移動します。`doggo new cli todo` の `cli` はコマンドラインアプリケーション用の構成、`todo` は作成するディレクトリとプロジェクトの名前です。最後の `cd todo` で、以降のコマンドを実行するプロジェクトへ移動します。新しいプロジェクトの `doggo.lock` は、依存関係と生成コードだけを固定した sync-only generation を参照します。実行には後で `doggo build` が作る built generation を使います。

プロジェクトが利用する外部コードとの関係を、依存関係と呼びます。生成されたファイルのうち、このガイドでは次の四つを使います。

- `src/todo/main.dog`: 実行するプログラム
- `test/todo/main_test.dog`: テスト
- `doggo.mod`: プロジェクトの設定と、必要な依存関係を記録するファイル
- `doggo.lock`: 実際に使う依存関係とビルド情報を固定するファイル

関連する関数や値をまとめる単位を名前空間と呼びます。プログラムを開始するときに呼ばれる関数はエントリポイントです。生成された `src/todo/main.dog` は、`todo.main` 名前空間を宣言し、受け取った引数の個数を表示します。

```doggo
(ns todo.main
  (:require [doggo.cli :as cli]))

(def run
  (fn [args]
    (str "args=" (count args))))

(def main
  (fn [& args]
    (println (run args))))

(when (cli/entrypoint?)
  (cli/main main))
```

`run` は引数の一覧を受け取り、その個数を文字列で返します。`main` の `& args` は、渡されたコマンドライン引数を一つの一覧として受け取ります。`main` は `run` の結果を表示します。最後の `when` は、ファイルがエントリポイントとして実行された場合だけ `main` を呼びます。

生成された `test/todo/main_test.dog` は、`run` に二つの引数を渡し、結果が `"args=2"` になることを確認します。

```doggo
(ns todo.main-test
  (:require [doggo.test :as t]
    [todo.main :as subject]))

(t/deftest run-test
  (t/is (= "args=2" (subject/run ["one" "two"]))))
```

`:require` は別の名前空間を読み込み、`:as` はその名前空間へ短い別名を付けます。ここでは、`doggo.test` を `t`、`todo.main` を `subject` という名前で参照します。`deftest` はテストを定義し、`is` は式の結果が真になることを確認します。

## 3. タスクをデータで表す

キーと値を組み合わせたデータをマップ、順番のある値をまとめたデータをベクターと呼びます。一件のタスクは `{:title "英語の宿題"}` のようなマップで表し、複数のタスクはベクターにまとめます。`:title` のようなキーを使うと、各値が何を表すかを名前で示せます。

入力だけで結果が決まり、ファイルなどの外部状態を変更しない関数を純粋関数と呼びます。タスクを加工する `add` と `list` を純粋関数にし、ファイルを扱う関数から分けます。`src/todo/main.dog` の内容を削除し、次の三つのコードを順に記述します。

EDN は、Doggo のベクターやマップをテキストとして保存できるデータ形式です。タスクの保存には `doggo.edn`、ファイルの確認には `doggo.io` という標準ライブラリの名前空間を使います。

```doggo
(ns todo.main
  (:require [doggo.edn :as edn]
    [doggo.io :as io]
    [doggo.cli :as cli]))

(def tasks-path "tasks.edn")

(def add
  (fn [tasks title]
    (conj tasks {:title title})))

(def list
  (fn [tasks]
    (map :title tasks)))
```

`tasks-path` は、タスクを保存するファイル名です。`add` は `conj` で新しいタスクを追加したベクターを返します。`list` は `map` で各タスクの `:title` だけを取り出します。

```doggo
(def load-tasks
  (fn []
    (if (io/exists? tasks-path)
      (edn/read tasks-path)
      [])))

(def save-tasks
  (fn [tasks]
    (edn/write tasks tasks-path)))
```

`doggo.edn` の `read` と `write` は EDN を読み書きし、`doggo.io` の `exists?` はファイルがあるかを調べます。`load-tasks` は `tasks.edn` を読み込み、まだファイルがなければ空のベクター `[]` を返します。`save-tasks` は、現在のタスクを `tasks.edn` へ保存します。

```doggo
(def main
  (fn [& args]
    (let [command (first args)
        title (second args)
        tasks (load-tasks)]
      (cond
        (and (= command "add") (not (nil? title)))
        (do
          (save-tasks (add tasks title))
          (println "追加しました:" title))

        (= command "list")
        (doseq [title (list tasks)]
          (println title))

        :else
        (println "使い方: doggo exec -- src/todo/main.dog add <タイトル> または list")))))

(when (cli/entrypoint?)
  (cli/main main))
```

`let` は、コマンド名、タイトル、保存済みのタスクへ一時的に名前を付けます。`first` と `second` は、引数の一番目と二番目を取り出します。`cond` は条件を上から順に調べ、どれも満たさなければ `:else` の処理を実行します。`do` は保存と表示を一つの分岐にまとめ、`doseq` はタイトルを一件ずつ表示します。

`and` は二つの条件を両方満たすかを調べます。`nil?` は値がない場合に `true` を返し、`not` は真偽を反転します。この組み合わせにより、`add` とタイトルの両方が指定された場合だけタスクを保存します。

`main` の `& args` は、個数が決まっていないコマンドライン引数を一つの一覧として受け取ります。最初の引数が `add` ならタスクを保存し、`list` なら保存済みのタイトルを順に表示します。`add` にタイトルがない場合は使い方だけを表示し、ファイルを変更しません。

`cli/entrypoint?` と `cli/main` により、このファイルを直接実行したときだけ `main` が呼ばれます。テストから名前空間を読み込んだ場合、コマンドの処理は実行されません。

## 4. データを加工する関数をテストする

`add` と `list` はファイルを操作しないため、入力と、関数が返した値だけで動作を確認できます。`test/todo/main_test.dog` を次の内容で置き換えます。

```doggo
(ns todo.main-test
  (:require [doggo.test :as t]
    [todo.main :as subject]))

(t/deftest add-test
  (t/is (= [{:title "英語の宿題"}]
      (subject/add [] "英語の宿題"))))

(t/deftest list-test
  (t/is (= ["英語の宿題"]
      (vec (subject/list [{:title "英語の宿題"}])))))
```

`add-test` は、空のベクターへタスクを追加した結果を確認します。`list-test` は、タスクからタイトルだけを取り出せることを確認します。`vec` は `list` の戻り値をベクターへ変換し、期待するベクターと比較できる形にします。

## 5. 整形、検査、テスト、ビルドを実行する

プログラムを実行する前に、ソースコードの形式と動作を次の順に確認します。

```bash
doggo fmt --check .
doggo lint .
doggo test
doggo build
```

各コマンドの役割は次のとおりです。

- `doggo fmt --check .` は、現在のディレクトリ以下にある `.dog` ファイルの書式を確認します。ファイルは変更しません。
- `doggo lint .` は、コードを実行せずに構文や名前空間の問題を調べます。この検査を静的検査と呼びます。
- `doggo test` は、`test` ディレクトリにあるテストを実行します。
- `doggo build` は現在世代を検証し、実行ファイルを含む新しい built generation を作ります。

各 generation は `.doggo/generations/<generation>/` に immutable な内容として保存されます。`doggo.lock` の `:build :binary` は built generation の実行ファイル（Unix 系では `.doggo/generations/<generation>/bin/doggo`、Windows では `.doggo/generations/<generation>/bin/doggo.exe`）を指し、sync-only generation では空です。読み込み時に内容の欠落や digest の不一致が見つかると `doggo.lock is stale or corrupt` として失敗し、自動修復はしません。依存関係を反映するには `doggo mod sync`、実行ファイルを作るには `doggo build` を明示的に実行します。

`doggo test` は次の形式で表示します。最後の実行時間は環境ごと・実行ごとに変わります。

```text
Ran 2 tests. 2 passed, 0 failed, 0 errors. (0.802 ms)
```

検査とビルドが成功したら、タスクを二件追加して一覧表示します。Doggo は、出力された値が文字列だと分かるように二重引用符を付けて表示します。

```console
$ doggo exec -- src/todo/main.dog add "英語の宿題"
"追加しました:" "英語の宿題"
$ doggo exec -- src/todo/main.dog add "牛乳を買う"
"追加しました:" "牛乳を買う"
$ doggo exec -- src/todo/main.dog list
"英語の宿題"
"牛乳を買う"
```

コマンドを `todo` ディレクトリで実行したため、同じディレクトリの `tasks.edn` に次のデータが保存されます。

```text
[{:title "英語の宿題"} {:title "牛乳を買う"}]
```

次に読む文書は、学びたい内容に応じて選びます。

- [言語ガイド](language-guide.md): Doggo の値、関数、条件分岐などを順に学ぶ
- [ツールの使い方](tooling.md): 整形、検査、テスト、ビルドの詳しい使い方を調べる
- [標準ライブラリ](standard-library.md): 利用できる関数と名前空間を調べる
