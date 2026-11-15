//go:build !windows

package mcp

import (
	"os/exec"
	"syscall"
)

// setProcessGroup puts the spawned server into a fresh process group led by its
// own pid, so killProcessGroup can reap the whole tree.
//
// Package runners (npx / npm exec / pnpm dlx) fork a chain — `npm exec` → `sh -c`
// → the real `node` server — and killing only the direct child leaves the
// grandchildren orphaned to PID 1, where they keep a full runtime resident
// forever. One process group per server makes Close authoritative.
func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killProcessGroup SIGKILLs every process in the group led by pid.
// A non-positive pid would address the caller's own group, so it is ignored.
func killProcessGroup(pid int) {
	if pid <= 0 {
		return
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}
