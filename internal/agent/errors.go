package agent

import "errors"

// エラー定義
var (
	ErrValidatorRejected = errors.New("validator rejected value")
	ErrAgentFailed       = errors.New("agent in error state")
	ErrAgentClosed       = errors.New("agent closed")
	ErrAwaitTimeout      = errors.New("await timeout")
	ErrAwaitInAction     = errors.New("await cannot be called from an agent action")
)
