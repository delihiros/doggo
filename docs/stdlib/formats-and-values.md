# 形式と値

このページでは、CSV、EDN、JSON、XML の読み書きと、日時、テンプレート、URL、UUID の変換を説明します。名前空間の一覧は[標準ライブラリ](../standard-library.md)、基本構文は[言語ガイド](../language-guide.md)を参照してください。

`read-string` と `write-string` は文字列や値だけを変換します。`read` と `write` は、パスまたは読み書き用ストリームを扱います。ストリームには `doggo.io` が作るもののほか、`doggo.process/stdout` と `doggo.process/stderr` が返す読み込み用ストリーム、`doggo.process/stdin` が返す書き込み用ストリームも含まれます。

以下の例はプロジェクトのルートで実行します。最初の例の前に一度 `doggo build` を実行し、構築した世代を `doggo exec --` で実行します。

## `doggo.csv`

### 用途

CSV の文字列またはファイルを、行と列からなるデータへ変換します。通常は行をベクターとして読み、`:headers true` では先頭行を列名とするマップとして読みます。

### 読み込み

次はコード断片です。

```doggo
(require '[doggo.csv :as csv])
```

### 公開 API

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `read-string` | CSV 文字列 `text`、省略可能な `{:headers boolean}` | `:headers false` では文字列セルの行ベクター、`true` では列名文字列をキーとするマップのベクター | 変換のみ。不正な CSV、空または重複した列名、真偽値でない `:headers` はエラー |
| `read` | パスまたは読み込み用ストリーム `source`、省略可能な `{:headers boolean}` | `read-string` と同じ | 入力を読む。読めない入力、不正な UTF-8、CSV、オプションはエラー |
| `write-string` | 同じ列数を持つ、空でない文字列行のベクター `rows` | CSV 文字列 | 変換のみ。行の列数が異なる場合や文字列以外のセルはエラー。行順と列順を保ち、各行を改行で終える |
| `write` | `rows target`。`target` はパスまたは書き込み用ストリーム | `nil` | パスは上書きし、ストリームへ出力する。追記を指定したファイル用ストリーム、書けない対象、不正な行はエラー |

引数や CSV が不正な場合、例外データには `:kind :invalid-argument` と呼び出した `:function` が入ります。引数を特定できる場合は `:argument`、`:expected`、`:actual` も入ります。

### 例

```doggo
(require '[doggo.csv :as csv])

(prn (csv/read-string "name,age\nAda,36\n" {:headers true}))
(prn (csv/write-string [["name" "age"] ["Ada" "36"]]))
```

```console
$ doggo build
$ doggo exec -- example.dog
```

```text
[{"name" "Ada", "age" "36"}]
"name,age\nAda,36\n"
```

## `doggo.edn`

### 用途

EDN を Doggo の値へ変換し、Doggo の値を EDN として書き出します。EDN は、キーワード、リスト、ベクター、マップ、セットなどを文字列で表せるデータ形式です。

### 読み込み

次はコード断片です。

```doggo
(require '[doggo.edn :as edn])
```

### 公開 API

読み込みオプションは `:readers`、`:default`、`:location?` です。単一値を読む `read-string` と `read` だけは、空の入力に返す値を `:eof` でも指定できます。書き込みオプションは `:pretty?`、`:indent`、`:sort-keys?`、`:newline?` です。

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `read-string` | EDN 文字列 `text`、省略可能な読み込みオプション | 1 個の値。空白だけなら `nil` または `:eof` の値 | 変換のみ。不正な EDN、複数の値、未登録のタグ、オプション不正はエラー |
| `read` | パスまたは読み込み用ストリーム `source`、省略可能な読み込みオプション | `read-string` と同じ | 入力を読む。読み込み失敗と `read-string` と同じ変換エラー |
| `read-string-all` | EDN 文字列 `text`、省略可能な `:readers`、`:default`、`:location?` | 全値のベクター。空白だけなら `[]` | 変換のみ。`:eof` は使えない。不正な EDN、タグ、オプションはエラー |
| `read-all` | パスまたは読み込み用ストリーム `source`、省略可能な `:readers`、`:default`、`:location?` | `read-string-all` と同じ | 入力を読む。`:eof` は使えない。読み込み失敗や変換エラー |
| `write-string` | 値 `value`、省略可能な書き込みオプション | EDN 文字列 | 変換のみ。`:sort-keys? true` はスカラー値のマップキーだけを表示文字列順にして決定的に出力し、それ以外のキーはエラー |
| `write` | `value target`、省略可能な書き込みオプション。`target` はパスまたは書き込み用ストリーム | `nil` | パスは上書きし、ストリームへ出力する。追記用ストリームでは追記する。書き込み失敗または `write-string` と同じ変換エラー |

