# 基本とデータ

このページでは、日常的な計算とコレクション操作、集合・文字列・数値、データの比較と変換、木構造の走査、スキーマを扱う標準ライブラリを説明します。名前空間の一覧は[標準ライブラリ](../standard-library.md)、言語の基本構文は[言語ガイド](../language-guide.md)を参照してください。

以下の例はプロジェクトのルートで実行します。最初の例の前に一度 `doggo build` を実行し、構築した世代を `doggo exec --` で実行します。

| 分類 | 名前空間 |
| --- | --- |
| 実行環境と基本操作 | `doggo.basis`, `doggo.core` |
| 集合・文字列・数学 | `doggo.set`, `doggo.string`, `doggo.math` |
| データの比較・変換・走査 | `doggo.data`, `doggo.datafy`, `doggo.walk`, `doggo.zip` |
| 検証とプロパティベーステスト | `doggo.schema` |

## `doggo.basis`

### 用途

実行中のプロジェクト、依存関係、読み込みパス、生成物の情報を取得します。現在の状態と起動時の状態を区別できます。

`current-basis` は呼び出すたびに `doggo.mod` を読み直し、現在の live project load path と、起動時に `GenerationSession` が検証して保持したロックスナップショットを組み合わせます。ロックファイルを再度開いたり、その場で修復したりはしません。`initial-basis` は起動時に記録した基盤をそのまま返します。generation dependency は opaque な検証済み root から読み込まれ、`:load-paths` には公開されません。

### 読み込み

次はコード断片です。

```doggo
(require '[doggo.basis :as basis])
```

### 公開API

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `current-basis` | なし | 現在の基盤を表すマップ | `doggo.mod` を読めない、または解析できない場合はエラー |
| `initial-basis` | なし | 起動時の基盤を表すマップ | 起動時に記録した `doggo.mod` の読み込み・解析エラーを返す |
| `project-root` | なし | プロジェクトルートの文字列 | `current-basis` の読み込みエラー |
| `module-name` | なし | モジュール名の文字列。未設定なら `nil` | `current-basis` の読み込みエラー |
| `deps` | なし | 依存指定のベクター | `current-basis` の読み込みエラー |
| `lock-deps` | なし | 解決済み依存情報のベクター | `current-basis` の読み込みエラー |
| `load-paths` | なし | パスで参照できる live project root だけを含む読み込みパスのベクター | `current-basis` の読み込みエラー |
| `generated-targets` | なし | `:go-packages`, `:reify-targets`, `:build` を持つマップ。各 reify target は `:source` と `:runtime` を持つ | `current-basis` の読み込みエラー |

### 例

```doggo
(require '[doggo.basis :as basis])

(println [(map? (basis/current-basis))
          (= (basis/current-basis) (basis/initial-basis))])
```

```console
$ doggo build
$ doggo exec -- example.dog
```

```text
[true true]
```

## `doggo.core`

### 用途

計算、判定、コレクション、高階関数、状態管理、並行処理、例外、メタデータなど、通常のプログラムで使う基本操作を提供します。

### 読み込み

`doggo.core` は起動時に自動で読み込まれるため、`require` は不要です。

### 公開API

引数の個数や型が合わない場合はエラーになります。下表では、そのほかの主要なエラーだけを記載します。

#### 実行状態・算術・比較

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `*ns*` | 値。呼び出さない | 現在の名前空間を表すシンボル | `ns` / `in-ns` で更新される |
| `*` | 数値 0 個以上 | 積。引数なしは `1` | 整数オーバーフロー |
| `+` | 数値 0 個以上 | 和。引数なしは `0` | 整数オーバーフロー |
| `-` | 数値 1 個以上 | 1 個なら符号反転、複数なら差 | 整数オーバーフロー |
| `/` | 数値 1 個以上 | 1 個なら逆数、複数なら商。浮動小数点数 | 0 除算 |
| `=` | 値 1 個以上 | すべて等しければ `true` | 比較不能な遅延値の評価エラー |
| `not=` | 値 1 個以上 | すべて同じでなければ `true` | 比較不能な遅延値の評価エラー |
| `<` | 値 1 個以上 | 厳密な昇順なら `true`。数値同士は数値順、それ以外は表示文字列順 | なし |
| `<=` | 値 1 個以上 | 非降順なら `true`。数値同士は数値順、それ以外は表示文字列順 | なし |
| `>` | 値 1 個以上 | 厳密な降順なら `true`。数値同士は数値順、それ以外は表示文字列順 | なし |
| `>=` | 値 1 個以上 | 非昇順なら `true`。数値同士は数値順、それ以外は表示文字列順 | なし |
| `abs` | 数値 `x` | 絶対値 | 最小整数の絶対値はオーバーフロー |
| `inc` | 数値 `x` | `x + 1` | 整数オーバーフロー |
| `dec` | 数値 `x` | `x - 1` | 整数オーバーフロー |
| `max` | 数値 1 個以上 | 最大値 | 数値以外はエラー |
| `min` | 数値 1 個以上 | 最小値 | 数値以外はエラー |
| `max-key` | `f x & xs` | `f` の数値結果が最大の要素 | `f` が呼べない、または数値を返さない場合はエラー |
| `min-key` | `f x & xs` | `f` の数値結果が最小の要素 | `f` が呼べない、または数値を返さない場合はエラー |
| `mod` | 整数相当の数値 `n divisor` | 除数と同じ符号の剰余 | 0 除算、有限整数範囲外 |
| `rem` | 整数相当の数値 `n divisor` | 被除数と同じ符号の剰余 | 0 除算、有限整数範囲外 |
| `rand` | なし、または正の有限数 `n` | なしなら `[0,1)`、`n` ありなら `[0,n)` の浮動小数 | 乱数状態を進める。`n <= 0` はエラー |
| `rand-int` | 正の整数相当値 `n` | `[0,n)` の整数 | 乱数状態を進める。`n <= 0` はエラー |
| `rand-nth` | 有限コレクション `coll` | 無作為な要素 | 乱数状態を進める。空またはシーケンス化不能ならエラー |
| `shuffle` | 有限コレクション `coll` | 無作為に並べ替えたベクター | 乱数状態を進める。シーケンス化不能ならエラー |
| `nano-time` | なし | プロセス開始からの単調な経過ナノ秒 | 経過時間の計測に使う。実時刻ではない |

