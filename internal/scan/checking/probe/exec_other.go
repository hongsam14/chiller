//go:build !linux

package probe

import (
	"context"
	"os"
	"os/exec"
)

// defaultExec (non-Linux fallback): process-group signalling is Linux-specific,
// so on other platforms we rely on exec.CommandContext's default behaviour
// (SIGKILL to the direct child on ctx cancel). The production image is Linux, so
// the process-group discipline in exec_linux.go is what actually ships; this
// exists only to keep the package buildable on developer machines.
func defaultExec(ctx context.Context, name string, env []string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	return cmd.Output()
}