`:readers` はタグから1引数の処理関数へのマップ、`:default` はタグと値を受け取る2引数の処理関数です。`:location? true` では読み込みエラーに位置情報が付きます。タグ付きリテラル処理関数の失敗では、例外データの `:context` に解析済みの `:tag` と `:value` も入ります。

`:pretty? true` は改行と字下げを加え、`:indent` は0以上の字下げ幅を指定します。`:newline? true` は末尾に改行を1個加えます。

整数と浮動小数点数の型は保存・再読み込み後も保持します。たとえば `1.0` は `1.0` として書き出し、`-0.0` の符号も保持します。

### 例

```doggo
(require '[doggo.edn :as edn])

(prn (edn/read-string-all "1 :two {:three 3}"))
(prn (edn/write-string {:b 2 :a 1} {:sort-keys? true}))
```

```console
$ doggo exec -- example.dog
```

```text
[1 :two {:three 3}]
"{:a 1, :b 2}"
```

## `doggo.json`

### 用途

JSON と Doggo の値を相互に変換します。JSON のオブジェクトはマップ、配列はベクター、`null` は `nil` になります。

### 読み込み

次はコード断片です。

```doggo
(require '[doggo.json :as json])
```

### 公開 API

読み込みでは `:key-fn` に `:string` または `:keyword`、`:number-mode` に `:int-or-float`、`:float`、`:string` を指定できます。書き込みでは `:pretty true`、または `{:pretty {:prefix "..." :indent "..."}}` を指定できます。

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `read-string` | JSON 文字列 `text`、省略可能な読み込みオプション | JSON を表す Doggo の値 | 変換のみ。不正な JSON、余分な2個目の値、不正なオプション、既定動作で64ビット整数範囲を外れる整数はエラー |
| `read` | パスまたは読み込み用ストリーム `source`、省略可能な読み込みオプション | `read-string` と同じ | 入力を読む。読み込み失敗と `read-string` と同じ変換エラー |
| `write-string` | 値 `value`、省略可能な書き込みオプション | JSON 文字列 | 変換のみ。表現できない値や、文字列・キーワード・シンボル以外のマップキー、同じ JSON 名になる複数のキーはエラー。オブジェクトのキーを文字列順に並べるため出力は決定的 |
| `write` | `value target`、省略可能な書き込みオプション。`target` はパスまたは書き込み用ストリーム | `nil` | パスは上書きし、ストリームへ出力する。追記を指定したファイル用ストリーム、書き込み失敗、変換エラーはエラー |

既定の `:number-mode :int-or-float` は整数を64ビット整数、小数と指数表記を浮動小数点数にします。`:number-mode :string` は整数、小数、指数表記を含むすべての JSON 数値を入力表記の文字列で返すため、64ビット整数範囲外の整数も保持できます。

マップの `:role`、`"role"`、`'role` はいずれも JSON の `"role"` になります。同じマップにこれらが重複している場合は、値の欠落を防ぐため変換エラーになります。`write` でも変換を完了してから出力するため、このエラーで既存ファイルを上書きしません。

### 例

