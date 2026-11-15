//go:build !windows

package mcp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// magicTag returns a per-invocation unique sleep duration, so a leaked child
// from an earlier (failing) run can never be mistaken for this run's spawn.
// `sleep` accepts fractional seconds, so this doubles as a unique /proc marker.
func magicTag() string {
	return fmt.Sprintf("7.%d", time.Now().UnixNano()%1000000000)
}

// procStat returns the ppid and pgid of pid.
func procStat(t *testing.T, pid int) (ppid, pgid int, ok bool) {
	t.Helper()
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, 0, false
	}
	// Format: pid (comm) state ppid pgrp ...
	// comm may contain spaces and parentheses, so split after the LAST ')'.
	s := string(raw)
	i := strings.LastIndex(s, ")")
	if i < 0 || i+2 >= len(s) {
		return 0, 0, false
	}
	fields := strings.Fields(s[i+2:])
	if len(fields) < 3 {
		return 0, 0, false
	}
	ppid, _ = strconv.Atoi(fields[1])
	pgid, _ = strconv.Atoi(fields[2])
	return ppid, pgid, true
}

// tagSurvivors returns live pids whose command line mentions tag.
func tagSurvivors(t *testing.T, tag string) []int {
	t.Helper()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		t.Fatalf("read /proc: %v", err)
	}
	var alive []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		cmdline, err := os.ReadFile("/proc/" + e.Name() + "/cmdline")
		if err != nil {
			continue
		}
		if strings.Contains(strings.ReplaceAll(string(cmdline), "\x00", " "), tag) {
			alive = append(alive, pid)
		}
	}
	return alive
}

// TestStdioCloseReapsGrandchildren is the regression test for orphaned MCP
// servers. A real MCP command is a package runner that forks a chain
// (npm exec -> sh -c -> node). Close() used to kill only the direct child, so
// the grandchildren stayed resident under PID 1 for the life of the container.
//
// The fake server below never speaks the protocol, so NewStdioClient fails the
// handshake and takes the Close() path — exactly what must reap the whole tree.
// On the unfixed code the `sh` child dies and its `sleep` grandchild survives,
// which this test reports.
func TestStdioCloseReapsGrandchildren(t *testing.T) {
	tag := magicTag()
	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()

	_, err := NewStdioClient(ctx, "fake", "sh", nil, "-c", "sleep "+tag+"; sleep "+tag)
	if err == nil {
		t.Fatal("handshake against a non-MCP command unexpectedly succeeded")
	}

	// Close has run by now; give the kernel a moment to reap the group.
	deadline := time.Now().Add(2 * time.Second)
	for {
		alive := tagSurvivors(t, tag)
		if len(alive) == 0 {
			return
		}
		if time.Now().After(deadline) {
			for _, pid := range alive {
				_, pgid, _ := procStat(t, pid)
				t.Logf("survivor pid=%d pgid=%d", pid, pgid)
			}
			t.Fatalf("Close left %d process(es) alive from the spawned tree: %v", len(alive), alive)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestStdioSpawnUsesOwnProcessGroup pins the mechanism: the spawned server must
// lead its own process group, which is what makes a negative-pid kill reach
// every descendant.
func TestStdioSpawnUsesOwnProcessGroup(t *testing.T) {
	tag := magicTag()
	cmd := exec.Command("sh", "-c", "sleep "+tag+"; sleep "+tag)
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	pid := cmd.Process.Pid
	defer func() {
		killProcessGroup(pid)
		_ = cmd.Wait()
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		_, pgid, ok := procStat(t, pid)
		if ok {
			if pgid != pid {
				t.Fatalf("child pgid = %d, want its own pid %d (Setpgid not applied)", pgid, pid)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("child vanished before its pgid could be read")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestKillProcessGroupIgnoresInvalidPid guards the negative-pid hazard:
// kill(2) with pid 0 addresses the CALLER's own group, which must never be sent.
func TestKillProcessGroupIgnoresInvalidPid(t *testing.T) {
	killProcessGroup(0)
	killProcessGroup(-1)
	if _, err := os.Stat("/proc/self"); err != nil {
		t.Fatalf("test process did not survive: %v", err)
	}
}
