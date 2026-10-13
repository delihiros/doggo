# 入出力とシステム

このページでは、ファイル、OS、外部プロセス、HTTP を扱う名前空間を説明します。名前空間の一覧は[標準ライブラリ](../standard-library.md)、基本構文は[言語ガイド](../language-guide.md)を参照してください。

これらの API は、サンドボックスやセキュリティ境界ではありません。Doggo を実行した利用者の通常の権限で、ファイル、ホストプロセスの環境、外部プロセス、ネットワークへアクセスします。受け取ったパス、コマンド、URL を信頼できるかどうかは、呼び出す側で確認してください。

以下の例はプロジェクトのルートで実行します。最初の例の前に一度 `doggo build` を実行し、構築した世代を `doggo exec --` で実行します。

## `doggo.io`

### 用途

パスの組み立て、ファイルの読み書き、ディレクトリの走査、一時ファイル、ファイル木の変更を扱います。パスの区切り方、アクセス権、シンボリックリンクを利用できるかどうかは、実行する OS とファイルシステムに依存します。

### 読み込み

次はコード断片です。

```doggo
(require '[doggo.io :as io])
```

### 公開 API

#### パス

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `join-path` | 1 個以上のパス部分 | OS に合わせて結合・整理したパス文字列 | ファイルには触れない。文字列でない部分はエラー |
| `normalize-relative-path` | 相対パス文字列 `path` | 整理した相対パス文字列 | ファイルには触れない。絶対パスはエラー |
| `as-url` | パスまたは URL の文字列 `path` | URL 文字列。通常のパスは絶対 `file` URL になる | ファイルには触れない。文字列でない場合や絶対パスへ変換できない場合はエラー |
| `basename` | パス文字列 `path` | 最後のパス要素 | ファイルには触れない。文字列でない場合はエラー |
| `dirname` | パス文字列 `path` | 親のパス | ファイルには触れない。文字列でない場合はエラー |
| `extension` | パス文字列 `path` | 先頭の `.` を除いた拡張子。なければ `nil` | ファイルには触れない。文字列でない場合はエラー |
| `normalize` | パス文字列 `path` | `.` や `..` を整理したパス | ファイルには触れない。文字列でない場合はエラー |
| `absolute?` | パス文字列 `path` | 絶対パスなら `true` | ファイルには触れない。文字列でない場合はエラー |
| `absolute` | パス文字列 `path` | 現在の作業ディレクトリを基準にした絶対パス | ファイルには触れない。文字列でない場合や変換失敗はエラー |
| `relative-to` | 基準パス `base`、対象パス `path` | `base` から `path` への相対パス | ファイルには触れない。絶対パスと相対パスの混在、異なるドライブなど互換性のない組み合わせはエラー |

#### 読み書き

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `reader-spec` | パス文字列 `path` | 文字列読み込み用の記述子 | まだ読み込まない。パスが文字列でなければエラー |
| `writer-spec` | パス文字列 `path`、省略可能な `{:append boolean}` | 文字列書き込み用の記述子 | まだ書き込まない。既定は上書き。オプション不正はエラー |
| `input-stream-spec` | パス文字列 `path` | バイナリ読み込み用の記述子 | まだ読み込まない。パスが文字列でなければエラー |
| `output-stream-spec` | パス文字列 `path`、省略可能な `{:append boolean}` | バイナリ書き込み用の記述子 | まだ書き込まない。既定は上書き。オプション不正はエラー |
| `slurp` | パスまたは読み込み用記述子 `source`、省略可能な `{:binary boolean}` | 文字列。バイナリなら 0〜255 の数値のベクター | 入力をすべて読む。存在しない・読めない入力、不正な UTF-8、記述子やオプションの不正はエラー。プロセス出力は 1 回だけ読める |
| `spit` | パスまたは書き込み用記述子 `target`、内容 `content`、省略可能な `{:binary boolean :append boolean}` | `nil` | 既定ではファイルを作成または上書きし、`:append true` では追記する。バイナリは 0〜255 の数値のベクターだけを受ける。書き込み失敗やオプション不正はエラー。プロセスの標準入力へ書く場合は書いた後に閉じる |
| `copy` | 読み込み元 `from`、書き込み先 `to` | `nil` | バイナリとして全内容を複製する。通常のパスは上書きし、追記用記述子なら追記する。読み書き失敗はエラー |

