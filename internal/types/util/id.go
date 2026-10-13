package util

import "sync/atomic"

// 一意なID生成用のカウンタ
var idCounter int64

// NextID は一意なIDを生成する
func NextID() int64 {
	return atomic.AddInt64(&idCounter, 1)
}
