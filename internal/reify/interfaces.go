package reify

// このファイルはインターフェース実装を生成するインターフェースの参照を保持します。
// 実際の生成はgo:generateディレクティブ経由で行われます。
//
// 新しいインターフェースを追加するには:
// 1. このファイルにインポートを追加
// 2. generate.goのgo:generateディレクティブにインターフェース名を追加
// 3. go generate ./internal/reify を実行

import (
	// インターフェース実装生成対象のパッケージをインポート
	// これにより依存関係が解決されます
	_ "net/http"
)

// 追加のインターフェースを登録する場合は、generate.goのgo:generateディレクティブに追加してください。
// 形式: package/path.InterfaceName
// 例:
//   - fmt.Stringer
//   - net/http.Handler
