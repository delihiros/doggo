// Package coll はClojure互換のコレクション型を提供します。
//
// # 主要な型
//
// このパッケージは以下のコレクション型を提供します:
//
//   - [List]: 不変リスト（Clojure list）。先頭への追加は O(n) です。
//   - [Vector]: 不変ベクター（Clojure vector）。インデックスアクセスと末尾追加が効率的です。
//   - [Map]: 不変ハッシュマップ（Clojure hash-map）。ハッシュバケットによる O(1) ルックアップ。
//   - [Set]: 不変セット（Clojure hash-set）。ハッシュバケットによる O(1) ルックアップ。
//   - [LazySeq]: 遅延シーケンス。必要になるまで要素を計算しません。
//
// # 共通インターフェース
//
// すべてのコレクションは [types.Value] インターフェースを実装し、
// 値ベースの等価性（Equals）とハッシュ（Hash）を提供します。
//
// また、多くのコレクションは以下のインターフェースを実装します:
//
//   - types.Seq: シーケンス操作（First, Rest, Next）
//   - types.IPersistentCollection: 不変コレクション操作（Count, Conj, Seq）
//   - types.Indexed: インデックスアクセス（Nth）
//
// # 不変性
//
// 永続コレクションは不変（immutable）です。
// Conj, Assoc, Disj などの操作は元のコレクションを変更せず、
// 新しいコレクションを返します。これにより、スレッドセーフな操作が可能です。
//
// # トランジェント
//
// バッチ更新のパフォーマンスを向上させるため、[TransientMap] を提供します。
// これは一時的に可変な状態を持ち、persistent! で不変マップに戻せます。
//
// # 遅延シーケンス
//
// [LazySeq] は遅延評価されるシーケンスです。
// [RangeLazySeq] など、特定の操作に最適化された実装も提供します。
//
// # 例
//
//	// ベクターの作成と操作
//	v := coll.NewVector(atom.NewNumberInt(1), atom.NewNumberInt(2))
//	v2 := v.Conj(atom.NewNumberInt(3)) // [1 2 3]
//
//	// マップの作成と操作
//	m := coll.NewMap(
//	    atom.NewKeyword("a"), atom.NewNumberInt(1),
//	    atom.NewKeyword("b"), atom.NewNumberInt(2),
//	)
//	val := m.Get(atom.NewKeyword("a")) // 1
//
//	// セットの作成と操作
//	s := coll.NewSet(atom.NewKeyword("x"), atom.NewKeyword("y"))
//	contains := s.Contains(atom.NewKeyword("x")) // true
package coll
