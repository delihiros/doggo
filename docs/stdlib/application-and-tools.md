# アプリケーションとツール

このページでは、コマンドラインのプログラム、テスト、対話環境、表示、例外の調査、ログとメトリクスを扱う標準ライブラリを説明します。基本構文は[言語ガイド](../language-guide.md)、名前空間の一覧は[標準ライブラリ](../standard-library.md)を参照してください。

以下の例はプロジェクトのルートで実行します。最初の例の前に一度 `doggo build` を実行し、構築した世代を `doggo exec --` で実行します。

## `doggo.cli`

### 用途

直接実行したファイルだけで開始関数を呼ぶための判定、引数の受け渡し、引数解析を提供します。開始処理は `(when (cli/entrypoint?) (cli/main run))` の形で置くと、他のファイルから読み込んだだけでは動きません。

### 読み込み

```doggo
(require '[doggo.cli :as cli])
```

### 公開 API

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `*command-line-args*` | 値。呼び出さない | 起動時の引数ベクター、未設定なら `nil` | `binding` と `main` の2引数形で一時的に変わる |
| `command-line-args` | なし | 現在束縛されている引数ベクター、未設定なら `nil` | なし |
| `entrypoint?` | なし | 直接実行中の入口ファイルなら真偽値 | 読み込まれたファイルや、その中から呼ばれた関数では `false` |
| `main` | `f`、または `f args` | `f` の戻り値 | `f` を引数展開して呼ぶ。`args` はベクターにしてその呼び出し中だけ束縛する |
| `parse-args` | `spec`、または `spec args` | `{:options ... :arguments ...}` | 未知のオプション、重複定義、値不足、不正な型はエラー。文字列値が `-` で始まる場合は `--name=value` 形式を使う |

`spec` はオプション名から設定マップへのマップです。設定の `:type` は `:string`（既定）か `:boolean`、`:short` は1文字の短縮名です。真偽値の `--flag` は `true`、`--no-flag` は `false` になります。

### 例

```doggo
(require '[doggo.cli :as cli])

(println (cli/parse-args {:verbose {:type :boolean :short :v}
                           :output {:short :o}}
                          ["-v" "-o" "out.txt" "input"]))
```

```console
$ doggo build
$ doggo exec -- example.dog
```

```text
{:options {:verbose true, :output "out.txt"}, :arguments ["input"]}
```

## `doggo.test`

### 用途

テストを名前空間ごとに登録し、条件を確かめ、結果をマップとして集計します。`deftest` で定義し、`is` で確認してから `run-tests` などで実行します。

### 読み込み

```doggo
(require '[doggo.test :as t])
```

### 公開 API

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `*each-fixtures*` | 値。呼び出さない | 名前空間ごとの各テスト用フィクスチャの atom | `use-fixtures :each` が更新する |
| `*once-fixtures*` | 値。呼び出さない | 名前空間ごとの一回限りフィクスチャの atom | `use-fixtures :once` が更新する |
| `*report-hook*` | 値。呼び出さない | ケース結果を受け取る関数の atom | `set-report-hook!` と `clear-report-hook!` が更新する |
| `*testing-contexts*` | 値。呼び出さない | 入れ子の `testing` 文脈の atom | テスト実行中に更新される |
| `*tests*` | 値。呼び出さない | `[名前空間 テスト名]` から関数への atom | `deftest` と `defproperty` が登録する |
| `are` | `[束縛...] 式 値...` | 値の組がなければ `nil`、それ以外は全組が成功すれば最後の `is` の `true` | 途中の失敗は例外。値の数が束縛数の倍数でない場合はエラーとなるマクロ |
| `clear-report-hook!` | なし | `nil` | 報告先を何もしない関数へ戻す |
| `defproperty` | `名前 束縛 本体...`、または `名前 設定 束縛 本体...` | 定義したテスト関数 | `doggo.schema/for-all` の性質テストを登録するマクロ |
| `deftest` | `名前 本体...` | 定義したテスト関数 | 現在の名前空間へ通常テストを登録するマクロ |
| `is` | `式`、任意でメッセージ | 成功なら `true` | 偽ならテスト失敗の例外。`(= expected actual)` は両方の値を結果に残すマクロ |
| `is-check-pass` | スキーマの検査結果、任意でメッセージ | 成功なら `true` | 正規の検査結果でなければ `:invalid-argument`、`:pass?` が偽ならテスト失敗の例外 |
| `is-equality-form?` | フォーム | 真偽値 | 3要素の `=` フォームかを調べる |
| `run-all-tests` | なし | `{:summary ... :cases ...}` | 登録済みの全名前空間を名前順に実行する。フィクスチャと報告フックが動く |
| `run-single-test` | `[名前空間 名前]`、テスト関数 | 1件のケースマップ | 各テスト用フィクスチャと報告フックが動く。例外は `:fail` または `:error` にする |
| `run-tests` | 名前空間 0個以上 | `{:summary ... :cases ...}` | 指定なしなら現在の名前空間を実行する。実行時間は `:duration-ms` に入る |
| `set-report-hook!` | `hook` | `hook` | 各ケース後に呼ぶ関数を設定する |
| `summarize-cases` | ケース列 | `:test`、`:pass`、`:fail`、`:error`、`:property`、`:duration-ms` のマップ | ケースを集計する |
| `test-vars` | テスト名、または `[名前空間 名前]` の列 | `{:summary ... :cases ...}` | 未登録の対象はエラー。指定したテストだけを実行する |
| `testing` | 説明文、本体... | 本体の最後の値 | 失敗ケースに説明文を積むマクロ。終了時に必ず元へ戻す |
| `thrown-with-msg?` | 文字列または正規表現、本体... | 例外メッセージが一致すれば真偽値 | 例外がなければ `false` となるマクロ |
| `thrown?` | 本体... | 例外が投げられれば `true`、なければ `false` | 真偽値を返すマクロ |
| `use-fixtures` | `:once` または `:each`、フィクスチャ関数... | `nil` | 現在の名前空間のフィクスチャを追加する。不正な範囲はエラー |

