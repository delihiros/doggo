//go:build unix

package vm

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A release gate prevents scheduler delays from letting the child create its
// marker before cancellation. EOF on its inherited pipe confirms termination.
func TestProcessKillTreeBackgroundShell(t *testing.T) {
	dir := t.TempDir()
	ready := filepath.Join(dir, "ready")
	marker := filepath.Join(dir, "marker")
	release := filepath.Join(dir, "release")
	child := fmt.Sprintf(`printf '%%s' "$$" > %s; while [ ! -f %s ]; do sleep 0.01; done; printf orphan > %s`, processShellSingleQuote(ready), processShellSingleQuote(release), processShellSingleQuote(marker))
	script := "sh -c " + processShellSingleQuote(child) + " & wait"
	h, err := processStartHandle(context.Background(), []string{"sh", "-c", script}, processStartOptions{
		in:  processRedirect{mode: processRedirectClosed},
		out: processRedirect{mode: processRedirectPipe},
		err: processRedirect{mode: processRedirectPipe},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.cmd.Cancel() })
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
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.stdout.done:
	case <-time.After(5 * time.Second):
		t.Fatal("background child kept stdout open after cancellation")
	}
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

// A native child holds the captured stdout open until released. Killing the
// whole group must close that descriptor without letting the child write its
// marker. Releasing the child after killing just the parent is a negative
// control: it must create the marker, proving this test detects orphaning.
func TestProcessKillTreeNativeDescendant(t *testing.T) {
	for _, killTree := range []bool{true, false} {
		t.Run(fmt.Sprintf("killTree=%v", killTree), func(t *testing.T) {
			dir := t.TempDir()
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			h, err := processStartHandle(ctx, []string{executable, "-test.run=^TestProcessTreeNativeFixture$", "--", "parent", dir}, processStartOptions{
				in:  processRedirect{mode: processRedirectClosed},
				out: processRedirect{mode: processRedirectPipe},
				err: processRedirect{mode: processRedirectPipe},
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = h.cmd.Cancel() })
			deadline := time.Now().Add(5 * time.Second)
			ready := filepath.Join(dir, "ready")
			for {
				if _, err := os.Stat(ready); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("native child did not become ready")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if killTree {
				if _, err := h.killTree(); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := h.cmd.Process.Kill(); err != nil {
					t.Fatal(err)
				}
				h.exitRef.Deref()
			}
			if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			select {
			case <-h.stdout.done:
			case <-time.After(5 * time.Second):
				t.Fatal("descendant kept stdout open after cancellation")
			}
			_, err = os.Stat(filepath.Join(dir, "marker"))
			if killTree && !os.IsNotExist(err) {
				t.Fatalf("group kill left child running: %v", err)
			}
			if !killTree && err != nil {
				t.Fatalf("negative control did not detect surviving child: %v", err)
			}
		})
	}
}

func TestProcessTreeNativeFixture(t *testing.T) {
	if len(os.Args) < 4 || os.Args[len(os.Args)-3] != "--" {
		return
	}
	mode, dir := os.Args[len(os.Args)-2], os.Args[len(os.Args)-1]
	switch mode {
	case "parent":
		executable, err := os.Executable()
		if err != nil {
			os.Exit(2)
		}
		child := exec.Command(executable, "-test.run=^TestProcessTreeNativeFixture$", "--", "child", dir)
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Run(); err != nil {
			os.Exit(3)
		}
	case "child":
		if err := os.WriteFile(filepath.Join(dir, "ready"), nil, 0600); err != nil {
			os.Exit(4)
		}
		deadline := time.Now().Add(8 * time.Second)
		for {
			if _, err := os.Stat(filepath.Join(dir, "release")); err == nil {
				break
			}
			if time.Now().After(deadline) {
				os.Exit(5)
			}
			time.Sleep(10 * time.Millisecond)
		}
		if err := os.WriteFile(filepath.Join(dir, "marker"), nil, 0600); err != nil {
			os.Exit(6)
		}
	default:
		return
	}
	os.Exit(0)
}
