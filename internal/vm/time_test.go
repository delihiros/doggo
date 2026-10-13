package vm

import (
	"context"
	"strings"
	"testing"
	stdtime "time"
)

func TestTimeNamespaceNowUsesSharedInstantShape(t *testing.T) {
	oldNow := timeBuiltinNow
	t.Cleanup(func() {
		timeBuiltinNow = oldNow
	})
	timeBuiltinNow = func() stdtime.Time {
		return stdtime.Date(2024, 1, 2, 3, 4, 5, 123000000, stdtime.UTC)
	}

	runner := MustNewRunner()
	value, err := runner.Run(`
		(require '[doggo.time :as time])
		(time/now)
	`)
	if err != nil {
		t.Fatal(err)
	}
	if got := value.String(); got != `{:instant "2024-01-02T03:04:05.123Z", :epoch-millis 1704164645123, :offset-seconds 0}` {
		t.Fatalf("now = %s", got)
	}
}

func TestTimeNamespaceSleepConvertsDurations(t *testing.T) {
	oldSleep := timeBuiltinSleep
	t.Cleanup(func() {
		timeBuiltinSleep = oldSleep
	})

	var calls []stdtime.Duration
	timeBuiltinSleep = func(ctx context.Context, duration stdtime.Duration) error {
		calls = append(calls, duration)
		return nil
	}

	runner := MustNewRunner()
	value, err := runner.Run(`
		(require '[doggo.time :as time])
		[(time/sleep 0)
		 (time/sleep (time/millis 25))
		 (time/sleep (time/seconds 2))]
	`)
	if err != nil {
		t.Fatal(err)
	}
	if got := value.String(); got != `[nil nil nil]` {
		t.Fatalf("sleep results = %s", got)
	}

	want := []stdtime.Duration{0, 25 * stdtime.Millisecond, 2 * stdtime.Second}
	if len(calls) != len(want) {
		t.Fatalf("sleep call count = %d, want %d", len(calls), len(want))
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("sleep duration %d = %v, want %v", i, calls[i], want[i])
		}
	}
}

func TestTimeNamespaceSleepHonorsCanceledContext(t *testing.T) {
	runner := MustNewRunner()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner.SetContext(ctx)

	_, err := runner.Run(`
		(require '[doggo.time :as time])
		(time/sleep 10)
	`)
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("sleep error = %v", err)
	}
}
