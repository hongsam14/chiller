//go:build linux

package probe

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// defaultExec must return a tool's stdout verbatim on success.
func TestDefaultExec_Success(t *testing.T) {
	out, err := defaultExec(context.Background(), "/bin/echo", nil, "hello")
	if err != nil {
		t.Fatalf("echo failed: %v", err)
	}
	if strings.TrimSpace(string(out)) != "hello" {
		t.Errorf("stdout = %q, want hello", out)
	}
}

// A non-zero exit is an error (→ the adapter's items become tool-failed, never
// a silent clean result).
func TestDefaultExec_NonZeroExit_IsError(t *testing.T) {
	if _, err := defaultExec(context.Background(), "/bin/sh", nil, "-c", "exit 3"); err == nil {
		t.Fatal("want error on non-zero exit, got nil")
	}
}

// The core NFR-P1 guarantee: when the context deadline fires, the WHOLE process
// group dies — not just the direct child. We launch a shell that spawns a
// long-lived grandchild and writes its PID to a file, then let the deadline kill
// the group and assert the grandchild is gone. A plain exec.CommandContext
// (child-only kill) would leave the grandchild alive → this test would fail.
func TestDefaultExec_TimeoutKillsProcessGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	// sh starts `sleep 60 & echo $! > pidfile; wait` — the sleep is the grandchild.
	script := "sleep 60 & echo $! > " + pidFile + "; wait"

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := defaultExec(ctx, "/bin/sh", nil, "-c", script)
	if err == nil {
		t.Fatal("want deadline error, got nil")
	}
	if elapsed := time.Since(start); elapsed > 6*time.Second {
		t.Fatalf("defaultExec did not return promptly (%v) — WaitDelay/group-kill not working", elapsed)
	}

	pid := readPID(t, pidFile)
	// Give the SIGKILL a moment to propagate through the group.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return // grandchild reaped with the group — NFR-P1 holds
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Clean up the leak so the test host is not polluted, then fail.
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Fatalf("grandchild pid %d survived the timeout — process group was NOT killed (zombie/orphan leak)", pid)
}

// proxy env is injected into the child's environment (financial mode).
func TestDefaultExec_InjectsProxyEnv(t *testing.T) {
	out, err := defaultExec(context.Background(), "/bin/sh",
		[]string{"HTTP_PROXY=http://sentinel:8080"}, "-c", "echo $HTTP_PROXY")
	if err != nil {
		t.Fatalf("sh failed: %v", err)
	}
	if strings.TrimSpace(string(out)) != "http://sentinel:8080" {
		t.Errorf("proxy env not injected: got %q", out)
	}
}

func readPID(t *testing.T, path string) int {
	t.Helper()
	// The child writes the pidfile asynchronously; poll briefly.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(path)
		if err == nil && len(strings.TrimSpace(string(b))) > 0 {
			pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
			if err != nil {
				t.Fatalf("bad pid file contents %q: %v", b, err)
			}
			return pid
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("grandchild never wrote its pid file")
	return 0
}

func processAlive(pid int) bool {
	// signal 0 probes existence without delivering a signal.
	if syscall.Kill(pid, 0) != nil {
		return false // gone (ESRCH) → reaped
	}
	// The PID may still exist as a ZOMBIE: killed but not yet reaped. That happens
	// when the test binary is PID 1 (e.g. run directly as a container entrypoint)
	// and does not reap the reparented grandchild. A zombie is effectively dead —
	// the process-group kill worked — so read /proc/<pid>/stat and treat state 'Z'
	// (and 'X'/dead) as not alive. Without this the test false-fails under PID 1.
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	// stat: "pid (comm) STATE ..." — the state char follows the ")".
	if i := bytes.LastIndexByte(b, ')'); i >= 0 && i+2 < len(b) {
		switch b[i+2] {
		case 'Z', 'X', 'x':
			return false // zombie / dead
		}
	}
	return true
}