#### ファイルとディレクトリ

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `delete-entry` | パス文字列 `path`、省略可能な `{:silently boolean}` | `nil` | 1 個のファイルまたは空ディレクトリを削除する。既定では対象がない場合もエラー。`:silently true` は対象がない場合だけ無視する |
| `resource-path` | リソース名文字列 `name` | 解決したパス。見つからなければ `nil` | 絶対パスまたは live project load path から検索する。検証済み generation dependency は検索せず、そのパスを公開しない。引数不正はエラー |
| `exists?` | パス文字列 `path` | 存在すれば `true` | ファイルシステムを確認する。確認できない場合はエラー |
| `file?` | パス文字列 `path` | 存在し、ディレクトリでなければ `true` | ファイルシステムを確認する。対象がなければ `false`、確認できなければエラー |
| `directory?` | パス文字列 `path` | ディレクトリなら `true` | ファイルシステムを確認する。対象がなければ `false`、確認できなければエラー |
| `list-entries` | ディレクトリのパス `path` | 直下のパスを名前順に並べたベクター | ディレクトリを読む。対象がない、ディレクトリでない、読めない場合はエラー |
| `file-seq` | パス文字列 `path` | 根を含む深さ優先の遅延シーケンス | `walk` より簡単な互換用 API で、`file-seq` が基準となる最小機能。対象がなくてもそのパスを 1 要素として返し、読めないディレクトリの子は省く。ディレクトリへのシンボリックリンクをたどるため、参照先が根の外なら走査も根の外へ出る。追跡を無効にするオプションや循環防止はない。制御する場合は、既定でリンクをたどらず `:follow-symlinks` と `:max-depth` を指定できる `walk` を使う |
| `walk` | 根のパス `path`、省略可能な `{:order :pre\|:post :type :all\|:files\|:dirs :include-root boolean :max-depth non-negative-integer-or-nil :follow-symlinks boolean}` | 条件に合うパスの遅延シーケンス | 既定は根を含む行きがけ順。存在しない根はエラーで、遅延部分を必要とした時点の読み込み失敗もエラー。既定ではシンボリックリンクをたどらない。`:follow-symlinks true` はディレクトリへのリンクをたどり、循環では下降を止める |
| `glob` | パターン文字列 `pattern` | 一致するパスを名前順に並べたベクター | ファイルシステムを検索する。一致なしは `[]`、不正なパターンはエラー |
| `create-temp-file` | 省略可能な `{:prefix string :suffix string :dir string}` | 作成した空ファイルのパス | 一時ファイルを作成する。既定の親は OS の一時ディレクトリ。作成失敗やオプション不正はエラー。後始末は呼び出し側が行う |
| `create-temp-dir` | 省略可能な `{:prefix string :dir string}` | 作成したディレクトリのパス | 一時ディレクトリを作成する。既定の親は OS の一時ディレクトリ。作成失敗やオプション不正はエラー。後始末は呼び出し側が行う |
| `make-parents` | ファイルのパス `path` | `nil` | 親ディレクトリを必要な分だけ作成する。作成失敗はエラー |
| `mkdirs` | ディレクトリのパス `path` | 整理した対象パス | 対象と不足している親ディレクトリを作成する。既存ディレクトリでも成功し、作成失敗はエラー |
| `delete-tree` | パス `path`、省略可能な `{:missing :error\|:ignore}` | `nil` | ファイルまたはディレクトリ木を削除する。既定の `:missing :error` は対象なしをエラーにし、`:ignore` は無視する。シンボリックリンクはリンクだけを削除し、参照先は削除しない |
| `copy-tree` | コピー元 `src`、先 `dst`、省略可能な `{:missing :error\|:ignore :overwrite boolean}` | `nil` | ファイルまたはディレクトリ木を複製する。先が既存ディレクトリなら内容を併合し、衝突しない既存項目は残す。既定は対象なしと衝突をエラーにする。`:missing :ignore` は元がない場合を無視し、`:overwrite true` は衝突するファイルを置き換える。自分自身や自分の子へのコピー、形の違う衝突、木の中のシンボリックリンクはエラー。処理は非原子的で、途中の衝突、シンボリックリンク、入出力エラーで失敗しても、先に複製・上書きした内容は残る |
| `move` | 移動元 `src`、先 `dst`、省略可能な `{:missing :error\|:ignore :overwrite boolean}` | `nil` | `copy-tree` と同じ不足・上書き・併合方針で逐次移動する。各項目を先へコピーしてからその移動元を削除し、ディレクトリは子の処理後に削除する。自分自身や自分の子への移動、形の違う衝突、木の中のシンボリックリンクはエラー。処理は非原子的で、途中で失敗すると、先の部分変更と元の一部削除が残ることがある |