結果の `:cases` には各テストの名前、状態、文脈、期待値・実値またはエラーが入り、`:summary` には合計が入ります。時間は実行ごとに変わるため、比較や表示には必要な項目だけを選ぶと扱いやすくなります。

### 例

```doggo
(ns example.test
  (:require [doggo.test :as t]))

(t/deftest adds
  (t/is (= (+ 1 2) 3)))

(println (select-keys (:summary (t/run-tests 'example.test))
                      [:test :pass :fail :error :property]))
```

```console
$ doggo exec -- example.dog
```

```text
{:test 1, :pass 1, :fail 0, :error 0, :property 0}
```

## `doggo.repl`

### 用途

対話環境で名前や定義を調べ、直前の例外の要点を表示します。`doc` と `source` は画面へ表示するマクロで、調べた文字列が必要なコードでは `source-str` を使います。

### 読み込み

```doggo
(require '[doggo.repl :as repl])
```

### 公開 API

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `apropos` | 文字列、正規表現、またはシンボル `query` | 一致する完全修飾名のベクター | 読み込み済み名前空間を検索する |
| `dir` | 名前空間のシンボルまたは文字列 | その公開名を名前順に並べたベクター。存在しなければ `[]` | シンボル・文字列以外はエラー |
| `doc` | シンボル | `nil` | 文書メタデータを標準出力へ表示するマクロ |
| `find-doc` | 文字列、正規表現、またはシンボル `query` | `{:name ... :doc ...}` のベクター | 文書メタデータを検索する |
| `pst` | なし、例外または設定、あるいは例外と設定 | `nil` | 例外を渡す、または直前の例外があるときだけ整形済みスタックトレースを標準出力へ表示する。直前の例外がなければ何も表示しない。`{:depth n :filter pred}` を渡せる |
| `root-cause` | 例外 | 最も内側の原因例外 | 例外でない値はエラー |
| `source` | シンボル | `nil` | 定義フォームを標準出力へ表示するマクロ |
| `source-str` | シンボル | 定義フォームの文字列 | 元のソースを取得できない場合は利用不能を示す文字列 |

### 例

```doggo
(require '[doggo.repl :as repl]
         '[doggo.cli])

(println (repl/dir 'doggo.cli))
```

```console
$ doggo exec -- example.dog
```

```text
[*command-line-args* command-line-args entrypoint? main parse-args]
```

## `doggo.pprint`

### 用途

値を読みやすく整形します。`pprint-str` は文字列を返し、`pprint` は整形結果を標準出力へ表示して `nil` を返します。`table-str` は表の文字列を返すだけで表示しません。出力先を指定する writer や `:stream` のAPIはありません。

### 読み込み

```doggo
(require '[doggo.pprint :as pprint])
```

