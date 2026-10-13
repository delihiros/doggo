package vm

import (
	"testing"
)

// BenchmarkPipelineVsTransduce compares pipeline performance with transduce
func BenchmarkPipelineVsTransduce(b *testing.B) {
	// パイプライン版
	b.Run("pipeline_map_filter_reduce", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			runner := MustNewRunner()
			_, err := runner.Run(`(->> (range 1000) (map inc) (filter even?) (reduce +))`)
			if err != nil {
				b.Fatalf("error: %v", err)
			}
		}
	})

	// トランスデューサ版
	b.Run("transduce_map_filter_reduce", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			runner := MustNewRunner()
			_, err := runner.Run(`(transduce (comp (map inc) (filter even?)) + 0 (range 1000))`)
			if err != nil {
				b.Fatalf("error: %v", err)
			}
		}
	})
}

// BenchmarkMapTransducer compares map performance
func BenchmarkMapTransducer(b *testing.B) {
	// 遅延シーケンス版
	b.Run("map_lazy_doall", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			runner := MustNewRunner()
			_, err := runner.Run(`(doall (map inc (range 1000)))`)
			if err != nil {
				b.Fatalf("error: %v", err)
			}
		}
	})

	// トランスデューサ版
	b.Run("map_transduce", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			runner := MustNewRunner()
			_, err := runner.Run(`(transduce (map inc) conj [] (range 1000))`)
			if err != nil {
				b.Fatalf("error: %v", err)
			}
		}
	})

	// into版
	b.Run("map_into", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			runner := MustNewRunner()
			_, err := runner.Run(`(into [] (map inc) (range 1000))`)
			if err != nil {
				b.Fatalf("error: %v", err)
			}
		}
	})
}

// BenchmarkFilterTransducer compares filter performance
func BenchmarkFilterTransducer(b *testing.B) {
	// 遅延シーケンス版
	b.Run("filter_lazy_doall", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			runner := MustNewRunner()
			_, err := runner.Run(`(doall (filter even? (range 1000)))`)
			if err != nil {
				b.Fatalf("error: %v", err)
			}
		}
	})

	// トランスデューサ版
	b.Run("filter_transduce", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			runner := MustNewRunner()
			_, err := runner.Run(`(transduce (filter even?) conj [] (range 1000))`)
			if err != nil {
				b.Fatalf("error: %v", err)
			}
		}
	})
}

// BenchmarkComplexPipeline compares complex pipeline performance
func BenchmarkComplexPipeline(b *testing.B) {
	// パイプライン版（複雑）
	b.Run("pipeline_complex", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			runner := MustNewRunner()
			_, err := runner.Run(`
				(->> (range 10000)
				     (map inc)
				     (filter even?)
				     (take 100)
				     (reduce +))
			`)
			if err != nil {
				b.Fatalf("error: %v", err)
			}
		}
	})

	// トランスデューサ版（複雑）
	b.Run("transduce_complex", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			runner := MustNewRunner()
			_, err := runner.Run(`
				(transduce 
				  (comp (map inc) (filter even?) (take 100)) 
				  + 0 
				  (range 10000))
			`)
			if err != nil {
				b.Fatalf("error: %v", err)
			}
		}
	})
}
