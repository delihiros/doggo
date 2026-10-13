# Go 連携

このガイドは [開発ツール](tooling.md) の依存関係とビルドを使える人向けです。最初に公開された Go モジュールを追加し、次にローカルの Go モジュールを使って関数、メソッド、インターフェースの連携を確認します。

## 1. 公開された Go モジュールを追加する

一例として、Ebitengine を依存関係に追加して同期するには、Doggo プロジェクトのルートで次を実行します。

```bash
doggo deps add github.com/hajimehoshi/ebiten/v2@v2.9.9
doggo mod sync
```

同期後、Doggo のソースファイルでは次のように import します。

```doggo
(import "github.com/hajimehoshi/ebiten/v2")
```

`github.com/hajimehoshi/ebiten/v2` は Go モジュールパスです。`v2.9.9` は解決するモジュールの正確なバージョンで、ソースではバージョンサフィックスを付けずに同じモジュールパスを `import` します。OS ごとの準備は [Ebitengine の公式インストール手順](https://ebitengine.org/ja/documents/install.html) に従ってください。

ウィンドウを表示するプログラムは、[Ebitengine で画面を表示する](ebitengine-tutorial.md) で作成します。

## 2. ローカルの Go モジュールを追加する

この例では、共通の `workspace` ディレクトリの下にある Doggo プロジェクト `doggo-app` と、その隣の Go モジュール `greeter` を使います。

次の構成にします。

```text
workspace/
├── doggo-app/
│   ├── doggo.mod
│   └── ...
└── greeter/
    ├── go.mod
    └── greeter.go
```

Doggo プロジェクトのルート `workspace/doggo-app` で、隣の Go モジュールを作成します。

```bash
mkdir ../greeter
cd ../greeter
go mod init example.com/greeter
```

`go mod init` により `go.mod` が作成されます。続けて `greeter.go` を作成します。

```go
// ../greeter/greeter.go
package greeter

type Greeter struct {
	Prefix string
}

func Hello(name string) string {
	return "Hello, " + name
}

func NewGreeter(prefix string) *Greeter {
	return &Greeter{Prefix: prefix}
}

func (g *Greeter) Greet(name string) string {
	return g.Prefix + name
}

type Formatter interface {
	Format(string) string
}

func Apply(formatter Formatter, value string) string {
	return formatter.Format(value)
}
```

Doggo プロジェクトへ戻り、依存関係を追加します。

```bash
cd ../doggo-app
doggo deps add ../greeter
```

`../greeter` は、`doggo.mod` がある Doggo プロジェクトのディレクトリを基準に解決されます。成功すると `doggo.mod` の `:deps` に `../greeter` が加わります。このコマンド自体は通常、何も表示しません。

```bash
doggo deps list
```

`doggo deps list` に `../greeter` が表示されれば、追加を確認できます。失敗したら、`doggo.mod` がある `workspace/doggo-app` で実行しているか確認します。

## 3. doggo.mod と doggo.lock の役割を分ける

```doggo
;; doggo.mod の一部
:deps ["github.com/hajimehoshi/ebiten/v2@v2.9.9"
       "../greeter"]
```

`doggo.mod` はモジュール名、版、ライセンス、対応する Doggo の版、依存元を記録します。依存元を追加または削除するときに変わるファイルです。

ここでは、新しく追加した `greeter` が `doggo.mod` に記録されていますが、`doggo.lock` は最後に同期した時点の内容を反映しています。セクション 4 で同期すると、`greeter` のパッケージとインターフェースも `doggo.lock` に追加されます。

```doggo
;; doggo.lock の一部
:generation "<64桁の generation digest>"
:scan {:go-packages ["github.com/hajimehoshi/ebiten/v2"]
       :reify-targets [{:source "github.com/hajimehoshi/ebiten/v2.Game"
                        :runtime "github.com/hajimehoshi/ebiten/v2.Game"}]}
:build {:binary "" :sha256 ""}
```

実際の二つのベクターには、この例の項目に加えて、Doggo が標準で登録するパッケージと `reify` 対象インターフェースも並びます。

`doggo.lock` は解決済みの依存関係、検出した Go パッケージと型同一性を持つ reify target、ビルド結果を一つの immutable generation として固定します。各依存関係の `:install-dir` はその generation を基準にした `deps/<dependency-key>` です。上のように `:binary` と `:sha256` が空なら sync-only generation、両方に値があれば built generation です。これはコマンドが生成するファイルなので、手で編集しません。

## 4. 依存関係と生成コードを同期する

```bash
doggo mod sync
```

成功すると、依存関係の snapshot、検出した Go パッケージと公開インターフェース、生成した連携コードを `.doggo/generations/<generation>/` に sync-only generation として一括公開し、最後に `doggo.lock` を切り替えます。実行ファイルはまだビルドしません。

失敗時は `sync stage 段階名:` に続く原因を最初に確認します。`../greeter/go.mod` の `module` が `example.com/greeter` であることと、Go コードがビルドできることも確認します。

## 5. パッケージを import して関数とコンストラクターを呼ぶ

```doggo
(import "example.com/greeter")

(println (greeter/Hello "Doggo"))

(def greeter
  (greeter/NewGreeter "Hi, "))
```

このコードを `interop.dog` に保存し、built generation で動かします。

```bash
doggo build
doggo exec -- interop.dog
```

実行結果は次のとおりです。

```text
"Hello, Doggo"
```

`import` の引数は文字列リテラルです。パッケージ名にはパスの最後の要素 `greeter` を使います。公開関数は `greeter/Hello` の形で呼び出します。Go のコンストラクターも公開関数なので、`greeter/NewGreeter` の形で呼び出します。

`import: package not found` や未定義シンボルのエラーが出たら、`doggo mod sync` 後の `doggo.lock` の `:go-packages` と、Go 側の名前が公開されているかを確認します。

## 6. メソッドを呼び出す

```doggo
(import "example.com/greeter")

(def greeter
  (greeter/NewGreeter "Hi, "))

(println (. greeter Greet "Doggo"))
```

このコードを `method.dog` に保存して `doggo exec -- method.dog` を実行すると、結果は `"Hi, Doggo"` です。`.` の次に Go の公開メソッド名を置き、その後に引数を並べます。引数の数や型が合わない場合は通常の Doggo エラーになるため、最初にメソッド名と引数を確認します。

## 7. reify でインターフェースを実装する

```doggo
(import "example.com/greeter")

(def formatter
  (reify greeter/Formatter
    {:format (fn [value]
        (str "[" value "]"))}))

(println (greeter/Apply formatter "Doggo"))
```

このコードを `reify.dog` に保存して `doggo exec -- reify.dog` を実行すると、結果は `"[Doggo]"` です。`reify` はインポートしたインターフェース型シンボルと、メソッドを実装するマップを受け取ります。`Format` メソッドには `:format` または `:Format` を使えます。

`no reify factory registered` が出たら、`doggo mod sync` 後の `doggo.lock` にある `:scan :reify-targets` を確認します。各 target の `:source` は import する型名、`:runtime` は Go の型同一性を固定する canonical identity です。マップで指定する値は関数で、引数の数は Go メソッドと合わせます。

## 8. ビルドで生成コードを使う

```bash
doggo build
```

成功すると、現在の sync-only または built generation を staging に複製して実行ファイルを追加し、content-addressed な built generation を確定します。`doggo.lock` の `:build :binary` には Unix 系では `.doggo/generations/<generation>/bin/doggo`、Windows では `.doggo/generations/<generation>/bin/doggo.exe`、`:sha256` には digest を記録します。既存 generation は変更しません。

generation 内の toolchain workspace には、パッケージの公開項目を登録する `internal/interop/generated.go` と、`reify` 用の実装を登録する `internal/reify/generated_reify.go` が含まれます。どちらも手で編集しません。

`doggo build` はパッケージを再検出しません。Go の依存関係や `import`、対象インターフェースを変えた後は、先に `doggo mod sync` を実行します。generation の欠落、symlink、digest 不一致は `doggo.lock is stale or corrupt` として拒否され、読み込み時には修復されません。入力を同期し直すには `doggo mod sync`、検証済みの sync-only generation に実行ファイルを加えるには `doggo build` を明示的に実行します。

## 9. Go 連携の対応範囲

```doggo
(import "example.com/greeter/subpkg")
```

同期時に登録されていないパッケージなら `import: package not found` になります。Go 連携は任意の Go 構文を Doggo に埋め込む機能ではありません。ここで確認した対象は、検出されたパッケージの公開関数とコンストラクター、返された Go の値にある公開メソッドと公開フィールド、生成されたインターフェースです。値を変換できない場合や整数が範囲外の場合はエラーになります。

## 10. パニックの扱い

Doggo の Go 連携を通じた関数またはメソッド呼び出しで、その呼び出しと同じゴルーチン上で起きたパニックは Doggo のエラーとして返されます。パッケージ初期化中や別のゴルーチンで起きたパニックは、この処理の対象ではありません。

## 11. 実行時の権限と安全上の注意

Go 連携と、プロセス、ファイルシステム、ネットワークの操作は、実行した利用者の通常の権限で動きます。Doggo はサンドボックスではなく、セキュリティ境界ではありません。信頼できないコードや依存関係を、隔離されたものとして実行しないでください。