### 公開 API

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `*layout-ops*` | 値。呼び出さない | 論理ブロック内のレイアウト操作の atom、通常は `nil` | `pprint-logical-block` が動的に束縛する |
| `*pprint-dispatch*` | 値。呼び出さない | 一時的な表示関数、通常は `nil` | `with-pprint-dispatch` が動的に束縛する |
| `*printer-context*` | 値。呼び出さない | 現在の幅・字下げ・表示関数の文脈、通常は `nil` | 整形中に動的に束縛される |
| `*printer-depth*` | 値。呼び出さない | 入れ子の深さ、通常は `0` | 整形中に動的に束縛される |
| `pprint` | `value`、任意で設定マップ | `nil` | 整形した値を標準出力へ表示する。設定は `:width`、`:right-margin`、`:indent`、`:dispatch`。幅は正、字下げは0以上の整数 |
| `pprint-indent` | 0以上の整数 `n` | `nil` | 論理ブロック内の次行の字下げを指定する。外側や不正な値ではエラー |
| `pprint-logical-block` | `{:prefix ... :suffix ...}`、0引数関数 | 文字列、または入れ子の場合 `nil` | 関数内の書き込み操作を一つの整形単位にする。接頭辞・接尾辞は文字列 |
| `pprint-newline` | `:linear` または `:mandatory` | `nil` | 論理ブロック内で改行候補または必須改行を追加する。外側や不正な種類ではエラー |
| `pprint-str` | `value`、任意で設定マップ | 整形済み文字列 | 表示はしない。`:width` と `:right-margin` を両方渡す場合は同じ値でなければエラー |
| `pprint-tab` | `:line` または `:line-relative`、0以上の列、正の間隔 | `nil` | 論理ブロック内でタブ位置まで空白を追加する。外側や不正な値ではエラー |
| `table-str` | `rows`、または `headers rows` | 先頭に改行を含む表の文字列 | 表示はしない。セルは通常の値整形を使う |
| `set-pprint-dispatch!` | 1引数関数 | `nil` | 以後の既定表示関数をプロセス内で更新する。別の表示が必要なら必ず元へ戻す |
| `with-pprint-dispatch` | 1引数関数、本体... | 本体の最後の値 | 本体だけに表示関数を適用するマクロ。不正な関数はエラー |
| `write-out` | `value` | `nil` | 論理ブロック内へ文字列化した値を書き込む。外側ではエラー |

`:width` と `:right-margin` は折り返し幅の別名です。`:indent` は入れ子ごとの字下げ幅で、既定は `2` です。表示関数は値を受け取り、文字列を返すとその表示を使い、`nil` なら組み込みの表示を使います。

### 例

```doggo
(require '[doggo.pprint :as pprint])

(pprint/pprint {:a 1 :b [2 3]} {:width 12})
```

```console
$ doggo exec -- example.dog
```

```text
"{
  :a 1
  :b [2 3]
}"
```

## `doggo.stacktrace`

### 用途

例外の原因連鎖、構造化されたスタックフレーム、表示用の文字列を扱います。`format-` で始まる関数は文字列を返し、`print-` で始まる関数は標準出力へ表示して `nil` を返します。出力先を指定する writer や `:stream` のAPIはありません。

### 読み込み

```doggo
(require '[doggo.stacktrace :as st])
```

### 公開 API

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `cause-chain` | 例外 | 外側から内側へ並ぶ例外ベクター | 例外でない値はエラー |
| `filter-stack-frames` | 例外、呼び出し可能な述語 | 述語に一致したフレームのベクター | 例外でない値、または呼び出せない述語はエラー |
| `format-cause-trace` | 例外 | `[番号] 種類: メッセージ` を改行で結んだ文字列 | 例外でない値はエラー |
| `format-stack-trace` | 例外、任意で `{:depth n :filter pred}` | 原因とフレームを整形した文字列 | `:depth` は数値、`:filter` は呼び出し可能。未知の設定や不正な値はエラー |
| `format-exception` | 例外 | `種類: メッセージ` | 例外以外はエラー |
| `print-cause-trace` | 例外 | `nil` | 原因連鎖を標準出力へ表示する |
| `print-stack-trace` | 例外、任意で設定 | `nil` | スタックトレースを標準出力へ表示する。設定の検証は `format-stack-trace` と同じ |
| `root-cause` | 例外 | 最も内側の原因例外 | 例外でない値はエラー |
| `stack-frames` | 例外 | 例外の `:trace` に入ったフレーム列 | 例外でない値はエラー |