#### 述語と型情報

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `associative?` | `x` | 連想コレクションなら真偽値 | なし |
| `atom?` | `x` | Atom なら真偽値 | なし |
| `boolean?` | `x` | 真偽値なら真偽値 | なし |
| `char?` | `x` | 文字なら真偽値 | なし |
| `coll?` | `x` | コレクションなら真偽値 | なし |
| `counted?` | `x` | 定数時間で個数を得られるなら真偽値 | なし |
| `empty?` | `x` | 空または `nil` なら真偽値 | シーケンス化不能ならエラー |
| `even?` | 整数相当の数値 `x` | 偶数なら真偽値 | 有限整数範囲外はエラー |
| `false?` | `x` | `false` なら真偽値 | なし |
| `float?` | `x` | 浮動小数点数なら真偽値 | なし |
| `fn?` | `x` | 関数なら真偽値 | なし |
| `ifn?` | `x` | 呼び出し可能なら真偽値 | なし |
| `int?` | `x` | 整数なら真偽値 | なし |
| `keyword?` | `x` | キーワードなら真偽値 | なし |
| `list?` | `x` | リストなら真偽値 | なし |
| `map?` | `x` | マップなら真偽値 | なし |
| `multimethod?` | `x` | マルチメソッドなら真偽値 | なし |
| `nat-int?` | `x` | 0 以上の整数なら真偽値 | なし |
| `neg-int?` | `x` | 負の整数なら真偽値 | なし |
| `neg?` | 数値 `x` | 負なら真偽値 | 数値以外はエラー |
| `nil?` | `x` | `nil` なら真偽値 | なし |
| `not` | `x` | `x` が偽なら `true` | なし |
| `number?` | `x` | 数値なら真偽値 | なし |
| `odd?` | 整数相当の数値 `x` | 奇数なら真偽値 | 有限整数範囲外はエラー |
| `pos-int?` | `x` | 正の整数なら真偽値 | なし |
| `pos?` | 数値 `x` | 正なら真偽値 | 数値以外はエラー |
| `protocol?` | `x` | プロトコルなら真偽値 | なし |
| `realized?` | 任意の `x` | Future・遅延シーケンスなどの保留中の値なら完了済みかの真偽値、それ以外は `true` | なし |
| `record-def?` | `x` | レコード定義なら真偽値 | なし |
| `record?` | `x` | レコード値なら真偽値 | なし |
| `reduced?` | `x` | `reduced` 値なら真偽値 | なし |
| `ref?` | `x` | Ref なら真偽値 | なし |
| `regex?` | `x` | 正規表現なら真偽値 | なし |
| `seq?` | `x` | シーケンスなら真偽値 | なし |
| `seqable?` | `x` | シーケンス化できるなら真偽値 | なし |
| `sequential?` | `x` | 順序コレクションなら真偽値 | なし |
| `set?` | `x` | セットなら真偽値 | なし |
| `some?` | `x` | `nil` 以外なら真偽値 | なし |
| `string?` | `x` | 文字列なら真偽値 | なし |
| `symbol?` | `x` | シンボルなら真偽値 | なし |
| `true?` | `x` | `true` なら真偽値 | なし |
| `type-def?` | `x` | 型定義なら真偽値 | なし |
| `type-instance?` | `x` | 型インスタンスなら真偽値 | なし |
| `var?` | `x` | Var なら真偽値 | なし |
| `vector?` | `x` | ベクターなら真偽値 | なし |
| `zero?` | 数値 `x` | 0 なら真偽値 | 数値以外はエラー |
| `type-name` | `x` | Doggo の型名の文字列 | なし |
| `go-type-name` | `x` | Go オブジェクトなら Go 型名、それ以外は `nil` | なし |

#### コレクションの作成・参照・更新

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `assoc` | `coll k v & kvs` | キーと値を関連付けた新しいマップまたはベクター | キーと値の数が不整合、型・添字不正はエラー |
| `assoc-in` | `m ks v` | 入れ子のキーへ値を関連付けたマップ | キーパスや途中の値が不正ならエラー |
| `butlast` | `coll` | 最後を除くリスト。空または `nil` なら `nil` | シーケンス化不能ならエラー |
| `concat` | `& colls` | 順に連結する遅延シーケンス | シーケンス化不能ならエラー |
| `conj` | `coll x & xs` | 要素を追加した同種のコレクション | 対応しないコレクションはエラー |
| `cons` | `x coll` | `x` を先頭に置いたシーケンス | `coll` がシーケンス化不能ならエラー |
| `contains?` | `coll key` | キーまたは添字が存在するかの真偽値 | 対応しない型はエラー |
| `count` | `coll` | 要素数 | 無限シーケンス、シーケンス化不能、評価失敗に注意 |
| `dedupe` | `coll` | 隣接する重複を除いたリスト | 全要素を実現する。シーケンス化不能ならエラー |
| `disj` | `set x` | `x` を除いたセット | セット以外はエラー |
| `dissoc` | `map key & keys` | キーを除いたマップ | マップ以外はエラー |
| `distinct` | `coll` | 重複を除いたリスト | 全要素を実現する。シーケンス化不能ならエラー |
| `find` | `map key` | `[key value]`、存在しなければ `nil` | マップ以外はエラー |
| `first` | `coll` | 先頭要素、空または `nil` なら `nil` | シーケンス化不能ならエラー |
| `flatten` | `coll` | 入れ子の順序コレクションを平坦化したリスト | 全要素を実現する。シーケンス化不能ならエラー |
| `frequencies` | `coll` | 要素から出現回数へのマップ | シーケンス化不能ならエラー |
| `get` | `coll key`、`coll key default` | 値。未存在なら `nil` または `default` | 対応しない型はエラー |
| `get-in` | `m ks`、`m ks default` | 入れ子の値。未存在なら `nil` または `default` | キーパス不正はエラー |
| `hash-map` | `& kvs` | マップ | キーと値の数が奇数ならエラー |
| `interleave` | `coll1 coll2 & colls` | 各コレクションから交互に取るリスト | 全要素を実現する。シーケンス化不能ならエラー |
| `interpose` | `sep coll` | 要素間に `sep` を挟むリスト | 全要素を実現する。シーケンス化不能ならエラー |
| `into` | `to from`、`to xform from` | `to` と同種のコレクション | 不正な変換器・コレクションはエラー |
| `key` | マップエントリー `entry` | キー | マップエントリー以外はエラー |
| `keys` | `map` | キーのシーケンス | マップ以外はエラー |
| `last` | `coll` | 最後の要素、空なら `nil` | 無限またはシーケンス化不能ならエラー |
| `list` | `& xs` | リスト | なし |
| `merge` | `& maps` | 左から統合したマップ | マップ以外はエラー |
| `merge-with` | `f & maps` | 重複キーを `f` で統合したマップ | `f` が呼べない、マップ以外はエラー |
| `next` | `coll` | 2 要素目以降のシーケンス。なければ `nil` | シーケンス化不能ならエラー |
| `nth` | `coll n`、`coll n default` | 0 始まりの要素 | 不正な添字。既定値なしの範囲外はエラー |
| `nthrest` | `coll n` | `n` 個後のシーケンス。負数は 0 と同じ | シーケンス化不能ならエラー |
| `range` | なし、`end`、`start end`、`start end step` | 数値の遅延シーケンス | `step` が 0、数値以外はエラー。引数なしは無限 |
| `rest` | `coll` | 2 要素目以降。空なら空リスト | シーケンス化不能ならエラー |
| `reverse` | `coll` | 逆順のリスト | 無限またはシーケンス化不能ならエラー |
| `rseq` | ベクター `v` | 逆順シーケンス。空なら `nil` | ベクター以外はエラー |
| `second` | `coll` | 2 要素目、なければ `nil` | シーケンス化不能ならエラー |
| `select-keys` | `map keys` | 指定キーだけを持つマップ | マップ・キー列が不正ならエラー |
| `seq` | `coll` | シーケンス。空または `nil` なら `nil` | シーケンス化不能ならエラー |
| `set` | `coll` | セット | シーケンス化不能ならエラー |
| `split-at` | `n coll` | 先頭 `n` 要素と残りのリストを持つベクター。負数は 0 と同じ | シーケンス化不能ならエラー |
| `split-with` | `pred coll` | 条件を満たす接頭部と残りのリストを持つベクター | 全要素を実現する。`pred` が呼べない、シーケンス化不能はエラー |
| `take-nth` | `n coll` | `n` 個おきの遅延シーケンス | `n <= 0`、シーケンス化不能はエラー |
| `update` | `m k f & args` | `k` の値を `f` で更新したマップ | `f` が呼べない、マップ以外はエラー |
| `update-in` | `m ks f & args` | 入れ子の値を `f` で更新したマップ | 不正なキーパス、`f` が呼べない場合はエラー |
| `val` | マップエントリー `entry` | 値 | マップエントリー以外はエラー |
| `vals` | `map` | 値のシーケンス | マップ以外はエラー |
| `vec` | `coll` | ベクター | シーケンス化不能ならエラー |
| `zipmap` | `keys vals` | 対応する要素から作ったマップ | シーケンス化不能ならエラー |

