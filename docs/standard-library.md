# 標準ライブラリ

標準ライブラリは、必要な名前空間を `require` して使います。

```doggo
(require '[doggo.json :as json])
```

詳しいページでは、名前空間ごとに公開 API を表で示します。表には API 名、引数、戻り値、副作用または主なエラー条件を載せ、代表例は別の節で示します。この文書と詳細ページには、現在公開されている API だけを載せています。

## 目的から探す

| やりたいこと | 名前空間 | 詳細 |
| --- | --- | --- |
| コレクションや文字列を扱う | `doggo.core`, `doggo.set`, `doggo.string` | [基本とデータ](stdlib/core-and-data.md) |
| データを変換・検証する | `doggo.datafy`, `doggo.schema`, `doggo.walk`, `doggo.zip` | [基本とデータ](stdlib/core-and-data.md) |
| 二つのデータを比較する | `doggo.data` | [基本とデータ](stdlib/core-and-data.md) |
| 数学の計算を行う | `doggo.math` | [基本とデータ](stdlib/core-and-data.md) |
| JSON、CSV、XML、EDN を扱う | `doggo.json`, `doggo.csv`, `doggo.xml`, `doggo.edn` | [形式と値](stdlib/formats-and-values.md) |
| 日時、URL、UUID を扱う | `doggo.instant`, `doggo.time`, `doggo.url`, `doggo.uuid` | [形式と値](stdlib/formats-and-values.md) |
| テンプレートに値を埋め込む | `doggo.template` | [形式と値](stdlib/formats-and-values.md) |
| ファイルや OS を扱う | `doggo.io`, `doggo.os` | [入出力とシステム](stdlib/io-and-system.md) |
| HTTP やプロセスを扱う | `doggo.http`, `doggo.process` | [入出力とシステム](stdlib/io-and-system.md) |
| プロジェクトや依存関係の情報を確認する | `doggo.basis` | [基本とデータ](stdlib/core-and-data.md) |
| コマンドラインのプログラムを作る | `doggo.cli` | [アプリケーションとツール](stdlib/application-and-tools.md) |
| データを読みやすく表示する | `doggo.pprint` | [アプリケーションとツール](stdlib/application-and-tools.md) |
| テスト、REPL、診断を行う | `doggo.test`, `doggo.repl`, `doggo.stacktrace`, `doggo.observability` | [アプリケーションとツール](stdlib/application-and-tools.md) |

## 基本とデータ

| 名前空間 | 用途 | 詳細 |
| --- | --- | --- |
| `doggo.basis` | 現在または初期の基盤から、プロジェクト、モジュール、依存関係、読み込みパス、生成先の情報を取得します。 | [基本とデータ](stdlib/core-and-data.md) |
| `doggo.core` | よく使う関数とマクロを追加します。 | [基本とデータ](stdlib/core-and-data.md) |
| `doggo.data` | `diff` で二つのデータの違いを比較します。 | [基本とデータ](stdlib/core-and-data.md) |
| `doggo.datafy` | 値をデータとして表現し、変換します。 | [基本とデータ](stdlib/core-and-data.md) |
| `doggo.math` | 数学的な計算を行います。 | [基本とデータ](stdlib/core-and-data.md) |
| `doggo.schema` | スキーマによる値の検証・変換と検証失敗の説明、テストデータ生成、プロパティベーステスト、失敗入力の縮小、関数呼び出しの検証を行います。 | [基本とデータ](stdlib/core-and-data.md) |
| `doggo.set` | 集合の比較、結合、差分を扱います。 | [基本とデータ](stdlib/core-and-data.md) |
| `doggo.string` | 文字列の検索、分割、置換を行います。 | [基本とデータ](stdlib/core-and-data.md) |
| `doggo.walk` | 入れ子のデータをたどりながら変換します。 | [基本とデータ](stdlib/core-and-data.md) |
| `doggo.zip` | 木構造を移動しながら更新します。 | [基本とデータ](stdlib/core-and-data.md) |

## 形式と値

| 名前空間 | 用途 | 詳細 |
| --- | --- | --- |
| `doggo.csv` | CSV の読み書きを行います。 | [形式と値](stdlib/formats-and-values.md) |
| `doggo.edn` | EDN の読み書きを行います。 | [形式と値](stdlib/formats-and-values.md) |
| `doggo.instant` | ISO 8601 形式のタイムスタンプ文字列を解析して、時点を表す値に変換します。 | [形式と値](stdlib/formats-and-values.md) |
| `doggo.json` | JSON の読み書きを行います。 | [形式と値](stdlib/formats-and-values.md) |
| `doggo.template` | テンプレートに値を埋め込みます。 | [形式と値](stdlib/formats-and-values.md) |
| `doggo.time` | 日時と期間の計算・表示を行います。 | [形式と値](stdlib/formats-and-values.md) |
| `doggo.url` | URL を解析・組み立てます。 | [形式と値](stdlib/formats-and-values.md) |
| `doggo.uuid` | UUID を作成・変換します。 | [形式と値](stdlib/formats-and-values.md) |
| `doggo.xml` | XML の読み書きを行います。 | [形式と値](stdlib/formats-and-values.md) |

## 入出力とシステム

| 名前空間 | 用途 | 詳細 |
| --- | --- | --- |
| `doggo.http` | HTTP リクエストを送信し、応答を受け取ります。 | [入出力とシステム](stdlib/io-and-system.md) |
| `doggo.io` | ファイル、ディレクトリ、入出力ストリームを扱います。 | [入出力とシステム](stdlib/io-and-system.md) |
| `doggo.os` | 環境変数や作業ディレクトリなど、OS と実行環境を扱います。 | [入出力とシステム](stdlib/io-and-system.md) |
| `doggo.process` | 外部プロセスを起動し、状態や出力を受け取ります。 | [入出力とシステム](stdlib/io-and-system.md) |

## アプリケーションとツール

| 名前空間 | 用途 | 詳細 |
| --- | --- | --- |
| `doggo.cli` | コマンドライン引数を解析し、プログラムの開始処理を作ります。 | [アプリケーションとツール](stdlib/application-and-tools.md) |
| `doggo.observability` | 実行中の値や動作を記録・観察します。 | [アプリケーションとツール](stdlib/application-and-tools.md) |
| `doggo.pprint` | データを読みやすい形で整形して表示します。 | [アプリケーションとツール](stdlib/application-and-tools.md) |
| `doggo.repl` | REPL で名前空間や定義を調べます。 | [アプリケーションとツール](stdlib/application-and-tools.md) |
| `doggo.stacktrace` | 例外が起きた場所と呼び出しの流れを確認します。 | [アプリケーションとツール](stdlib/application-and-tools.md) |
| `doggo.test` | テストを定義して実行します。 | [アプリケーションとツール](stdlib/application-and-tools.md) |