```doggo
(require '[doggo.json :as json])

(prn (json/read-string "{\"name\":\"Ada\",\"active\":true}" {:key-fn :keyword}))
(prn (json/write-string {:name "Ada" :active true}))
```

```console
$ doggo exec -- example.dog
```

```text
{:active true, :name "Ada"}
"{\"active\":true,\"name\":\"Ada\"}"
```

## `doggo.xml`

### 用途

XML と、`{:tag ... :attrs ... :content ...}` というノードのマップを相互に変換します。`:tag` は要素名のキーワード、`:attrs` は属性マップ、`:content` は文字列または子ノードのベクターです。属性や内容がない場合は `nil` になります。

### 読み込み

次はコード断片です。

```doggo
(require '[doggo.xml :as xml])
```

### 公開 API

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `read-string` | XML 文字列 `text` | ルートノードのマップ | 変換のみ。不正な XML、ルート要素の不足、ルート外の内容はエラー |
| `read` | パスまたは読み込み用ストリーム `source` | ルートノードのマップ | 入力を読む。読み込み失敗または不正な XML はエラー |
| `write-string` | ノードマップ `node` | XML 宣言を含む文字列 | 変換のみ。不正なノード形状や要素名・属性名はエラー。属性を名前順に並べるため、同じノードの出力は決定的 |
| `write` | `node target`。`target` はパスまたは書き込み用ストリーム | `nil` | パスは上書きし、ストリームへ出力する。追記を指定したファイル用ストリーム、書き込み失敗、不正なノードはエラー |
| `tag` | ノードマップ `node` | `:tag` の値 | なし |
| `attrs` | ノードマップ `node` | `:attrs` の値 | なし |
| `content` | ノードマップ `node` | `:content` の値 | なし |

