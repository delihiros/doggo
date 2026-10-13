package vm

import "testing"

func TestResetAndEnableExecStatsDiscardsPreviousCounts(t *testing.T) {
	machine := &VM{}
	machine.ResetAndEnableExecStats()
	machine.execStats.TotalInstructions = 7

	machine.ResetAndEnableExecStats()
	if !machine.collectStats || machine.execStats == nil || machine.execStats.TotalInstructions != 0 {
		t.Fatalf("execution stats were not reset and enabled: collect=%v stats=%+v", machine.collectStats, machine.execStats)
	}
}