#### 高階関数・遅延シーケンス

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `apply` | `f & fixed-and-final-coll` | 最後のコレクションを展開して `f` を呼んだ結果 | `f` が呼べない、最後の値がシーケンス化不能ならエラー |
| `comp` | `& fs` | 右から左に合成した関数、または変換器 | 混在する値や呼び出せない値は実行時エラー。引数なしは `identity` 相当 |
| `complement` | `pred` | `pred` の真偽を反転する関数 | `pred` が呼べない場合は実行時エラー |
| `constantly` | `x` | 任意の引数に `x` を返す関数 | なし |
| `cycle` | `coll` | 要素を繰り返す遅延シーケンス。空なら空 | シーケンス化不能ならエラー |
| `doall` | `coll` | 全要素を実現したリスト | 副作用を実行する。無限または評価失敗に注意 |
| `dorun` | `coll` | `nil` | 全要素を走査して副作用を実行。無限または評価失敗に注意 |
| `drop` | `n`、または `n coll` | 1 引数なら変換器、2 引数なら先頭を除く遅延シーケンス。負数は 0 と同じ | シーケンス化不能ならエラー |
| `drop-while` | `pred coll` | 条件を満たす接頭部を除いたシーケンス | `pred` が呼べない、シーケンス化不能はエラー |
| `every-pred` | `pred & preds` | すべての述語が真か判定する関数 | 述語が呼べない場合は実行時エラー |
| `every?` | `pred coll` | 全要素で真なら真偽値 | `pred` が呼べない、シーケンス化不能はエラー |
| `filter` | `pred`、または `pred coll` | 1 引数なら変換器、2 引数なら抽出する遅延シーケンス | `pred` が呼べない、シーケンス化不能はエラー |
| `fnil` | `f default1`、`f default1 default2`、`f default1 default2 default3` | 先頭から対応する `nil` を既定値に置き換える関数 | `f` が呼べない場合は実行時エラー |
| `group-by` | `f coll` | `f` の結果をキー、該当要素のベクターを値とするマップ | `f` が呼べない、シーケンス化不能はエラー |
| `identity` | `x` | `x` | なし |
| `iterate` | `f x` | `x`, `(f x)` と続く無限遅延シーケンス | `f` が呼べない場合は実現時エラー |
| `juxt` | `f & fs` | 同じ引数を各関数へ渡し、結果のベクターを返す関数 | 関数が呼べない場合は実行時エラー |
| `keep` | `f coll` | `f` の `nil` 以外の結果を並べる遅延シーケンス | `f` が呼べない、シーケンス化不能はエラー |
| `keep-indexed` | `f coll` | `f` に添字と要素を渡し、`nil` 以外を並べる遅延シーケンス | `f` が呼べない、シーケンス化不能はエラー |
| `map` | `f`、または `f coll` | 1 引数なら変換器、2 引数なら変換する遅延シーケンス | `f` が呼べない、シーケンス化不能はエラー |
| `map-indexed` | `f coll` | `f` に添字と要素を渡す遅延シーケンス | `f` が呼べない、シーケンス化不能はエラー |
| `mapcat` | `f coll` | `f` の各結果を連結する遅延シーケンス | `f` が呼べない、結果がシーケンス化不能なら実現時エラー |
| `memoize` | `f` | 引数ごとに結果を保存する関数 | 内部状態を更新する。`f` のエラーは保存しない |
| `not-any?` | `pred coll` | 真になる要素がなければ真偽値 | `pred` が呼べない、シーケンス化不能はエラー |
| `not-every?` | `pred coll` | 偽になる要素があれば真偽値 | `pred` が呼べない、シーケンス化不能はエラー |
| `partial` | `f & bound-args` | 先頭引数を固定した関数 | `f` が呼べない場合は実行時エラー |
| `partition` | `n coll`、`n step coll` | 長さ `n` の完全な区画を並べるリスト | 全要素を実現する。`n` / `step` が正でない、シーケンス化不能はエラー |
| `partition-all` | `n coll`、`n step coll` | 末尾の不完全な区画も含むリスト | 全要素を実現する。`n` / `step` が正でない、シーケンス化不能はエラー |
| `partition-by` | `f coll` | `f` の結果が連続して同じ要素を区画化したリスト | 全要素を実現する。`f` が呼べない、シーケンス化不能はエラー |
| `pcalls` | `& fs` | 各 0 引数関数を並行実行した結果の遅延シーケンス | goroutine を使う。各関数のエラーは実現時に返る |
| `pmap` | `f coll` | 並行して変換する遅延シーケンス | goroutine を使う。`f` のエラーは実現時に返る |
| `reduce` | `f coll`、`f init coll` | 畳み込み結果。初期値なしの空では `f` の 0 引数呼び出し結果 | `f` を呼ぶ場合に呼び出し不能・引数個数不正、または `coll` がシーケンス化不能ならエラー |
| `reduce-kv` | `f init associative` | `f` に累積値・キー・値を渡す畳み込み結果 | `f` が呼べない、連想コレクション以外はエラー |
| `reductions` | `f coll`、`f init coll` | 途中の累積値を含む遅延シーケンス | `f` が呼べない、シーケンス化不能はエラー |
| `remove` | `pred coll` | 条件を満たさない要素の遅延シーケンス | `pred` が呼べない、シーケンス化不能はエラー |
| `repeat` | `x` | `x` を繰り返す無限遅延シーケンス | なし |
| `repeatedly` | `f`、`n f` | `f` の結果を並べる無限または最大 `n` 件の遅延シーケンス | `f` の副作用・エラーは実現時に発生 |
| `run!` | `f coll` | `nil` | 各要素で `f` を実行。呼び出し不能・シーケンス化不能はエラー |
| `some` | `pred coll` | 最初の真となる結果。なければ `nil` | `pred` が呼べない、シーケンス化不能はエラー |
| `some-fn` | `pred & preds` | 最初の真となる述語結果を返す関数 | 述語が呼べない場合は実行時エラー |
| `sort` | `coll` | 昇順のリスト。数値同士は数値順、それ以外は表示文字列順 | シーケンス化不能はエラー |
| `sort-by` | `f coll` | `f` の結果で昇順に並べたリスト。数値同士は数値順、それ以外は表示文字列順 | `f` が呼べない、シーケンス化不能はエラー |
| `take` | `n`、または `n coll` | 1 引数なら変換器、2 引数なら先頭の遅延シーケンス。0 以下なら空 | シーケンス化不能ならエラー |
| `take-while` | `pred coll` | 条件を満たす接頭部の遅延シーケンス | `pred` が呼べない、シーケンス化不能はエラー |
| `transduce` | `xform f init coll` | 変換しながら畳み込んだ結果 | 変換器・関数・コレクションが不正ならエラー |
| `reduced` | `x` | 畳み込みの早期終了を表す値 | なし |
| `unreduced` | `x` | `reduced` の中身。通常値はそのまま | なし |