オプションを受け取る関数は、最後に 1 個のマップだけを受け取ります。追記は `:append`、元がない場合の扱いは `:missing`、衝突時の上書きは `:overwrite` で指定します。`delete-tree` に `:overwrite` を渡す、または `copy-tree` と `move` に `:follow-symlinks` を渡すなど、未対応のオプションはエラーです。木の変更はシンボリックリンクに慎重です。`delete-tree` はリンク自体だけを削除し、`copy-tree` と `move` はリンクを処理せずエラーにします。

### 例

一時ディレクトリだけを使い、最後に削除します。

```doggo
(require '[doggo.io :as io])

(let [dir (io/create-temp-dir {:prefix "doggo-docs-"})
      path (io/join-path dir "message.txt")]
  (try
    (do
      (io/spit path "こんにちは")
      (prn [(io/basename path) (io/slurp path)]))
    (finally
      (io/delete-tree dir {:missing :ignore}))))
```

```console
$ doggo build
$ doggo exec -- example.dog
```

```text
["message.txt" "こんにちは"]
```

## `doggo.os`

### 用途

環境変数と、作業ディレクトリ、ホームディレクトリ、ホスト名などの OS 情報を扱います。返るパス、OS 名、コマンドの検索結果は実行環境に依存します。

### 読み込み

次はコード断片です。

```doggo
(require '[doggo.os :as os])
```

### 公開 API

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `cwd` | なし | 現在の作業ディレクトリのパス | ホストから取得する。取得失敗はエラー |
| `env` | なし | 現在の環境変数を文字列キーと文字列値で持つマップ | ホストプロセスの環境を読み取った時点の値を返す |
| `getenv` | 環境変数名の文字列 `key` | 値の文字列。未設定なら `nil` | ホストプロセスの環境を読む。引数不正はエラー |
| `hostname` | なし | ホスト名の文字列 | OS から取得する。取得失敗はエラー |
| `os-name` | なし | `linux`、`windows` などの OS 名文字列 | 実行環境に依存する |
| `setenv` | 環境変数名 `key`、文字列値 `value` | `nil` | ホストプロセスの環境を設定する。名前・値の不正や OS の失敗はエラー |
| `system-temp-dir` | なし | OS の一時ディレクトリのパス | 情報を返すだけで、ディレクトリは作らない |
| `unsetenv` | 環境変数名の文字列 `key` | `nil` | ホストプロセスの環境から削除する。名前の不正や OS の失敗はエラー |
| `user-home` | なし | 利用者のホームディレクトリのパス | OS から取得する。取得失敗はエラー |
| `which` | コマンド名の文字列 `name` | 実行ファイルのパス。見つからなければ `nil` | ホストのコマンド検索パスを調べる。引数不正や検索失敗はエラー |

`setenv` と `unsetenv` は VM 内だけの環境マップではない値を変更します。同じホストプロセスに埋め込まれた他の処理にも見える、ホストプロセス全体への副作用です。実行ごとの設定には、可能なら明示的な引数や値を使ってください。

### 例

