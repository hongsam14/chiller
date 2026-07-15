//go:build linux

package probe

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// defaultExec runs the CLI in its OWN process group and, on ctx timeout/cancel,
// SIGKILLs the whole group — not just the direct child (NFR-P1). Several probes
// spawn children that would otherwise survive as zombies/orphans: testssl.sh
// forks openssl repeatedly, and the pipx-wrapped Python tools spawn interpreters.
// A plain exec.CommandContext kills only the leader, leaving those children to
// leak. Setpgid makes the child a group leader; Cancel kills the negative PID
// (the whole group); WaitDelay bounds how long we wait for the group to die
// before Wait returns even if a grandchild still holds a stdout pipe open.
func defaultExec(ctx context.Context, name string, env []string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			// Negative PID → deliver to the entire process group (NFR-P1).
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	cmd.WaitDelay = 5 * time.Second
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	return cmd.Output()
}