#### マクロ

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `->` | `x & forms` | `x` を各形式の第 1 引数へ渡す展開結果 | 不正な形式はコンパイル時エラー |
| `->>` | `x & forms` | `x` を各形式の末尾引数へ渡す展開結果 | 不正な形式はコンパイル時エラー |
| `..` | `x & members` | Go メソッドまたはフィールドを順にたどる結果 | 未登録・不正な相互運用はエラー |
| `as->` | `expr name & forms` | 各段階を `name` に束縛した結果 | 不正な束縛はコンパイル時エラー |
| `binding` | `[var value ...] & body` | 本体の最後の値 | 現在の実行コンテキストで動的 Var を一時変更し、終了時に復元。不正な Var はエラー |
| `case` | `expr & value-result-pairs-and-default` | 最初に一致した結果、または末尾の既定値 | 一致せず既定値もない場合は `nil` |
| `comment` | `body` | `nil` | `body` を評価しない |
| `cond` | `& test-result-pairs` | 最初に真となる結果。`:else` を使用可能 | 句が不足すると意図しない値になるため、対で指定する |
| `cond->` | `expr & test-form-pairs` | 真の条件だけ第 1 引数へ通した結果 | 句は対で指定する |
| `cond->>` | `expr & test-form-pairs` | 真の条件だけ末尾引数へ通した結果 | 句は対で指定する |
| `condp` | `pred expr test1 result1 test2 result2 default` | `(pred test expr)` が最初に真となる結果、または既定値 | 現在は 2 組の候補を受け付ける |
| `defn` | `name params & body`、または `name & arity-forms` | 定義した関数 | グローバル定義を更新。不正な宣言はコンパイル時エラー |
| `defprotocol` | `name & method-specs` | プロトコル | プロトコルとディスパッチ関数を定義。重複・不正な宣言はエラー |
| `doseq` | `[name coll] & body` | `nil` | 各要素で本体の副作用を実行 |
| `dotimes` | `[name n] & body` | `nil` | `0` から `n - 1` まで本体の副作用を実行 |
| `doto` | `x & forms` | `x` | 各形式へ同じ `x` を第 1 引数として渡す |
| `extend-protocol` | `protocol & type-method-specs` | 最後の拡張結果 | 複数の型へ実装を登録。不正な型・メソッドはエラー |
| `extend-type` | `type-name protocol & method-specs` | プロトコル | 型へ実装を登録。不正な型・メソッドはエラー |
| `for` | `[name coll ...] body` | 内包表記の遅延シーケンス | `:let`, `:when`, `:while` を使用可能。不正な束縛はコンパイル時エラー |
| `future` | `& body` | Future | 本体を別の goroutine で実行 |
| `if-let` | `[name expr] then else` | 条件が真なら `then`、偽なら `else` | なし |
| `if-some` | `[name expr] then else` | `nil` 以外なら `then`、`nil` なら `else` | `false` も束縛する |
| `lazy-seq` | `& body` | 遅延シーケンス | 本体の副作用・エラーは実現時に発生 |
| `some->` | `expr & forms` | `nil` で停止する第 1 引数スレッドの結果 | なし |
| `some->>` | `expr & forms` | `nil` で停止する末尾引数スレッドの結果 | なし |
| `time` | `expr` | `expr` の結果 | 経過時間を標準出力へ 1 行表示 |
| `unless` | `cond & body` | 条件が偽なら本体の最後の値、それ以外は `nil` | なし |
| `when` | `cond & body` | 条件が真なら本体の最後の値、それ以外は `nil` | なし |
| `when-first` | `[name coll] & body` | 要素があれば先頭を束縛した本体の最後の値、なければ `nil` | シーケンス化不能ならエラー |
| `when-let` | `[name expr] & body` | 条件が真なら本体の最後の値、それ以外は `nil` | なし |
| `when-not` | `cond & body` | 条件が偽なら本体の最後の値、それ以外は `nil` | なし |
| `when-some` | `[name expr] & body` | `nil` 以外なら本体の最後の値、それ以外は `nil` | `false` も束縛する |

#### 文字列・表示・正規表現・例外・メタデータ

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `ex->map` | 例外 `e` | 正規化した例外マップ | 例外以外はエラー |
| `ex-cause` | 例外 `e` | 原因例外。なければ `nil` | 例外以外はエラー |
| `ex-data` | 例外 `e` | 付加データのマップ | 例外以外はエラー |
| `ex-info` | `message data`、`message data cause` | 例外値 | メッセージ・データ・原因の型が不正ならエラー |
| `ex-message` | 例外 `e` | メッセージ文字列 | 例外以外はエラー |
| `format` | 書式文字列 `fmt & xs` | 書式を適用した文字列 | 書式と引数の不整合は出力中に診断表現となる |
| `gensym` | なし、または文字列・シンボル `prefix` | 一意なシンボル | プロセス内カウンターを進める |
| `keyword` | 文字列・シンボル・キーワード `name`、または文字列 `ns name` | キーワード | 型不正はエラー |
| `meta` | `x` | メタデータのマップ。未対応・未設定なら `nil` | なし |
| `pr-str` | `& xs` | 読み戻せる表記を空白で連結した文字列 | 遅延値の表示で評価エラーの可能性 |
| `print` | `& xs` | `nil` | 標準出力へ改行なしで表示 |
| `println` | `& xs` | `nil` | 標準出力へ表示して改行 |
| `prn` | `& xs` | `nil` | 読み戻せる表記を標準出力へ表示して改行 |
| `re-find` | `regex s` | 最初の一致。なければ `nil` | 型不正はエラー |
| `re-matches` | `regex s` | 文字列全体の一致。なければ `nil` | 型不正はエラー |
| `re-pattern` | 文字列 `s` | 正規表現 | 不正なパターンはエラー |
| `re-seq` | `regex s` | 一致を並べる遅延シーケンス | 型不正はエラー |
| `read-string` | 文字列 `s` | 最初の Doggo 形式 | 構文・Unicode 不正はエラー |
| `slurp` | パス `path`、任意で `{:binary true}` | 文字列、バイナリ時はバイトのベクター | ファイルを読む。I/O・UTF-8・オプション不正はエラー |
| `spit` | `path content`、任意で `{:append boolean :binary boolean}` | `nil` | ファイルを書き換えまたは追記。I/O・バイト・オプション不正はエラー |
| `str` | `& xs` | 人が読む表記を連結した文字列。`nil` は空文字列 | なし |
| `subs` | `s start`、`s start end` | Unicode コードポイント位置の部分文字列 | 範囲外・型不正はエラー |
| `vary-meta` | `obj f & args` | `f` でメタデータを変換した値 | `f` が呼べない、対象がメタデータ非対応ならエラー |
| `with-meta` | `obj metadata` | メタデータを付けた値 | 対象非対応、マップ以外のメタデータはエラー |

