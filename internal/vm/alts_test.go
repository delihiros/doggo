package vm

import (
	"strings"
	"testing"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

func TestAltsReceivesFromReadyChannel(t *testing.T) {
	runner := MustNewRunner()

	result, err := runner.Run(`
		(let [ch (chan 1)
		      res (do
		            (>! ch 42)
		            (alts! [ch]))]
		  (and (= 42 (first res))
		       (chan? (second res))))
	`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.String() != "true" {
		t.Fatalf("expected true, got %s", result.String())
	}
}

func TestAltsClosedChannelReturnsNilAndChannel(t *testing.T) {
	runner := MustNewRunner()

	result, err := runner.Run(`
		(let [ch (chan)
		      res (do
		            (close! ch)
		            (alts! [ch]))]
		  (and (nil? (first res))
		       (chan? (second res))))
	`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.String() != "true" {
		t.Fatalf("expected true, got %s", result.String())
	}
}

func TestAltsDefaultWhenNoReadyChannels(t *testing.T) {
	runner := MustNewRunner()

	result, err := runner.Run(`
		(let [ch (chan)]
		  (alts! [ch] {:default :none}))
	`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.String() != "[:none :default]" {
		t.Fatalf("expected [:none :default], got %s", result.String())
	}
}

func TestAltsDefaultUsesReadyChannelWhenAvailable(t *testing.T) {
	runner := MustNewRunner()

	result, err := runner.Run(`
		(let [ch (chan 1)
		      res (do
		            (>! ch :ready)
		            (alts! [ch] {:default :none}))]
		  (and (= :ready (first res))
		       (chan? (second res))))
	`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.String() != "true" {
		t.Fatalf("expected true, got %s", result.String())
	}
}

func TestAltsTimeoutWhenNoReadyChannels(t *testing.T) {
	runner := MustNewRunner()

	result, err := runner.Run(`
		(let [ch (chan)]
		  (alts! [ch] {:timeout 1}))
	`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.String() != "[nil :timeout]" {
		t.Fatalf("expected [nil :timeout], got %s", result.String())
	}
}

func TestAltsTimeoutUsesReadyChannelWhenAvailable(t *testing.T) {
	runner := MustNewRunner()

	result, err := runner.Run(`
		(let [ch (chan 1)
		      res (do
		            (>! ch 7)
		            (alts! [ch] {:timeout 1000}))]
		  (and (= 7 (first res))
		       (chan? (second res))))
	`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.String() != "true" {
		t.Fatalf("expected true, got %s", result.String())
	}
}

func TestAltsAcceptsListPorts(t *testing.T) {
	runner := MustNewRunner()

	result, err := runner.Run(`
		(let [ch (chan 1)
		      res (do
		            (>! ch 1)
		            (alts! (list ch)))]
		  (and (= 1 (first res))
		       (chan? (second res))))
	`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.String() != "true" {
		t.Fatalf("expected true, got %s", result.String())
	}
}

func TestAltsSendPortSuccess(t *testing.T) {
	runner := MustNewRunner()

	result, err := runner.Run(`
		(let [ch (chan 1)]
		  (let [res (alts! [[ch 1]])]
		    (and (= true (first res))
		         (chan? (second res))
		         (= 1 (<! ch)))))
	`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.String() != "true" {
		t.Fatalf("expected true, got %s", result.String())
	}
}

func TestAltsPriorityPrefersLeftReadyPort(t *testing.T) {
	runner := MustNewRunner()

	result, err := runner.Run(`
		(let [send-ch (chan 1)
		      recv-ch (chan 1)]
		  (>! recv-ch :recv)
		  (let [res (alts! [[send-ch :sent] recv-ch] {:priority true})
		        send-state (alts! [send-ch] {:default :none})
		        recv-state (alts! [recv-ch] {:default :none})]
		    (and (= true (first res))
		         (= :sent (first send-state))
		         (= :recv (first recv-state))
		         (chan? (second res)))))
	`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.String() != "true" {
		t.Fatalf("expected true, got %s", result.String())
	}
}

func TestAltsRejectsUnknownOptions(t *testing.T) {
	runner := MustNewRunner()
	_, err := runner.Run(`
		(let [ch (chan)]
		  (alts! [ch] {:z 1 :a 2 :default :ok}))
	`)
	if err == nil || !strings.Contains(err.Error(), "alts!: unsupported option :a") {
		t.Fatalf("expected sorted unsupported-option error, got %v", err)
	}
}

func TestParseAltsOptionsTimeoutRoundsUp(t *testing.T) {
	optsMap := coll.NewMap(map[types.Value]types.Value{
		atom.Keyword("timeout"): atom.NewNumberFloat(0.1),
	})

	opts, errVal := parseAltsOptions(optsMap)
	if errVal != nil {
		t.Fatalf("unexpected parse error: %v", errVal)
	}
	if opts.timeoutMs != 1 {
		t.Fatalf("expected timeoutMs=1, got %d", opts.timeoutMs)
	}
}

func TestAltsErrors(t *testing.T) {
	tests := []struct {
		name      string
		code      string
		wantError string
	}{
		{
			name:      "empty vector ports",
			code:      `(alts! [])`,
			wantError: "alts!: ports must not be empty",
		},
		{
			name:      "empty list ports",
			code:      `(alts! (list))`,
			wantError: "alts!: ports must not be empty",
		},
		{
			name:      "non channel in ports",
			code:      `(alts! [1])`,
			wantError: "alts!: ports[0] must be a channel",
		},
		{
			name:      "ports type must be vector or list",
			code:      `(alts! {:a 1})`,
			wantError: "alts!: first argument must be a vector or list of channels",
		},
		{
			name:      "opts must be map",
			code:      `(alts! [(chan)] :oops)`,
			wantError: "alts!: opts must be a map",
		},
		{
			name:      "default timeout conflict",
			code:      `(alts! [(chan)] {:default :x :timeout 1})`,
			wantError: "alts!: :default and :timeout cannot be used together",
		},
		{
			name:      "timeout must be number",
			code:      `(alts! [(chan)] {:timeout :x})`,
			wantError: "alts!: :timeout must be a number",
		},
		{
			name:      "timeout must be non negative",
			code:      `(alts! [(chan)] {:timeout -1})`,
			wantError: "alts!: :timeout must be non-negative",
		},
		{
			name:      "fractional timeout must be non negative",
			code:      `(alts! [(chan)] {:timeout -0.1})`,
			wantError: "alts!: :timeout must be non-negative",
		},
		{
			name:      "timeout must fit duration",
			code:      `(alts! [(chan)] {:timeout 10000000000000})`,
			wantError: "alts!: :timeout is out of range",
		},
		{
			name:      "priority must be boolean",
			code:      `(alts! [(chan)] {:priority :yes})`,
			wantError: "alts!: :priority must be boolean",
		},
		{
			name:      "send port must be [channel value]",
			code:      `(alts! [[(chan)]])`,
			wantError: "alts!: ports[0] send port must be [channel value]",
		},
		{
			name:      "send port first element must be channel",
			code:      `(alts! [[1 2]])`,
			wantError: "alts!: ports[0] send port first element must be a channel",
		},
		{
			name:      "closed send channel should error",
			code:      `(let [ch (chan)] (close! ch) (alts! [[ch 1]]))`,
			wantError: "alts!: send on closed channel",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := MustNewRunner()
			_, err := runner.Run(tt.code)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantError)
			}
			if !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("expected error containing %q, got: %v", tt.wantError, err)
			}
		})
	}
}
