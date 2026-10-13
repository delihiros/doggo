package vm

import (
	"context"
	"errors"
	"testing"
)

func TestProcessNeedsTreeControl(t *testing.T) {
	cancellableCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tests := []struct {
		name      string
		ctx       context.Context
		timeoutMs int64
		want      bool
	}{
		{name: "background_without_timeout", ctx: context.Background(), timeoutMs: 0, want: false},
		{name: "background_with_timeout", ctx: context.Background(), timeoutMs: 10, want: true},
		{name: "cancellable_without_timeout", ctx: cancellableCtx, timeoutMs: 0, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := processNeedsTreeControl(tt.ctx, tt.timeoutMs); got != tt.want {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func TestResolveWindowsProcessTreeStrategy(t *testing.T) {
	tests := []struct {
		name      string
		env       windowsProcessTreeEnvironment
		want      windowsProcessTreeStrategy
		wantError string
	}{
		{
			name: "not_in_job",
			env:  windowsProcessTreeEnvironment{},
			want: windowsProcessTreeStrategy{},
		},
		{
			name: "nested_jobs_supported",
			env: windowsProcessTreeEnvironment{
				currentProcessInJob: true,
				nestedJobsSupported: true,
			},
			want: windowsProcessTreeStrategy{},
		},
		{
			name: "breakaway_required_on_legacy_windows",
			env: windowsProcessTreeEnvironment{
				currentProcessInJob:      true,
				parentJobAllowsBreakaway: true,
			},
			want: windowsProcessTreeStrategy{breakawayFromParentJob: true},
		},
		{
			name: "unsupported_legacy_job",
			env: windowsProcessTreeEnvironment{
				currentProcessInJob: true,
			},
			wantError: "nested jobs or a parent job with breakaway enabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveWindowsProcessTreeStrategy(tt.env)
			if tt.wantError != "" {
				if err == nil {
					t.Fatalf("expected error containing %q", tt.wantError)
				}
				if !errors.Is(err, errProcessTreeUnsupported) {
					t.Fatalf("expected unsupported-process-tree error, got %v", err)
				}
				if err.Error() != "Windows process-tree management requires nested jobs or a parent job with breakaway enabled" {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %+v, got %+v", tt.want, got)
			}
		})
	}
}

func TestWindowsProcessTreeStateTransitions(t *testing.T) {
	t.Run("cancel_before_assignment_targets_process", func(t *testing.T) {
		state := windowsProcessTreeState{}
		if got := state.cancelAction(); got != windowsProcessTreeCancelProcess {
			t.Fatalf("expected process cancel, got %v", got)
		}
		if !state.markAssigned() {
			t.Fatal("expected assignment to observe prior cancellation")
		}
	})

	t.Run("cancel_after_assignment_targets_job", func(t *testing.T) {
		state := windowsProcessTreeState{}
		if state.markAssigned() {
			t.Fatal("did not expect pre-existing cancellation")
		}
		if got := state.cancelAction(); got != windowsProcessTreeCancelJob {
			t.Fatalf("expected job cancel, got %v", got)
		}
		if !state.isCanceled() {
			t.Fatal("expected canceled state")
		}
	})
}