#### 状態・並行処理・動的束縛

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `<!` | チャネル `ch` | 受信値。閉じて空なら `nil` | 値が届くまで待機。チャネル以外はエラー |
| `>!` | `ch value` | `nil` | 送信できるまで待機。閉じたチャネル、チャネル以外はエラー |
| `add-watch` | `target key f` | `target` | Atom・Agent・Ref に監視関数 `[key target old new]` を登録。対象・関数不正はエラー |
| `agent` | `initial` | Agent | 非同期状態を作成 |
| `agent-error` | Agent `a` | エラーメッセージ。なければ `nil` | Agent 以外はエラー |
| `agent?` | `x` | Agent なら真偽値 | なし |
| `alter` | `ref f & args` | 更新後の値 | `dosync` 内で Ref を更新。外部または関数不正はエラー |
| `alts!` | `ports`、任意で `{:default x :timeout ms :priority boolean}` | `[value selected-port]` | 複数の受信チャネルまたは `[ch value]` 送信候補を待機。候補・オプション不正はエラー |
| `assoc!` | `transient-map k v & kvs` | 同じ一時マップ | 一時マップを破壊的に更新。所有する論理実行の違反、永続化後、対でない引数はエラー |
| `atom` | `initial` | Atom | 同期状態を作成 |
| `await` | `agent & agents` | `nil` | 送信済み処理の完了まで待機。Agent の失敗はエラー |
| `await-for` | `timeout-ms agent & agents` | 時間内に完了なら真偽値 | 指定時間まで待機。負数・範囲外・対象不正はエラー |
| `chan` | なし、または非負のバッファ数 `n` | チャネル | チャネルを作成。負数・範囲外はエラー |
| `chan?` | `x` | チャネルなら真偽値 | なし |
| `close!` | チャネル `ch` | `nil` | チャネルを閉じる。再度閉じる、型不正はエラー |
| `compare-and-set!` | `atom old new` | 更新できたかの真偽値 | Atom を条件付き更新。監視関数のエラーを返すことがある |
| `deref` | Atom・Future・チャネル・Ref・Agent・対応する Go 値・Var `x` | 保持値または受信値 | Future・チャネル・プロセス終了参照などでは待機し、実行コンテキストのキャンセル・期限切れで待機を中止してエラーにする。対応しない値はエラー |
| `ensure` | Ref `r` | 現在値 | `dosync` 内で参照値を読み取り保護。外部・型不正はエラー |
| `future-call` | 0 引数関数 `f` | Future | `f` を別の goroutine で実行 |
| `persistent!` | 一時マップ `m` | 永続マップ | 一時マップを無効化。所有する論理実行の違反、再実行はエラー |
| `pop-dynamic-bindings` | なし | `nil` | 現在の実行コンテキストから最新の動的束縛を解除 |
| `push-dynamic-bindings` | `{Var value ...}` | `nil` | 現在の実行コンテキストへ動的 Var の束縛を追加。動的でない Var やキー不正はエラー |
| `ref` | `initial` | Ref | トランザクション状態を作成 |
| `ref-set` | `ref value` | `value` | `dosync` 内で Ref を設定。外部・型不正はエラー |
| `remove-watch` | `target key` | `target` | Atom・Agent・Ref の監視関数を解除。対象不正はエラー |
| `reset!` | `atom value` | `value` | Atom を設定し、監視関数を実行 |
| `reset-vals!` | `atom value` | `[old new]` | Atom を設定し、監視関数を実行 |
| `restart-agent` | `agent new-state & option-pairs` | Agent | 失敗した Agent を再開。`:clear-actions boolean` のみ対応 |
| `send` | `agent f & args` | Agent | Agent ごとのキューへ追加して非同期更新。関数・Agent 不正はエラー |
| `set-error-mode!` | `agent :fail` または `agent :continue` | Agent | Agent の失敗時動作を変更。モード不正はエラー |
| `swap!` | `atom f & args` | 更新後の値 | Atom を原子的に更新し、監視関数を実行。関数不正はエラー |
| `swap-vals!` | `atom f & args` | `[old new]` | Atom を原子的に更新し、監視関数を実行。関数不正はエラー |
| `dynamically-bound?` | Var `v` | 現在の実行コンテキストで動的に束縛されているかの真偽値 | Var 以外はエラー |
| `transient` | 永続マップ `m` | 一時マップ | 現在はマップのみ対応 |

#### 多態・レコード・型

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `add-method` | `multimethod dispatch-value f` | 同じマルチメソッド | メソッド表を更新。対象・関数不正はエラー |
| `create-multimethod` | `name dispatch-fn` | マルチメソッド | ディスパッチ関数が呼べない場合はエラー |
| `extends?` | `protocol type-name` | 型に実装があるかの真偽値 | プロトコル・型名不正はエラー |
| `get-field` | `type-instance field-name` | フィールド値 | インスタンス・フィールド不正はエラー |
| `get-method` | `multimethod dispatch-value` | メソッド関数。なければ `nil` | マルチメソッド以外はエラー |
| `make-record` | `record-def & values` | レコード値 | 定義やフィールド数が不正ならエラー |
| `make-record-def` | `name fields` | レコード定義 | 名前は文字列またはシンボル、フィールドはリストまたはベクター |
| `make-record-from-map` | `record-def map` | レコード値 | 定義・マップ・必須フィールド不正はエラー |
| `make-type-def` | `name fields` | 型定義 | 名前は文字列またはシンボル、フィールドはリストまたはベクター |
| `make-type-instance` | `type-def & values` | 型インスタンス | 定義やフィールド数が不正ならエラー |
| `methods` | マルチメソッド `m` | ディスパッチ値から関数へのマップ | マルチメソッド以外はエラー |
| `satisfies?` | `protocol value` | 値が実装を満たすかの真偽値 | プロトコル以外はエラー |

### 例

```doggo
(println (->> [1 2 3 4]
          (filter even?)
          (map (fn [x] (* x x)))
          vec))
```

```console
$ doggo exec -- example.dog
```

```text
[4 16]
```

## `doggo.set`

### 用途

セット演算と、マップのセットを関係として扱う選択・射影・結合を行います。

### 読み込み

次はコード断片です。

```doggo
(require '[doggo.set :as set])
```

