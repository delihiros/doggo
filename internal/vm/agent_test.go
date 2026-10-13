package vm

import (
	"strings"
	"testing"
	"time"
)

func TestAgentBasic(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "create agent",
			input:    `(agent? (agent 42))`,
			expected: "true",
		},
		{
			name:     "agent? false",
			input:    `(agent? 42)`,
			expected: "false",
		},
		{
			name:     "deref agent",
			input:    `@(agent 42)`,
			expected: "42",
		},
		{
			name:     "deref with deref function",
			input:    `(deref (agent "hello"))`,
			expected: `"hello"`,
		},
		{
			name:     "agent with map",
			input:    `@(agent {:count 0})`,
			expected: `{:count 0}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			assertRunResult(t, runner, tt.input, tt.expected)
		})
	}
}

func TestAgentSend(t *testing.T) {
	runner := MustNewRunner()

	// エージェントを作成してインクリメント
	code := `
		(def a (agent 0))
		(send a inc)
		; 少し待機してから値を取得
		a
	`
	result, err := runner.Run(code)
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}

	// エージェントが返ることを確認
	if !strings.HasPrefix(result.String(), "#<Agent@") {
		t.Errorf("expected agent, got %s", result.String())
	}

	// 少し待機してから値を確認
	time.Sleep(100 * time.Millisecond)

	result2, err := runner.Run("@a")
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}
	if result2.String() != "1" {
		t.Errorf("expected 1, got %s", result2.String())
	}
}

func TestAgentSendMultiple(t *testing.T) {
	runner := MustNewRunner()

	code := `
		(def counter (agent 0))
		(send counter inc)
		(send counter inc)
		(send counter inc)
		counter
	`
	_, err := runner.Run(code)
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}

	// 待機
	time.Sleep(100 * time.Millisecond)

	assertRunResult(t, runner, "@counter", "3")
}

func TestAgentSendWithArgs(t *testing.T) {
	runner := MustNewRunner()

	code := `
		(def a (agent 10))
		(send a + 5)
		a
	`
	_, err := runner.Run(code)
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}

	// 待機
	time.Sleep(100 * time.Millisecond)

	assertRunResult(t, runner, "@a", "15")
}

func TestAgentSendWithCustomFunction(t *testing.T) {
	runner := MustNewRunner()

	code := `
		(def a (agent 10))
		(send a (fn [x] (* x 2)))
		a
	`
	_, err := runner.Run(code)
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}

	// 待機
	time.Sleep(100 * time.Millisecond)

	assertRunResult(t, runner, "@a", "20")
}

func TestSendCapturesRuntimeBeforeVMRelease(t *testing.T) {
	runner := MustNewRunner()

	if _, err := runner.Run(`
		(def captured (agent 0))
		(send captured (fn [state] (count [state 1 2])))
		nil
	`); err != nil {
		t.Fatalf("runner error: %s", err)
	}

	result, err := runner.Run(`
		(do
		  (await-for 500 captured)
		  @captured)
	`)
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}
	if result.String() != "3" {
		t.Errorf("expected 3, got %s", result.String())
	}
}

func TestAgentError(t *testing.T) {
	runner := MustNewRunner()

	// エラーを起こすアクションを送信
	code := `
		(def a (agent 0))
		(agent-error a)
	`
	result, err := runner.Run(code)
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}
	// 最初はエラーなし
	if result.String() != "nil" {
		t.Errorf("expected nil, got %s", result.String())
	}
}

func TestAgentRestart(t *testing.T) {
	runner := MustNewRunner()

	code := `
		(def a (agent 10))
		(restart-agent a 100)
		@a
	`
	assertRunResult(t, runner, code, "100")
}

func TestAgentSetErrorMode(t *testing.T) {
	runner := MustNewRunner()

	// エラーモードを設定
	code := `
		(def a (agent 0))
		(set-error-mode! a :continue)
		(agent? a)
	`
	assertRunResult(t, runner, code, "true")
}

func TestAgentAwaitFor(t *testing.T) {
	t.Run("await blocks until queued actions finish", func(t *testing.T) {
		runner := MustNewRunner()
		result, err := runner.Run(`
			(require '[doggo.time :as time])
			(def a (agent 0))
			(send a (fn [state] (time/sleep 50) (inc state)))
			(await a)
			@a
		`)
		if err != nil {
			t.Fatalf("runner error: %s", err)
		}
		if result.String() != "1" {
			t.Fatalf("expected completed action state 1, got %s", result.String())
		}
	})

	t.Run("await-for reports timeout before queued actions finish", func(t *testing.T) {
		runner := MustNewRunner()
		result, err := runner.Run(`
			(require '[doggo.time :as time])
			(def a (agent 0))
			(send a (fn [state] (time/sleep 50) (inc state)))
			[(await-for 1 a) @a]
		`)
		if err != nil {
			t.Fatalf("runner error: %s", err)
		}
		if result.String() != "[false 0]" {
			t.Fatalf("expected timeout with unchanged state, got %s", result.String())
		}
	})
}

func TestAgentAwaitRejectsAgentAction(t *testing.T) {
	runner := MustNewRunner()
	done := make(chan error, 1)
	go func() {
		_, err := runner.Run(`
			(def a (agent 0))
			(send a (fn [state] (await a) (inc state)))
			(await a)
		`)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "await cannot be called from an agent action") {
			t.Fatalf("expected agent-action await error, got %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("await deadlocked inside an agent action")
	}
}

func TestAgentAwaitRejectsAgentWatcher(t *testing.T) {
	runner := MustNewRunner()
	done := make(chan error, 1)
	go func() {
		_, err := runner.Run(`
			(def a (agent 0))
			(add-watch a :self (fn [_ _ _ _] (await a)))
			(send a inc)
			(await a)
		`)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "await cannot be called from an agent action") {
			t.Fatalf("expected agent-watcher await error, got %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("await deadlocked inside an agent watcher")
	}
}

func TestAgentIdentityActionWithMetadataWrappedInfiniteRangeTerminates(t *testing.T) {
	runner := MustNewRunner()
	result, err := runner.Run(`
		(def a (agent (with-meta (range) {})))
		(send a identity)
		(await-for 100 a)
	`)
	if err != nil {
		t.Fatalf("agent identity action failed: %v", err)
	}
	if got := result.String(); got != "true" {
		t.Fatalf("agent identity action did not finish: %s", got)
	}
}

func TestAgentWithMap(t *testing.T) {
	runner := MustNewRunner()

	code := `
		(def state (agent {:count 0 :name "test"}))
		(send state (fn [s] (assoc s :count (inc (get s :count)))))
		state
	`
	_, err := runner.Run(code)
	if err != nil {
		t.Fatalf("runner error: %s", err)
	}

	// 待機
	time.Sleep(100 * time.Millisecond)

	assertRunResult(t, runner, "(get @state :count)", "1")
}

func TestAgentErrorHandling(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectError string
	}{
		{
			name:        "send with non-agent",
			input:       `(send 42 inc)`,
			expectError: "must be an agent",
		},
		{
			name:        "restart-agent with non-agent",
			input:       `(restart-agent 42 0)`,
			expectError: "must be an agent",
		},
		{
			name:        "set-error-mode! with non-agent",
			input:       `(set-error-mode! 42 :fail)`,
			expectError: "must be an agent",
		},
		{
			name:        "set-error-mode! with invalid mode",
			input:       `(set-error-mode! (agent 0) :invalid)`,
			expectError: "must be :fail or :continue",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			_, err := runner.Run(tt.input)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.expectError)
			}
			if !strings.Contains(err.Error(), tt.expectError) {
				t.Errorf("expected error containing %q, got %q", tt.expectError, err.Error())
			}
		})
	}
}

// TestAgentChainRelay はAgentチェーンでのメッセージ転送をテストする。
// これは tutorial.dog の Agent リング例を再現し、VMの定数プールが
// 失われることによるパニック(index out of range)を検出する。
func TestAgentChainRelay(t *testing.T) {
	runner := MustNewRunner()

	// Agentチェーンを作成してメッセージを転送する
	// relay関数内で別のAgentにsendを呼び出す
	code := `
		; relay関数: メッセージを次のAgentに転送
		(defn relay [state msg]
		  (when (:next state)
		    (send (:next state) relay msg))
		  (when (:done-ch state)
		    (>! (:done-ch state) msg))
		  state)

		; チェーンを作成する関数
		(defn create-agent-chain [n done-ch]
		  (let [tail (agent {:done-ch done-ch})]
		    (loop [next tail
		           count (dec n)]
		      (if (<= count 0)
		        next
		        (recur (agent {:next next}) (dec count))))))

		; 5つのAgentチェーンを作成
		(def chain-done (chan))
		(def chain-head (create-agent-chain 5 chain-done))

		; メッセージを送信
		(send chain-head relay "Hello through chain!")

		; 結果を受信
		(<! chain-done)
	`
	result, err := runner.Run(code)
	if err != nil {
		t.Fatalf("agent chain relay failed: %s", err)
	}

	if result.String() != `"Hello through chain!"` {
		t.Errorf("expected \"Hello through chain!\", got %s", result.String())
	}
}
