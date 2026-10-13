package env

import (
	"fmt"
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
)

func TestEnvironmentGetSet(t *testing.T) {
	env := NewEnvironment()
	sym := types.NewSymbol("foo")
	val := atom.NewNumberInt(42)

	// 未定義のシンボルは取得できない
	_, ok := env.Get(sym)
	if ok {
		t.Error("Get() on undefined symbol returned true")
	}

	// 値を設定
	env.Set(sym, val)

	// 設定した値を取得できる
	result, ok := env.Get(sym)
	if !ok {
		t.Error("Get() on defined symbol returned false")
	}
	if result != val {
		t.Errorf("Get() = %v, want %v", result, val)
	}
}

func TestEnvironmentChild(t *testing.T) {
	parent := NewEnvironment()
	// グローバル変数を設定
	parent.Set(types.NewSymbol("parent"), atom.NewNumberInt(1))

	// 子環境を作成し、Extend でローカル変数を定義（Clojure スタイル）
	childEnv := parent.Child()
	child, ok := childEnv.(*LocalEnv)
	if !ok {
		t.Fatal("Child() did not return *LocalEnv")
	}
	child = child.Extend(types.NewSymbol("child"), atom.NewNumberInt(2))

	// 子スコープから親の値を取得できる
	val, ok := child.Get(types.NewSymbol("parent"))
	if !ok {
		t.Error("Child().Get() on parent symbol returned false")
	}
	if num, ok := val.(*atom.Number); !ok || num.IntVal == nil || *num.IntVal != 1 {
		t.Errorf("Child().Get() = %v, want 1", val)
	}

	// 子スコープの値を取得できる
	val, ok = child.Get(types.NewSymbol("child"))
	if !ok {
		t.Error("Child().Get() on child symbol returned false")
	}
	if num, ok := val.(*atom.Number); !ok || num.IntVal == nil || *num.IntVal != 2 {
		t.Errorf("Child().Get() = %v, want 2", val)
	}

	// 親スコープから子スコープの値は取得できない
	_, ok = parent.Get(types.NewSymbol("child"))
	if ok {
		t.Error("Parent.Get() on child symbol returned true")
	}
}

func TestEnvironmentShadowing(t *testing.T) {
	parent := NewEnvironment()
	// グローバル変数を設定
	parent.Set(types.NewSymbol("x"), atom.NewNumberInt(1))

	// 子環境を作成し、Extend でローカル変数を定義してシャドウイング（Clojure スタイル）
	childEnv := parent.Child()
	child, ok := childEnv.(*LocalEnv)
	if !ok {
		t.Fatal("Child() did not return *LocalEnv")
	}
	child = child.Extend(types.NewSymbol("x"), atom.NewNumberInt(2))

	// 子スコープでは子の値が優先される
	val, ok := child.Get(types.NewSymbol("x"))
	if !ok {
		t.Error("Child().Get() returned false")
	}
	if num, ok := val.(*atom.Number); !ok || num.IntVal == nil || *num.IntVal != 2 {
		t.Errorf("Child().Get() = %v, want 2", val)
	}

	// 親スコープの値は変更されていない
	val, ok = parent.Get(types.NewSymbol("x"))
	if !ok {
		t.Error("Parent.Get() returned false")
	}
	if num, ok := val.(*atom.Number); !ok || num.IntVal == nil || *num.IntVal != 1 {
		t.Errorf("Parent.Get() = %v, want 1", val)
	}
}

func TestEnvironmentConcurrency(t *testing.T) {
	env := NewEnvironment()
	done := make(chan bool)
	errors := make(chan error, 100)

	// 複数のgoroutineで同時に読み書き
	numGoroutines := 10
	numOps := 100

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer func() { done <- true }()
			for j := 0; j < numOps; j++ {
				sym := types.NewSymbol(fmt.Sprintf("var%d", id*numOps+j))
				val := atom.NewNumberInt(int64(id*numOps + j))

				// 書き込み
				env.Set(sym, val)

				// 読み込み
				result, ok := env.Get(sym)
				if !ok {
					errors <- fmt.Errorf("goroutine %d: Get() returned false for %s", id, sym)
					return
				}
				if result != val {
					errors <- fmt.Errorf("goroutine %d: Get() = %v, want %v", id, result, val)
					return
				}
			}
		}(i)
	}

	// すべてのgoroutineの完了を待つ
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// エラーをチェック
	close(errors)
	for err := range errors {
		t.Error(err)
	}
}

func TestGetSTMStore(t *testing.T) {
	t.Run("success with GlobalEnv", func(t *testing.T) {
		env := NewEnvironment()
		store, err := GetSTMStore(env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if store == nil {
			t.Error("expected non-nil store")
		}
	})

	t.Run("error when root is not GlobalEnv", func(t *testing.T) {
		// LocalEnv without Global
		env := &LocalEnv{
			Key:    "test",
			Val:    atom.NewNumberInt(1),
			Next:   nil,
			Global: nil, // no global env
		}
		_, err := GetSTMStore(env)
		if err == nil {
			t.Error("expected error when root is not GlobalEnv, got nil")
		}
		if err != ErrRootNotGlobalEnv {
			t.Errorf("expected ErrRootNotGlobalEnv, got %v", err)
		}
	})
}

func TestEnvironmentConcurrentChild(t *testing.T) {
	parent := NewEnvironment()
	parent.Set(types.NewSymbol("parent"), atom.NewNumberInt(1))

	done := make(chan bool)
	errors := make(chan error, 10)

	// 複数のgoroutineで同時に子環境を作成して使用
	numGoroutines := 5
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer func() { done <- true }()
			childEnv := parent.Child()
			child, ok := childEnv.(*LocalEnv)
			if !ok {
				errors <- fmt.Errorf("goroutine %d: Child() did not return *LocalEnv", id)
				return
			}
			sym := types.NewSymbol(fmt.Sprintf("child%d", id))
			val := atom.NewNumberInt(int64(id))

			// 子環境に Extend でローカル変数を定義（Clojure スタイル）
			child = child.Extend(sym, val)

			// 子環境から読み込み
			result, ok := child.Get(sym)
			if !ok {
				errors <- fmt.Errorf("goroutine %d: child.Get() returned false", id)
				return
			}
			if result != val {
				errors <- fmt.Errorf("goroutine %d: child.Get() = %v, want %v", id, result, val)
				return
			}

			// 親環境から読み込み
			parentVal, ok := child.Get(types.NewSymbol("parent"))
			if !ok {
				errors <- fmt.Errorf("goroutine %d: child.Get(parent) returned false", id)
				return
			}
			// 数値の比較
			if num, ok := parentVal.(*atom.Number); !ok || num.IntVal == nil || *num.IntVal != 1 {
				errors <- fmt.Errorf("goroutine %d: child.Get(parent) = %v, want 1", id, parentVal)
				return
			}
		}(i)
	}

	// すべてのgoroutineの完了を待つ
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// エラーをチェック
	close(errors)
	for err := range errors {
		t.Error(err)
	}
}
