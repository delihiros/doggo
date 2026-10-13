// Package stm はSTM（Software Transactional Memory）を実装する
package stm

import "errors"

// STMエラー定義
var (
	// ErrRetry はトランザクションのリトライが必要なことを示す
	ErrRetry = errors.New("トランザクションのリトライが必要")

	// ErrValidatorRejected はバリデータが値を拒否したことを示す
	ErrValidatorRejected = errors.New("バリデータが値を拒否")

	// ErrMaxRetriesExceeded は最大リトライ回数を超えたことを示す
	ErrMaxRetriesExceeded = errors.New("最大リトライ回数を超過")
)
