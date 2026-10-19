//go:build unix

package vm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Exercise the stdlib's background-shell fixture directly, so a failure reports
// whether the child escaped the group or produced its marker before cancellation.
func TestProcessKillTreeBackgroundShell(t *testing.T) {
	dir := t.TempDir()
	ready := filepath.Join(dir, "ready")
	marker := filepath.Join(dir, "marker")
	child := fmt.Sprintf(`printf '%%s' "$$" > %s; sleep 0.5; printf orphan > %s`, processShellSingleQuote(ready), processShellSingleQuote(marker))
	script := "sh -lc " + processShellSingleQuote(child) + " & sleep 5"
	h, err := processStartHandle(context.Background(), []string{"sh", "-lc", script}, processStartOptions{
		in:  processRedirect{mode: processRedirectClosed},
		out: processRedirect{mode: processRedirectPipe},
		err: processRedirect{mode: processRedirectPipe},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = h.killTree() })
	deadline := time.Now().Add(5 * time.Second)
	var pidData []byte
	for time.Now().Before(deadline) {
		pidData, _ = os.ReadFile(ready)
		if len(pidData) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	childPID, err := strconv.Atoi(string(pidData))
	if err != nil {
		t.Fatalf("child readiness: %q: %v", pidData, err)
	}
	rootPGID, _ := syscall.Getpgid(h.cmd.Process.Pid)
	childPGID, _ := syscall.Getpgid(childPID)
	_, before := os.Stat(marker)
	t.Logf("root pid=%d pgid=%d child pid=%d pgid=%d markerBeforeKill=%v", h.cmd.Process.Pid, rootPGID, childPID, childPGID, before == nil)
	exit, err := h.killTree()
	if err != nil {
		t.Fatal(err)
	}
	if exit.String() == "0" {
		t.Fatal("kill returned successful exit")
	}
	time.Sleep(600 * time.Millisecond)
	stderr, err := h.stderr.readAll()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(stderr)) != "" {
		t.Logf("shell stderr: %s", stderr)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("child survived kill: marker stat=%v; stderr=%q", err, stderr)
	}
}
