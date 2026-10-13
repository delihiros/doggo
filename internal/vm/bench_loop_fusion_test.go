package vm

import (
	"testing"
)

// BenchmarkMapFilterReducePipeline はパイプライン処理のベンチマークです。
// 最適化前後でアロケーションと実行時間を比較します。
func BenchmarkMapFilterReducePipeline(b *testing.B) {
	runner := MustNewRunner()

	// 初期化コード（ベンチ対象外）
	_, err := runner.Run(`
		(def bench-data (vec (range 1 101)))
		(def double-fn (fn [x] (* x 2)))
		(def even-pred (fn [x] (= 0 (mod x 2))))
		(def sum-fn +)
	`)
	if err != nil {
		b.Fatalf("setup failed: %v", err)
	}

	// プリコンパイル（グローバルシンボルテーブルの肥大化を防ぐ）
	code, err := runner.Compile(`
		(->> bench-data
			(map double-fn)
			(filter even-pred)
			(reduce sum-fn 0))
	`)
	if err != nil {
		b.Fatalf("compile failed: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := runner.RunPrecompiled(code)
		if err != nil {
			b.Fatalf("bench failed: %v", err)
		}
	}
}

// BenchmarkMapReduce は map→reduce パイプラインのベンチマークです。
func BenchmarkMapReduce(b *testing.B) {
	runner := MustNewRunner()

	_, err := runner.Run(`
		(def bench-data (vec (range 1 101)))
	`)
	if err != nil {
		b.Fatalf("setup failed: %v", err)
	}

	// プリコンパイル
	code, err := runner.Compile(`
		(->> bench-data
			(map inc)
			(reduce + 0))
	`)
	if err != nil {
		b.Fatalf("compile failed: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := runner.RunPrecompiled(code)
		if err != nil {
			b.Fatalf("bench failed: %v", err)
		}
	}
}

// BenchmarkFilterReduce は filter→reduce パイプラインのベンチマークです。
func BenchmarkFilterReduce(b *testing.B) {
	runner := MustNewRunner()

	_, err := runner.Run(`
		(def bench-data (vec (range 1 101)))
	`)
	if err != nil {
		b.Fatalf("setup failed: %v", err)
	}

	// プリコンパイル
	code, err := runner.Compile(`
		(->> bench-data
			(filter odd?)
			(reduce + 0))
	`)
	if err != nil {
		b.Fatalf("compile failed: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := runner.RunPrecompiled(code)
		if err != nil {
			b.Fatalf("bench failed: %v", err)
		}
	}
}

// BenchmarkLargeCollection は大きなコレクションでのパイプライン処理です。
func BenchmarkLargeCollection(b *testing.B) {
	runner := MustNewRunner()

	_, err := runner.Run(`
		(def large-data (vec (range 1 1001)))
	`)
	if err != nil {
		b.Fatalf("setup failed: %v", err)
	}

	// プリコンパイル
	code, err := runner.Compile(`
		(->> large-data
			(map (fn [x] (* x x)))
			(filter even?)
			(reduce + 0))
	`)
	if err != nil {
		b.Fatalf("compile failed: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := runner.RunPrecompiled(code)
		if err != nil {
			b.Fatalf("bench failed: %v", err)
		}
	}
}

// BenchmarkManualFusedLoop は手動で融合したループとの比較用です。
// 最適化後はパイプライン版がこれと同等になるべき。
func BenchmarkManualFusedLoop(b *testing.B) {
	runner := MustNewRunner()

	_, err := runner.Run(`
		(def bench-data (vec (range 1 101)))
		; 手動で融合した版
		(def manual-fused
			(fn [coll]
				(loop [items coll
				       acc 0]
					(if (empty? items)
						acc
						(let [x (first items)
						      doubled (* x 2)]
							(if (= 0 (mod doubled 2))
								(recur (rest items) (+ acc doubled))
								(recur (rest items) acc)))))))
	`)
	if err != nil {
		b.Fatalf("setup failed: %v", err)
	}

	// プリコンパイル
	code, err := runner.Compile(`(manual-fused bench-data)`)
	if err != nil {
		b.Fatalf("compile failed: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := runner.RunPrecompiled(code)
		if err != nil {
			b.Fatalf("bench failed: %v", err)
		}
	}
}

// BenchmarkNoFusionBaseline は融合対象ではないケースのベースラインです。
func BenchmarkNoFusionBaseline(b *testing.B) {
	runner := MustNewRunner()

	_, err := runner.Run(`
		(def bench-data (vec (range 1 101)))
	`)
	if err != nil {
		b.Fatalf("setup failed: %v", err)
	}

	// プリコンパイル
	code, err := runner.Compile(`
		(->> bench-data
			(map inc)
			(take 50)
			(reduce + 0))
	`)
	if err != nil {
		b.Fatalf("compile failed: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := runner.RunPrecompiled(code)
		if err != nil {
			b.Fatalf("bench failed: %v", err)
		}
	}
}

// BenchmarkFusionComparison はループフュージョン有効/無効を比較するベンチマークです。
// 注意: map/filter/range ソースは ReduceDirect を持つためフュージョンがスキップされます。
// このベンチマークは、スキップ後のパフォーマンスを確認するためのものです。
func BenchmarkFusionComparison(b *testing.B) {
	b.Run("MapReduce/FusionEnabled", func(b *testing.B) {
		runner := MustNewRunner()
		// ループフュージョン有効（デフォルト）
		// ただし map ソースは ReduceDirect を持つためスキップされる

		_, err := runner.Run(`(def bench-data (vec (range 1 1001)))`)
		if err != nil {
			b.Fatalf("setup failed: %v", err)
		}

		code, err := runner.Compile(`
			(reduce + 0 (map (fn [x] (* x 2)) bench-data))
		`)
		if err != nil {
			b.Fatalf("compile failed: %v", err)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := runner.RunPrecompiled(code)
			if err != nil {
				b.Fatalf("bench failed: %v", err)
			}
		}
	})

	b.Run("MapReduce/FusionDisabled", func(b *testing.B) {
		runner := MustNewRunner()
		runner.DisableLoopFusion() // ループフュージョン無効

		_, err := runner.Run(`(def bench-data (vec (range 1 1001)))`)
		if err != nil {
			b.Fatalf("setup failed: %v", err)
		}

		code, err := runner.Compile(`
			(reduce + 0 (map (fn [x] (* x 2)) bench-data))
		`)
		if err != nil {
			b.Fatalf("compile failed: %v", err)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := runner.RunPrecompiled(code)
			if err != nil {
				b.Fatalf("bench failed: %v", err)
			}
		}
	})
}

// BenchmarkReduceDirectPerformance は ReduceDirect パスの性能を測定します。
//
// ReduceDirect の動作:
// - VMMappedSeq: 共有 map キャッシュを直接埋め、単純な算術クロージャはパターン実行する
// - RangeLazySeq: 数値範囲を直接反復する
//
// フュージョンをスキップする理由:
// - ReduceDirect が有効な場合: Go ネイティブループが使用され高速
// - ReduceDirect が無効な場合: Go の seq イテレーションが使用され、VM dispatch より高速
func BenchmarkReduceDirectPerformance(b *testing.B) {
	// VMMappedSeq.ReduceDirect: クロージャを使用した map
	b.Run("VMMappedSeq/MapReduce", func(b *testing.B) {
		runner := MustNewRunner()

		_, err := runner.Run(`(def bench-data (vec (range 1 1001)))`)
		if err != nil {
			b.Fatalf("setup failed: %v", err)
		}

		// クロージャ (fn [x] ...) を使用 → VMMappedSeq が作成され ReduceDirect が有効
		code, err := runner.Compile(`
			(reduce + 0 (map (fn [x] (* x 2)) bench-data))
		`)
		if err != nil {
			b.Fatalf("compile failed: %v", err)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := runner.RunPrecompiled(code)
			if err != nil {
				b.Fatalf("bench failed: %v", err)
			}
		}
	})

	// filter の遅延シーケンス + range ソース
	b.Run("LazySeq/FilterReduceRange", func(b *testing.B) {
		runner := MustNewRunner()

		code, err := runner.Compile(`
			(reduce + 0 (filter odd? (range 1000)))
		`)
		if err != nil {
			b.Fatalf("compile failed: %v", err)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := runner.RunPrecompiled(code)
			if err != nil {
				b.Fatalf("bench failed: %v", err)
			}
		}
	})

	// RangeLazySeq: 直接 range
	b.Run("RangeLazySeq/Reduce", func(b *testing.B) {
		runner := MustNewRunner()

		code, err := runner.Compile(`
			(reduce + 0 (range 1000))
		`)
		if err != nil {
			b.Fatalf("compile failed: %v", err)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := runner.RunPrecompiled(code)
			if err != nil {
				b.Fatalf("bench failed: %v", err)
			}
		}
	})

	// filter の遅延シーケンス + 非 range ソース
	b.Run("LazySeq/FilterReduceVector", func(b *testing.B) {
		runner := MustNewRunner()

		_, err := runner.Run(`(def bench-data (vec (range 1 1001)))`)
		if err != nil {
			b.Fatalf("setup failed: %v", err)
		}

		code, err := runner.Compile(`
			(reduce + 0 (filter odd? bench-data))
		`)
		if err != nil {
			b.Fatalf("compile failed: %v", err)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := runner.RunPrecompiled(code)
			if err != nil {
				b.Fatalf("bench failed: %v", err)
			}
		}
	})
}

// BenchmarkIndexBasedFusion は Vector リテラルソースに対するインデックスベースフュージョンを測定します。
// Phase 2 の成果として、Vector リテラルに対しては count/nth を使用したインデックスベースのループが生成されます。
func BenchmarkIndexBasedFusion(b *testing.B) {
	// 小さい Vector リテラル (10 要素)
	b.Run("VectorLiteral/MapReduce/Small", func(b *testing.B) {
		runner := MustNewRunner()

		// Vector リテラルを直接使用 → インデックスベースフュージョンが適用される
		code, err := runner.Compile(`
			(reduce + 0 (map (fn [x] (* x 2)) [1 2 3 4 5 6 7 8 9 10]))
		`)
		if err != nil {
			b.Fatalf("compile failed: %v", err)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := runner.RunPrecompiled(code)
			if err != nil {
				b.Fatalf("bench failed: %v", err)
			}
		}
	})

	// Vector リテラル: filter のみ
	b.Run("VectorLiteral/FilterReduce/Small", func(b *testing.B) {
		runner := MustNewRunner()

		code, err := runner.Compile(`
			(reduce + 0 (filter odd? [1 2 3 4 5 6 7 8 9 10]))
		`)
		if err != nil {
			b.Fatalf("compile failed: %v", err)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := runner.RunPrecompiled(code)
			if err != nil {
				b.Fatalf("bench failed: %v", err)
			}
		}
	})

	// Vector リテラル: map + filter
	b.Run("VectorLiteral/MapFilterReduce/Small", func(b *testing.B) {
		runner := MustNewRunner()

		code, err := runner.Compile(`
			(reduce + 0 (filter odd? (map inc [1 2 3 4 5 6 7 8 9 10])))
		`)
		if err != nil {
			b.Fatalf("compile failed: %v", err)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := runner.RunPrecompiled(code)
			if err != nil {
				b.Fatalf("bench failed: %v", err)
			}
		}
	})

	// シンボルソース（比較用）: ReduceDirect パスを使用
	b.Run("SymbolSource/MapReduce/1K", func(b *testing.B) {
		runner := MustNewRunner()

		_, err := runner.Run(`(def bench-data (vec (range 1 1001)))`)
		if err != nil {
			b.Fatalf("setup failed: %v", err)
		}

		// シンボル参照 → インデックスベースフュージョンは適用されない
		// ReduceDirect パスを使用
		code, err := runner.Compile(`
			(reduce + 0 (map (fn [x] (* x 2)) bench-data))
		`)
		if err != nil {
			b.Fatalf("compile failed: %v", err)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := runner.RunPrecompiled(code)
			if err != nil {
				b.Fatalf("bench failed: %v", err)
			}
		}
	})
}