一時的な名前を使い、以前の値があれば最後に戻します。

```doggo
(require '[doggo.os :as os])

(let [key "DOGGO_DOCS_IO_SYSTEM_EXAMPLE"
      before (os/getenv key)]
  (try
    (do
      (os/setenv key "temporary")
      (prn (os/getenv key)))
    (finally
      (if (nil? before)
        (os/unsetenv key)
        (os/setenv key before)))))
```

```console
$ doggo exec -- example.dog
```

```text
"temporary"
```

## `doggo.process`

### 用途

外部プロセスを同期または非同期に起動し、標準入力・標準出力・標準エラー出力、終了状態、停止を扱います。利用できるコマンド、終了値、プロセス木の停止、パスとシグナルの動作は OS に依存します。任意のシグナルを選んで送る API はなく、停止には `kill-tree` を使います。

### 読み込み

次はコード断片です。

```doggo
(require '[doggo.process :as process])
```

### 公開 API

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `alive?` | プロセス `process` | 実行中なら `true` | 状態をすぐ確認する。不正なプロセス値はエラー |
| `close-stdin` | プロセス `process` | `nil` | パイプにした標準入力を閉じる。すでに閉じていれば成功し、閉じられない場合はエラー |
| `check-output` | コマンドと引数の文字列、最後に省略可能な `start` と同じオプション | 標準出力の文字列。標準出力を別の場所へ向けた場合は `nil` | 起動して終了まで待つ。標準エラー出力の既定は親へ引き継ぐ。終了値 0 以外は `:process-failure`、起動・入出力失敗もエラー |
| `exit-ref` | プロセス `process` | 参照値 | `@` で参照すると終了まで待って終了値を返す。不正なプロセス値はエラー |
| `exit-value` | プロセス `process` | 終了済みなら終了値、実行中なら `nil` | 状態をすぐ確認する。不正なプロセス値はエラー |
| `exited?` | プロセス `process` | 終了済みなら `true` | 状態をすぐ確認する。不正なプロセス値はエラー |
| `input-file-spec` | パス `path` | 標準入力へつなぐバイナリ読み込み記述子 | まだファイルを開かない。パス不正はエラー |
| `kill-tree` | プロセス `process` | 停止後の終了値。すでに終了していればその終了値 | 対応する OS では子孫を含むプロセス木を停止し、終了まで待つ。プロセス木の管理が使えない環境では明示的なエラー |
| `process-tree-supported?` | なし | プロセス木を管理できれば `true` | OS の対応状況を調べる。確認失敗はエラー |
| `run` | コマンドと引数の文字列、最後に省略可能な `{:dir path :in string :env map :timeout milliseconds-or-duration}` | `:exit`、`:out`、`:err` を持つマップ | 起動して終了まで待ち、出力を UTF-8 文字列として集める。終了値 0 以外でもマップを返す。時間切れは終了値 124。起動失敗、不正な出力やオプションはエラー |
| `start` | コマンドと引数の文字列、最後に省略可能な `{:dir path :env map :clear-env boolean :timeout milliseconds-or-duration :in input :out output :err output}` | プロセス | 非同期に起動する。既定では 3 入出力をパイプにする。コマンド不在、起動・入出力準備・オプションの失敗はエラー |
| `stderr` | プロセス `process` | 標準エラー出力の読み込み記述子 | `:err :pipe` の出力を `doggo.io/slurp` で 1 回だけ読める。読み込みは終了まで待つことがある。パイプでない場合や再読はエラー |
| `stdin` | プロセス `process` | 標準入力の書き込み記述子 | `:in :pipe` の入力を `doggo.io/spit` で 1 回書いて閉じられる。パイプでない場合や再書き込みはエラー |
| `stdout` | プロセス `process` | 標準出力の読み込み記述子 | `:out :pipe` の出力を `doggo.io/slurp` で 1 回だけ読める。読み込みは終了まで待つことがある。パイプでない場合や再読はエラー |
| `output-file-spec` | パス `path`、省略可能な `{:append boolean}` | 標準出力または標準エラー出力をつなぐバイナリ書き込み記述子 | まだファイルを開かない。実行時の既定は上書きで、`:append true` は追記。オプション不正はエラー |
| `wait-for` | プロセス `process`、ミリ秒数または `{:millis number}` | 時間内に終了すれば終了値、しなければ `nil` | 時間制限付き待機だけを行い、時間切れでもプロセスは停止しない。負数、有限でない数、大きすぎる値などはエラー |
| `write-stdin` | プロセス `process`、内容 `content` | `nil` | パイプにした標準入力へ書く。ベクターはバイト列、それ以外は文字列として扱う。閉じた入力、バイト範囲外、書き込み失敗はエラー |