### 公開API

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `union` | `& sets` | すべての要素を含むセット。引数なしは空セット | セット以外はエラー |
| `intersection` | `& sets` | すべてに共通する要素のセット。引数なしは空セット | セット以外はエラー |
| `difference` | `& sets` | 先頭から後続セットの要素を除いたセット。引数なしは空セット | セット以外はエラー |
| `subset?` | `a b` | `a` が `b` の部分集合かの真偽値 | セット以外はエラー |
| `superset?` | `a b` | `a` が `b` の上位集合かの真偽値 | セット以外はエラー |
| `select` | `pred coll` | シーケンス化可能なコレクションから条件を満たす要素を集めたセット | `pred` が呼べない、シーケンス化不能はエラー |
| `project` | `relation keys` | 各マップを指定キーへ射影したセット | 関係の要素がマップでない場合はエラー |
| `rename-keys` | `map key-map` | キー名を置き換えたマップ | 引数がマップでない場合はエラー |
| `rename` | `relation key-map` | 各マップのキー名を置き換えたセット | 関係の要素がマップでない場合はエラー |
| `index` | `relation keys` | 射影値から該当マップのセットへの索引マップ | 関係の要素がマップでない場合はエラー |
| `map-invert` | `map` | 値をキー、キーを値とするマップ | 値が重複すると後の項目が残る。マップ以外はエラー |
| `join` | `xrel yrel`、`xrel yrel key-map` | 共通キー、または対応キーで自然結合したセット | 関係の要素や対応表がマップでない場合はエラー |

セットを受け取る API では `nil` を空セットとして扱います。

### 例

```doggo
(require '[doggo.set :as set])

(println [(= #{1 2 3} (set/union #{1 2} #{2 3}))
          (= #{{:id 1 :name "Ada" :city "Tokyo"}}
             (set/join #{{:id 1 :name "Ada"}}
                       #{{:id 1 :city "Tokyo"}}))])
```

```console
$ doggo exec -- example.dog
```

```text
[true true]
```

## `doggo.string`

### 用途

文字列の分割、結合、空白処理、大文字・小文字変換、検索、置換を行います。位置は Unicode コードポイント単位です。

### 読み込み

次はコード断片です。

```doggo
(require '[doggo.string :as str])
```

### 公開API

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `blank?` | 文字列または `nil` `s` | `nil`、空、空白だけなら真偽値 | それ以外の型はエラー |
| `capitalize` | 文字列 `s` | 先頭を大文字、残りを小文字にした文字列 | 文字列以外はエラー |
| `ends-with?` | `s suffix` | 末尾が一致するかの真偽値 | 文字列以外はエラー |
| `escape` | `s replacements` | 文字または 1 文字文字列をキーとする置換マップを適用した文字列 | 引数型不正はエラー |
| `includes?` | `s needle` | 部分文字列を含むかの真偽値 | 文字列以外はエラー |
| `index-of` | `s needle` | 最初の位置。なければ `-1` | 文字列以外はエラー |
| `join` | `separator coll` | 各要素を区切り文字で結合した文字列 | 区切りが文字列でない、コレクションがシーケンス化不能ならエラー |
| `last-index-of` | `s needle` | 最後の位置。なければ `-1` | 文字列以外はエラー |
| `lower-case` | 文字列 `s` | 小文字へ変換した文字列 | 文字列以外はエラー |
| `re-quote-replacement` | 文字列 `s` | 置換文字列用に `\\` と `$` をエスケープした文字列 | 文字列以外はエラー |
| `replace` | `s match replacement` | すべての一致を置換した文字列 | 引数が文字列でない場合はエラー |
| `replace-first` | `s match replacement` | 最初の一致だけを置換した文字列 | 引数が文字列でない場合はエラー |
| `split` | `s separator` | 分割した文字列のベクター | 引数が文字列でない場合はエラー |
| `split-lines` | 文字列 `s` | 改行で分割したベクター | 文字列以外はエラー |
| `starts-with?` | `s prefix` | 先頭が一致するかの真偽値 | 文字列以外はエラー |
| `trim` | 文字列 `s` | 前後の Unicode 空白を除いた文字列 | 文字列以外はエラー |
| `trim-newline` | 文字列 `s` | 末尾の `\n` と `\r` をすべて除いた文字列 | 文字列以外はエラー |
| `triml` | 文字列 `s` | 先頭の Unicode 空白を除いた文字列 | 文字列以外はエラー |
| `trimr` | 文字列 `s` | 末尾の Unicode 空白を除いた文字列 | 文字列以外はエラー |
| `upper-case` | 文字列 `s` | 大文字へ変換した文字列 | 文字列以外はエラー |

### 例

```doggo
(require '[doggo.string :as str])

(println [(str/split "赤,青,緑" ",")
          (str/upper-case "Doggo")
          (str/index-of "A😀B" "😀")])
```

```console
$ doggo exec -- example.dog
```

```text
[["赤" "青" "緑"] "DOGGO" 1]
```

## `doggo.math`

### 用途

数学定数、指数・対数、三角関数、丸め、浮動小数点数の隣接値などを扱います。

### 読み込み

次はコード断片です。

```doggo
(require '[doggo.math :as math])
```

### 公開API

特記のない関数は数値を受け取り、浮動小数点数を返します。数値以外や引数個数の不一致はエラーです。数学的な定義域外では `NaN` や無限大を返すことがあります。

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `E` | 値。呼び出さない | 自然対数の底 `2.718281828459045` | なし |
| `PI` | 値。呼び出さない | 円周率 `3.141592653589793` | なし |
| `abs` | `x` | 絶対値。整数入力は整数 | 最小整数ではオーバーフロー |
| `acos` | `x` | 逆余弦、ラジアン | 定義域外は `NaN` |
| `asin` | `x` | 逆正弦、ラジアン | 定義域外は `NaN` |
| `atan` | `x` | 逆正接、ラジアン | なし |
| `atan2` | `y x` | 座標から求めた角度、ラジアン | なし |
| `cbrt` | `x` | 立方根 | なし |
| `ceil` | `x` | 正の無限大方向へ丸めた小数 | なし |
| `copy-sign` | `magnitude sign` | `sign` の符号を持つ `magnitude` | なし |
| `cos` | `x` | 余弦 | なし |
| `cosh` | `x` | 双曲線余弦 | オーバーフロー時は無限大 |
| `exp` | `x` | `e` の `x` 乗 | オーバーフロー時は無限大 |
| `expm1` | `x` | `e^x - 1` | オーバーフロー時は無限大 |
| `floor` | `x` | 負の無限大方向へ丸めた小数 | なし |
| `get-exponent` | `x` | 浮動小数点表現の指数を整数で返す | なし |
| `hypot` | `x y` | `sqrt(x*x + y*y)` | なし |
| `log` | `x` | 自然対数 | 負数は `NaN`、0 は負の無限大 |
| `log10` | `x` | 常用対数 | 負数は `NaN`、0 は負の無限大 |
| `log1p` | `x` | `log(1 + x)` | `x < -1` は `NaN` |
| `next-after` | `start direction` | `direction` 側の隣接浮動小数点数 | なし |
| `next-down` | `x` | 負の無限大側の隣接浮動小数点数 | なし |
| `next-up` | `x` | 正の無限大側の隣接浮動小数点数 | なし |
| `pow` | `x y` | `x` の `y` 乗 | 定義域外は `NaN` |
| `rint` | `x` | 最も近い偶数側へ丸めた小数 | なし |
| `round` | `x` | 最も近い整数。中間は 0 から遠い側 | `NaN` は 0、無限大は整数範囲端へ変換 |
| `scalb` | `x scale-factor` | `x * 2^scale-factor` | 係数は 32 ビット整数相当。範囲外はエラー |
| `signum` | `x` | `-1.0`, `0.0`, `1.0` のいずれか。`NaN` は `NaN` | なし |
| `sin` | `x` | 正弦 | なし |
| `sinh` | `x` | 双曲線正弦 | オーバーフロー時は無限大 |
| `sqrt` | `x` | 平方根 | 負数は `NaN` |
| `tan` | `x` | 正接 | なし |
| `tanh` | `x` | 双曲線正接 | なし |
| `to-degrees` | ラジアン `x` | 度 | なし |
| `to-radians` | 度 `x` | ラジアン | なし |
| `ulp` | `x` | `x` の最小単位幅 | `NaN` は `NaN`、無限大は無限大 |