フレームのファイル名や行番号は実行場所で変わるため、テストやログの比較では件数、関数名の有無、原因の種類など安定した情報を使うと安全です。

### 例

```doggo
(require '[doggo.stacktrace :as st])

(let [e (ex-info "outer" {:kind :outer}
          (ex-info "inner" {:kind :inner}))]
  (println [(= :inner (:kind (ex-data (st/root-cause e))))
            (= "[0] :outer: outer\n[1] :inner: inner"
               (st/format-cause-trace e))]))
```

```console
$ doggo exec -- example.dog
```

```text
[true true]
```

## `doggo.observability`

### 用途

構造化ログ、相関ID、トレース、メトリクスを記録します。同じ Doggo の実行環境では状態を共有します。メトリクスと出力先の参照はロックで保護され、スナップショットはコピーを返しますが、複数呼び出し全体の順序を保証する仕組みではありません。

### 読み込み

```doggo
(require '[doggo.observability :as obs])
```

### 公開 API

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `debug` | `message`、任意でフィールドマップ | `nil` | `:debug` レベルのJSON行をログ出力先へ書く。メッセージやフィールドが不正ならエラー |
| `error` | `message`、任意でフィールドマップ | `nil` | `:error` レベルのJSON行をログ出力先へ書く。値は出力されるため秘密情報を渡さない |
| `event!` | `event`、`event fields`、または `level event fields` | `nil` | イベント名を `:event` フィールドに加えログへ書く。2引数形の第2引数はマップ必須、3引数形のフィールドはマップか `nil` |
| `info` | `message`、任意でフィールドマップ | `nil` | `:info` レベルのJSON行をログ出力先へ書く。フィールド名の `-` は `_` になる |
| `log!` | `level message`、任意でフィールドマップ | `nil` | JSON行を出力する。既定の出力先は標準エラー出力。時刻、レベル、メッセージ、相関IDやトレース情報を含み、書き込みに失敗するとエラー |
| `metric-set-gauge!` | 名前、数値 | `nil` | ゲージ値を置き換える。名前は文字列・シンボル・キーワード、値は数値でなければエラー |
| `metric-inc!` | 名前、任意で増分 | `nil` | カウンターを既定で1、指定時はその数だけ加算する。名前や増分が不正ならエラー |
| `metric-observe!` | 名前、数値 | `nil` | ヒストグラムの `:count`、`:sum`、`:min`、`:max` を更新する。名前や値が不正ならエラー |
| `metrics-snapshot` | なし | `{:counters ... :gauges ... :histograms ...}` | 現在値のコピーを返す。書き込みはしない |
| `correlation-id` | なし | 現在の相関IDの文字列、なければ `nil` | なし |
| `trace-id` | なし | 現在のトレースID。スパン外なら `nil` | なし |
| `warn` | `message`、任意でフィールドマップ | `nil` | `:warn` レベルのJSON行をログ出力先へ書く。値は出力されるため秘密情報を渡さない |
| `with-span` | スパン名、0引数関数 | 関数の戻り値 | トレースIDとスパンIDを付けて関数を実行し、成功・失敗をJSON行としてログ出力する。失敗時は例外メッセージも出力し、例外は再送出される |

ログの出力先を変えるエクスポーターの選択APIはなく、既定では標準エラー出力へJSON行を書きます。フィールド値はJSONで表せる形へ変換され、`nil` は `null`、文字列・真偽値・数値は同じ種類のJSON値になります。キーワードは `:` 付きの文字列、シンボルは文字列、シーケンスとセットは配列、マップはキーを整形した入れ子のオブジェクト、これら以外の値は表示用の文字列になります。この変換は入れ子にも行われます。呼び出し側のフィールドは最後に加わるため、`timestamp`、`level`、`message`、`correlation_id`、`trace_id`、`span_id`、`span` は予約名として使わないでください。自動的な秘匿化はなく、`with-span` が失敗時に出す例外メッセージにも秘密情報が含まれ得ます。メトリクスにはリセットAPIがないため、テストでは `doggo exec -- example.dog` で新しいプロセスを使うか、読み取りだけの例にします。

### 例

```doggo
(require '[doggo.observability :as obs])

(println (get (obs/metrics-snapshot) :counters))
```

```console
$ doggo exec -- example.dog
```

```text
{}
```