`start` の `:in` は入力内容の文字列、バイトのベクター、`:pipe`、`:inherit`、`:closed`、または `input-file-spec`、`doggo.io/reader-spec`、`doggo.io/input-stream-spec` の記述子を受けます。`:out` は `:pipe`、`:inherit`、`:discard`、パスまたは `output-file-spec` の記述子、`:err` はそれらに加えて `:stdout` を受けます。`:env` は親の環境へ値を上書きし、`:clear-env true` では指定した値だけを子プロセスへ渡します。

`start` と `run` の `:timeout` は、時間切れ時にプロセス木を停止します。0 は時間制限を設けません。これに対して `wait-for` は状態を調べるための時間制限付き待機で、停止はしません。

### 例

次の例は短いコマンドだけを実行します。`sh` コマンドを持つ POSIX 系の環境向けです。他の OS では、その OS にある同等のコマンドへ置き換えてください。

```doggo
(require '[doggo.process :as process])

(let [result (process/run "sh" "-c" "printf doggo")]
  (prn [(:exit result) (:out result) (:err result)]))
```

```console
$ doggo exec -- example.dog
```

```text
[0 "doggo" ""]
```

## `doggo.http`

### 用途

同期 HTTP リクエストを送り、応答全体を受け取ります。呼び出し中は終了または時間切れまで待ちます。通信先、名前解決、証明書、経路、応答時間はネットワークと OS の設定に依存します。

### 読み込み

次はコード断片です。

```doggo
(require '[doggo.http :as http])
```

### 公開 API

| API | 引数 | 戻り値 | 副作用・主なエラー |
| --- | --- | --- | --- |
| `request` | HTTP または HTTPS の URL、省略可能な `{:method keyword :headers map :body string :timeout milliseconds-or-duration :max-body-bytes integer}` | `:status`、`:headers`、`:body`、最終 `:url` を持つマップ | ネットワークへ同期的に要求を送り、通信先の状態を変更することがある。URL、方式、ヘッダー、本文、オプションの不正は `:invalid-argument`。通信・時間切れ、応答上限超過、不正な UTF-8 応答は `:runtime-failure` |

既定は `:method :get`、30 秒の時間制限、8 MiB の応答本文上限です。`:timeout 0` は要求ごとの時間制限を設けません。`:method` は名前空間の付かないキーワードで指定します。`:headers` は文字列の名前から文字列または文字列ベクターへのマップです。`Content-Length`、`Transfer-Encoding`、`Trailer` は通信処理が管理するため指定できません。

応答の `:status` は整数、`:headers` は文字列の名前から文字列ベクターへのマップ、`:body` と `:url` は文字列です。転送先へ移動した場合、`:url` は最後の URL です。400 や 500 などの応答も例外にはせず、状態番号を含む応答マップとして返します。本文全体をメモリへ読み込むため、信頼できない通信先には適切な `:max-body-bytes` を指定してください。

### 例

この例は HTTP メソッドを文字列で指定した検証エラーを表示します。検証は通信より先に行われるため、外部ネットワークへ接続しません。

```doggo
(require '[doggo.http :as http])

(try
  (http/request "https://example.invalid" {:method "get"})
  (catch [e]
    (prn [(:kind (ex-data e)) (:argument (ex-data e))])))
```

```console
$ doggo exec -- example.dog
```

```text
[:invalid-argument :method]
```