### 例

```doggo
(require '[doggo.math :as math])

(println [(math/abs -3) (math/round 2.6) (math/pow 2 10)])
```

```console
$ doggo exec -- example.dog
```

```text
[3 3 1024]
```

## `doggo.data`

### 用途

二つの値を再帰的に比較し、左だけ、右だけ、共通部分へ分けます。

### 読み込み

次はコード断片です。

```doggo
(require '[doggo.data :as data])
```

### 公開API

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `diff` | `a b` | `[a-only b-only both]`。該当部分がなければ `nil` | マップ、セット、順序コレクションは再帰比較。遅延値の比較・実現エラーを返すことがある |

### 例

```doggo
(require '[doggo.data :as data])

(println (data/diff {:a 1 :b 2}
                    {:a 1 :b 3}))
```

```console
$ doggo exec -- example.dog
```

```text
[{:b 2} {:b 3} {:a 1}]
```

## `doggo.datafy`

### 用途

値を調査しやすい通常のデータへ変換します。通常のデータはそのまま、例外・レコード・型・Go オブジェクトは説明用マップになります。

### 読み込み

次はコード断片です。

```doggo
(require '[doggo.datafy :as datafy])
```

### 公開API

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `datafy` | `value` | データ表現。通常データは同じ値 | なし |

例外のデータ表現は `:type`, `:kind`, `:phase`, `:message`, `:cause`, `:data`, `:context`, `:at`, `:trace`, `:via` を持ちます。レコード値と型インスタンスは `:kind`, `:ns`, `:name`, `:qualified-name`, `:data`、定義はさらに `:fields`, `:meta` を持ちます。

### 例

```doggo
(require '[doggo.datafy :as datafy])

(defrecord User [name])
(println (datafy/datafy (->User "Ada")))
```

```console
$ doggo exec -- example.dog
```

```text
{:kind :record, :name "User", :ns "user", :qualified-name "user/User", :data {:name "Ada"}}
```

## `doggo.walk`

### 用途

リスト、ベクター、マップ、セットからなる入れ子のデータを走査し、各ノードを変換します。

### 読み込み

次はコード断片です。

```doggo
(require '[doggo.walk :as walk])
```

### 公開API

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `walk` | `inner outer form` | 直下の要素へ `inner`、再構築後へ `outer` を適用した値 | 関数が呼べない場合はエラー |
| `prewalk` | `f form` | 親から子の順に `f` を適用した値 | 関数が呼べない場合はエラー |
| `postwalk` | `f form` | 子から親の順に `f` を適用した値 | 関数が呼べない場合はエラー |
| `keywordize-keys` | `form` | 入れ子の全マップで文字列キーをキーワードにした値 | 同じキーワードへ衝突すると後の値が残る |
| `stringify-keys` | `form` | 入れ子の全マップでキーワードキーを名前の文字列にした値 | 同じ文字列へ衝突すると後の値が残る |
| `macroexpand-all` | `form` | 入れ子のリストを再帰的にマクロ展開した形式 | 不正なマクロ呼び出しはエラー |
| `prewalk-replace` | `replacement-map form` | 親から子の順で一致する値を置換した値 | なし |
| `postwalk-replace` | `replacement-map form` | 子から親の順で一致する値を置換した値 | なし |

### 例

```doggo
(require '[doggo.walk :as walk])

(println (walk/keywordize-keys
          {"user" {"name" "Ada"}}))
```

```console
$ doggo exec -- example.dog
```

```text
{:user {:name "Ada"}}
```

## `doggo.zip`

### 用途

木の現在位置を表すロケーションを使い、親・子・兄弟へ移動しながら不変な木を編集します。

### 読み込み

次はコード断片です。

```doggo
(require '[doggo.zip :as zip])
```

### 公開API

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `zipper` | `branch? children make-node root` | 汎用ロケーション | 3 関数が呼べない場合は移動・再構築時にエラー |
| `seq-zip` | 任意の `root` | リストまたはシーケンスを枝として扱うロケーション | それ以外のノードは葉として扱い、`down` は `nil` |
| `vector-zip` | 任意の `root` | ベクターを枝として扱うロケーション | ベクター以外のノードは葉として扱い、`down` は `nil` |
| `xml-zip` | 任意の `root` | `:tag` を持つマップを枝として扱うロケーション | それ以外のノードは葉として扱い、`down` は `nil` |
| `node` | `loc` | 現在ノード | ロケーション不正はエラー |
| `end?` | `loc` | 深さ優先走査の終端かの真偽値 | ロケーション不正はエラー |
| `branch?` | `loc` | 現在ノードが枝かの真偽値 | 判定関数のエラーを返す |
| `children` | `loc` | 子のシーケンス | 葉で呼ぶと `:invalid-state` エラー |
| `down` | `loc` | 最初の子のロケーション。なければ `nil` | ロケーション不正はエラー |
| `up` | `loc` | 親のロケーション。最上位なら `nil` | 変更済みなら木を再構築。再構築関数のエラーを返す |
| `right` | `loc` | 右隣のロケーション。なければ `nil` | ロケーション不正はエラー |
| `left` | `loc` | 左隣のロケーション。なければ `nil` | ロケーション不正はエラー |
| `leftmost` | `loc` | 同じ階層の最左ロケーション | ロケーション不正はエラー |
| `rightmost` | `loc` | 同じ階層の最右ロケーション | ロケーション不正はエラー |
| `lefts` | `loc` | 左側ノードのシーケンス。最上位なら `nil` | ロケーション不正はエラー |
| `rights` | `loc` | 右側ノードのシーケンス。最上位なら `nil` | ロケーション不正はエラー |
| `path` | `loc` | ルートから親までのベクター。最上位なら `nil` | ロケーション不正はエラー |
| `root` | `loc` | 変更を反映したルートノード | 再構築関数のエラーを返す |
| `replace` | `loc value` | 現在ノードを置換したロケーション | なし |
| `edit` | `loc f & args` | 現在ノードを `(f node args...)` で置換したロケーション | `f` が呼べない場合はエラー |
| `insert-left` | `loc item` | 左隣へ挿入したロケーション | 最上位では `:invalid-state` エラー |
| `insert-right` | `loc item` | 右隣へ挿入したロケーション | 最上位では `:invalid-state` エラー |
| `append-child` | `loc item` | 子の末尾へ追加したロケーション | 葉では `:invalid-state` エラー |
| `remove` | `loc` | 現在ノードを除き、直前位置へ移動したロケーション | 最上位では `:invalid-state` エラー |
| `next` | `loc` | 深さ優先順の次のロケーション。末尾後は終端 | 再構築関数のエラーを返す |
| `prev` | `loc` | 深さ優先順の前のロケーション。なければ `nil` | ロケーション不正はエラー |