書き込み時は、要素名と属性名を [XML 1.0 Fifth Edition の Name 構文](https://www.w3.org/TR/xml/#NT-Name)で検査します。名前空間の接頭辞はそのまま保持します。不正な名前を持つ子ノードも出力前に検査するため、変換エラーで既存ファイルを上書きしません。

### 例

```doggo
(require '[doggo.xml :as xml])

(let [node (xml/read-string "<root b=\"2\" a=\"1\"><child>text</child></root>")]
  (prn [(xml/tag node) (xml/attrs node) (count (xml/content node))]))
(prn (xml/write-string {:tag :root :attrs {:b "2" :a "1"} :content nil}))
```

```console
$ doggo exec -- example.dog
```

```text
[:root {:a "1", :b "2"} 1]
"<?xml version='1.0' encoding='UTF-8'?>\n<root a='1' b='2'/>"
```

## `doggo.instant`

### 用途

ISO 8601 の日時文字列を解析し、UTC の日時、Unix エポックからのミリ秒、入力の UTC オフセットを持つマップへ変換します。

### 読み込み

次はコード断片です。

```doggo
(require '[doggo.instant :as instant])
```

### 公開 API

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `parse-timestamp` | ISO 8601 文字列 `text` | `:instant`、`:epoch-millis`、`:offset-seconds` を持つマップ | 純粋な変換。不正な日時、日付、UTC オフセットはエラー |

`2024` や `2024-01` のように省略した入力は、省略部分を年初・月初・午前0時として補います。`:instant` は UTC に正規化されますが、入力の UTC オフセットは秒単位で `:offset-seconds` に残ります。UTC オフセットを省略した入力は UTC、`:offset-seconds 0` として解析されます。

### 例

```doggo
(require '[doggo.instant :as instant])

(let [value (instant/parse-timestamp "2024-01-02T12:04:05+09:00")]
  (prn [(:instant value) (:epoch-millis value) (:offset-seconds value)]))
```

```console
$ doggo exec -- example.dog
```

```text
["2024-01-02T03:04:05Z" 1704164645000 32400]
```

## `doggo.template`

### 用途

式の中にあるシンボルを値で置き換えます。同じ形の式を複数の値の組に対して繰り返すマクロも提供します。

### 読み込み

次はコード断片です。

```doggo
(require '[doggo.template :as template])
```

### 公開 API

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `apply-template` | シンボルのベクター `argv`、式 `expr`、対応する値のコレクション `values` | シンボルを再帰的に置き換えた式 | 純粋な変換。`argv` がベクターでない、またはシンボル以外を含む場合はエラー |
| `do-template` | シンボルのベクター `argv`、式 `expr`、`argv` の個数ごとに並べた値 | マクロ展開後は `do` 式。実行結果は最後の式の値 | マクロ。実行時の副作用とエラーは `expr` に依存する |

### 例

```doggo
(require '[doggo.template :as template])

(prn (template/apply-template '[x] '(+ x x) [3]))
(template/do-template [x y] (println (+ x y)) 1 2 3 4)
```

```console
$ doggo exec -- example.dog
```

```text
(+ 3 3)
3
7
```

## `doggo.time`

### 用途

現在時刻、時点を表す値、期間を表す `{:millis ...}` マップを扱います。時点の計算、比較、表示、待機ができます。

### 読み込み

次はコード断片です。

```doggo
(require '[doggo.time :as time])
```

### 公開 API

`now` はシステム時計を読み、`sleep` は実行を待機させます。それ以外は、同じ引数に同じ結果を返す純粋な変換または計算です。

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `now` | なし | UTC の時点マップ | システム時計を読むため結果は実行時刻に依存する |
| `from-epoch-millis` | Unix エポックからのミリ秒 `millis` | UTC の時点マップ | 純粋。整数でない値または64ビット整数範囲外はエラー |
| `format` | 時点 `instant`、省略可能な `{:offset :utc}`、`{:offset :preserve}`、または `{:offset-seconds n}` | ISO 8601 文字列 | 純粋。不整合な時点、未対応オプション、RFC 3339 の範囲外または分単位でないオフセットはエラー |
| `epoch-millis` | 時点 `instant` | Unix エポックからのミリ秒 | 純粋。不整合な時点はエラー |
| `offset-seconds` | 時点 `instant` | UTC オフセットの秒数 | 純粋。不整合な時点はエラー |
| `millis` | 数値 `value` | `{:millis value}` | 純粋。数値以外はエラー |
| `seconds` | 数値 `value` | ミリ秒へ換算した期間マップ | 純粋。数値以外はエラー |
| `minutes` | 数値 `value` | ミリ秒へ換算した期間マップ | 純粋。数値以外はエラー |
| `hours` | 数値 `value` | ミリ秒へ換算した期間マップ | 純粋。数値以外はエラー |
| `days` | 数値 `value` | ミリ秒へ換算した期間マップ。1日は24時間 | 純粋。数値以外はエラー |
| `sleep` | 待機するミリ秒、または期間マップ | `nil` | 指定時間だけ実行を待機する。負数、整数でない値、対応範囲外はエラー |
| `in-millis` | 期間 `duration` | ミリ秒 | 純粋。不正な期間はエラー |
| `in-seconds` | 期間 `duration` | 秒数 | 純粋。不正な期間はエラー |
| `in-minutes` | 期間 `duration` | 分数 | 純粋。不正な期間はエラー |
| `in-hours` | 期間 `duration` | 時間数 | 純粋。不正な期間はエラー |
| `in-days` | 期間 `duration` | 日数 | 純粋。不正な期間はエラー |
| `plus` | 時点 `instant`、期間 `duration` | 期間を加えた時点 | 純粋。不整合な時点または期間はエラー |
| `minus` | 時点と期間、または二つの時点 | 期間を引いた時点、または二時点間の期間 | 純粋。不整合な時点または期間はエラー |
| `compare` | 二つの時点 `left right` | 前なら `-1`、同時なら `0`、後なら `1` | 純粋。不整合な時点はエラー |
| `before?` | 二つの時点 `left right` | `left` が前なら真偽値 | 純粋。不整合な時点はエラー |
| `after?` | 二つの時点 `left right` | `left` が後なら真偽値 | 純粋。不整合な時点はエラー |

### 例

```doggo
(require '[doggo.instant :as instant])
(require '[doggo.time :as time])

(let [base (instant/parse-timestamp "2024-01-02T12:04:05+09:00")]
  (prn [(time/format base)
        (time/format base {:offset :preserve})
        (time/format (time/plus base (time/minutes 1)) {:offset :preserve})]))
```

```console
$ doggo exec -- example.dog
```

```text
["2024-01-02T03:04:05Z" "2024-01-02T12:04:05+09:00" "2024-01-02T12:05:05+09:00"]
```

## `doggo.url`

### 用途

階層型 URL を構成要素へ分け、組み立て直します。相対 URL の解決と、問い合わせ文字列の変換も行います。

### 読み込み

次はコード断片です。

```doggo
(require '[doggo.url :as url])
```

### 公開 API

URL の構成要素は `:scheme`、`:host`、`:path`、`:query`、`:fragment` の文字列を持つマップです。`:path` と `:query` はエスケープ済みの文字列を保ちます。

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `parse` | 絶対または相対 URL 文字列 `text` | 5個の構成要素を持つマップ | 純粋。不正な UTF-8、階層型でない URL、ユーザー情報付き URL はエラー |
| `format` | URL の構成要素マップ `components` | URL 文字列 | 純粋。未対応キー、不正な構成要素、保持できないエスケープはエラー |
| `resolve` | 絶対 URL `base`、相対または絶対 URL `ref` | 解決後の URL 文字列 | 純粋。`base` が絶対 URL でない、または URL が不正ならエラー |
| `encode-query` | 文字列キーから文字列または文字列ベクターへのマップ `query` | 問い合わせ文字列 | 純粋。キーを名前順、各ベクターの値を元の順に出すため出力は決定的。不正な型や UTF-8 はエラー |
| `decode-query` | 問い合わせ文字列 `query` | 文字列キーから文字列ベクターへのマップ | 純粋。不正なエスケープや UTF-8 はエラー |

引数や URL が不正な場合、例外データには `:kind :invalid-argument`、`:function`、`:argument`、`:expected`、`:actual` が入ります。

### 例

```doggo
(require '[doggo.url :as url])

(let [parts (url/parse "https://example.test/a%2Fb?x=1#top")]
  (prn [(:scheme parts) (:host parts) (:path parts) (:query parts) (:fragment parts)])
  (println (url/format parts)))
```

```console
$ doggo exec -- example.dog
```

```text
["https" "example.test" "/a%2Fb" "x=1" "top"]
"https://example.test/a%2Fb?x=1#top"
```

## `doggo.uuid`

### 用途

UUID 文字列を UUID 値へ変換し、安全な乱数源からバージョン4 UUID を作ります。値が UUID かどうかも判定できます。

### 読み込み

次はコード断片です。

```doggo
(require '[doggo.uuid :as uuid])
```

### 公開 API

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `parse` | UUID 文字列 `text` | UUID 値。`str` で小文字の標準表記になる | 純粋。不正な UUID または UTF-8 はエラー |
| `random` | なし | 無作為なバージョン4 UUID 値 | システムの安全な乱数源を読むため結果は予測できず、実行ごとに変わり得る。乱数源の失敗はエラー |
| `uuid?` | 値 `value` | UUID 値なら真偽値 | 純粋。エラーなし |

### 例

`random` の値そのものは実行ごとに変わり得るため、例では UUID かどうかだけを表示します。

```doggo
(require '[doggo.uuid :as uuid])

(let [id (uuid/parse "550E8400-E29B-41D4-A716-446655440000")]
  (println (str id))
  (prn [(uuid/uuid? id) (uuid/uuid? (uuid/random))]))
```

```console
$ doggo exec -- example.dog
```

```text
"550e8400-e29b-41d4-a716-446655440000"
[true true]
```
