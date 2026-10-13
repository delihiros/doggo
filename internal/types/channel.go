package types

import (
	"fmt"

	"doggo/internal/types/util"
)

// Channel は Go のチャネルをラップします。
// goroutine 間で値を安全にやり取りするために使用されます。
type Channel struct {
	Value chan Value
}

func (c *Channel) String() string {
	return fmt.Sprintf("#<Channel %p>", c.Value)
}

func (c *Channel) Type() string {
	return "channel"
}

func (c *Channel) Hash() uint32 {
	// チャネルはポインタアドレスをハッシュとして使用
	return util.HashString(fmt.Sprintf("%p", c.Value))
}

func (c *Channel) Equals(other Value) bool {
	if other == nil {
		return false
	}
	if o, ok := other.(*Channel); ok {
		return c.Value == o.Value
	}
	return false
}

// NewChannel は新しいチャネルを作成します。
// buffer はバッファサイズを指定します（0 の場合は unbuffered channel）。
func NewChannel(buffer int) (channel *Channel, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			channel = nil
			err = fmt.Errorf("invalid channel buffer size: %v", recovered)
		}
	}()
	return &Channel{Value: make(chan Value, buffer)}, nil
}

// Send はチャネルに値を送信します。
// ブロッキング操作です。
func (c *Channel) Send(val Value) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("%v", recovered)
		}
	}()
	c.Value <- val
	return nil
}

// Receive はチャネルから値を受信します。
// ブロッキング操作です。チャネルが閉じられている場合は (nil, false) を返します。
func (c *Channel) Receive() (Value, bool) {
	val, ok := <-c.Value
	return val, ok
}

// TryReceive はチャネルから値を非ブロッキングで受信を試みます。
// 値が利用可能な場合は (value, true, true) を返します。
// チャネルが閉じられている場合は (nil, false, true) を返します。
// 値が利用不可能な場合は (nil, false, false) を返します。
func (c *Channel) TryReceive() (Value, bool, bool) {
	select {
	case val, ok := <-c.Value:
		return val, ok, true
	default:
		return nil, false, false
	}
}

// Close はチャネルを閉じます。
func (c *Channel) Close() (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("%v", recovered)
		}
	}()
	close(c.Value)
	return nil
}

// IsSelfEvaluating は自己評価型かどうかを返します。
func (c *Channel) IsSelfEvaluating() bool {
	return true
}
