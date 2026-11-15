//go:build windows

package mcp

import "os/exec"

// setProcessGroup is a no-op on Windows: there is no process group to join, so
// Close falls back to killing the direct child process only.
func setProcessGroup(cmd *exec.Cmd) {}

// killProcessGroup is a no-op on Windows; Close still kills the direct child.
func killProcessGroup(pid int) {}