### 例

```doggo
(require '[doggo.zip :as zip])

(println (-> [1 [2 3] 4]
          zip/vector-zip
          zip/down
          zip/right
          zip/down
          zip/right
          (zip/replace 9)
          zip/root))
```

```console
$ doggo exec -- example.dog
```

```text
[1 [2 9] 4]
```

## `doggo.schema`

### 用途

通常の Doggo データでスキーマを表し、検証・適合・失敗説明、テストデータ生成、プロパティベーステスト、反例の縮小、関数の引数と戻り値の検査を行います。

### 読み込み

次はコード断片です。

```doggo
(require '[doggo.schema :as schema])
```

### 公開API

#### スキーマ形式

単純なスキーマは `:any`, `:nil`, `:string`, `:keyword`, `:symbol`, `:bool`, `:int`, `:float`, `:number`, `:map`, `:vector`, `:set`, `:list`, `:seq` です。次の複合形式を使用できます。

- `[:maybe schema]`
- `[:vector schema]`, `[:set schema]`, `[:list schema]`
- `[:tuple schema1 schema2 ...]`
- `[:map [key schema] ...]`
- `[:map-of key-schema value-schema]`
- `[:or schema1 schema2 ...]`, `[:and schema1 schema2 ...]`
- `[:enum value1 value2 ...]`
- `[:predicate callable]`

不正なスキーマ形式は正規化エラーになります。例外データには該当する場合に `:path`, `:expected`, `:actual`, `:actual_type` が入ります。

#### 検証・説明

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `valid?` | `schema value` | 適合するかの真偽値 | スキーマ不正、述語の実行失敗はエラー |
| `explain` | `schema value` | `:valid?`, `:schema`, `:value`, `:problems` を持つマップ | スキーマ不正、述語の実行失敗はエラー |
| `defschema` | `name schema` | 定義したスキーマ | グローバル定義を更新。不正な名前はコンパイル時エラー |

`explain` の各問題は `:path`, `:expected`, `:actual`, `:message` を持ち、必要に応じて `:predicate`, `:branch` も持ちます。

#### 生成・標本

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `generate` | `schema`、任意で `options` | 生成した 1 値 | 生成不能な形式、スキーマ・オプション不正はエラー |
| `sample` | `schema n`、任意で `options` | 生成した `n` 値のベクター | `n` が負数・整数範囲外、生成不能、オプション不正はエラー |

`options` は `:seed` に整数、`:size` に非負整数、`:generator-overrides` にスキーマ種別キーワードから固定値または 0 引数関数へのマップを指定できます。同じ `:seed` とオプションは同じ結果を返します。`[:predicate ...]` と `[:and ...]` は対応する生成上書きがない場合は生成できません。

#### プロパティベーステスト

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `check` | `property schema`、任意で `options` | 成否と反例を持つレポートマップ | プロパティを繰り返し実行。呼び出し不能、生成不能、スキーマ・オプション不正だけが例外 |
| `check-report?` | `report` | 正規のレポート形状なら真偽値 | なし |
| `check-pass?` | `report` | 正規レポートの `:pass?`。不正な値は `false` | なし |
| `for-all` | `[name schema ...] & body`、または `options [name schema ...] & body` | `check` のレポートマップ | 束縛ベクター不正はエラー。プロパティ本体を繰り返し実行 |

`check` は第1引数に検査する関数を受け取ります。`options` には `:seed`、非負整数の `:iterations`, `:size`, `:shrink-limit`、生成時と同じ `:generator-overrides` を指定できます。既定の反復回数と縮小試行上限はどちらも 100 です。

単項プロパティはスキーマをそのまま指定します。複数引数は `[:tuple ...]`、0 引数は `[:tuple]` を使います。通常のベクタースキーマは常に 1 引数として渡されます。`for-all` は束縛が 1 個なら単項、複数ならタプル、空なら 0 引数の `check` を組み立てます。

レポートは成功時も失敗時も、次の 8 フィールドを必ず持ちます。

- `:pass?`: 成否
- `:iterations`: 成功時は実行回数、失敗時は反例が見つかった回数
- `:seed`: 使用したシード
- `:failing-input`: 最初の反例。成功時は `nil`
- `:shrunk-input`: 縮小後の反例。成功時は `nil`
- `:shrink-steps`: 採用した縮小候補のベクター
- `:failure`: 失敗内容。成功時は `nil`
- `:explain`: 現在は `nil`

`:failure` は `:kind`, `:message`, `:value`, `:exception` を持ちます。`:kind` は `:return-false` / `:return-nil` / `:threw` のいずれかです。

テストへ登録する場合は `doggo.test` の `defproperty` を使い、既存のレポートを検査する場合は `is-check-pass` を使います。`run-tests` / `test-vars` がレポート処理関数へ渡す各ケースには `:kind` があり、`defproperty` のケースは `:property` です。集計結果の `:property` はプロパティのケース数です。

#### 縮小

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `shrink` | `schema value` | スキーマを満たす、より単純な候補の遅延シーケンス | 値が不適合、スキーマ不正、縮小不能な形式はエラー |
| `shrink-seq` | `schema value n` | 最初の最大 `n` 候補のベクター | `n` が負数・整数範囲外、値が不適合、縮小不能ならエラー |

数値、文字列、真偽値、`maybe`、タプル、ベクター、リスト、セット、マップ、`map-of`、`or`、`enum` を縮小できます。入れ子では子要素も縮小し、マップの必須キーと全候補のスキーマ適合を保ちます。`[:predicate ...]` と `[:and ...]` は現在は縮小できません。

#### 関数の検査

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `instrument` | `callable contract`、`symbol contract`、任意で空の `options` | 検査付き関数。シンボル指定時は現在の定義も置換 | `contract` は `:args` または `:ret` が必要。`:args` は `[:tuple ...]`。違反時は構造化エラー |
| `unstrument` | 任意の値、または現在の名前空間の未修飾シンボル | 検査付き関数なら元の関数、それ以外の非シンボルは同じ値 | シンボル指定時は定義を元へ戻す。修飾済み、または現在の名前空間のグローバル定義を指さないシンボルはエラー |

`contract` は `{:args [:tuple ...] :ret schema}` です。どちらか一方だけでも指定できます。引数違反の例外データは `:reason` が `"instrumented args do not satisfy contract"`、戻り値違反は `"instrumented return value does not satisfy contract"` になり、`:contract`, `:target`, `:actual`, `:problems` を含みます。シンボル指定は現在の名前空間にある未修飾のグローバル定義だけを受け付けます。

### 例

```doggo
(require '[doggo.schema :as schema])

(println [(schema/valid? [:vector :int] [1 2 3])
          (schema/valid? [:vector :int] [1 "x"])
          (schema/sample [:enum :a :b] 3 {:seed 7})
          (schema/check-pass?
           (schema/check (fn [] true)
                         [:tuple]
                         {:seed 7 :iterations 1}))])
```

```console
$ doggo exec -- example.dog
```

```text
[true false [:a :a :b] true]
```
