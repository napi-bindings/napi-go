//go:build linux

package main

/*
#include <signal.h>
#include <string.h>

// handler_flags returns the sa_flags of the handler of signo, or -1 if sigaction fails.
static long handler_flags(int signo) {
  struct sigaction sa;
  memset(&sa, 0, sizeof sa);
  if (sigaction(signo, NULL, &sa) != 0) return -1;
  return (long)sa.sa_flags;
}

// watchdog_linux.c
int gn_start_watchdog(unsigned ms);
*/
import "C"

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// procTaskDir lists the threads of the current process.
const procTaskDir = "/proc/self/task"

// sigBlkField is the line of /proc/<pid>/task/<tid>/status with the blocked-signal mask, in hex
// (bit signo-1 set when signo is blocked).
var sigBlkField = []byte("SigBlk:")

// threadBlocksSignal reports whether thread tid blocks signo. A thread that exited, or whose
// status cannot be read, counts as blocking: it must not be signaled.
func threadBlocksSignal(tid, signo int) bool {
	status, err := os.ReadFile(procTaskDir + "/" + strconv.Itoa(tid) + "/status")
	if err != nil {
		return true
	}
	for _, line := range bytes.Split(status, []byte("\n")) {
		if v, ok := bytes.CutPrefix(line, sigBlkField); ok {
			mask, err := strconv.ParseUint(string(bytes.TrimSpace(v)), 16, 64)
			return err != nil || mask&(1<<(signo-1)) != 0
		}
	}
	return true
}

// signalAllThreads sends signo to every thread of the process except the caller and those that
// block it, with tgkill: unlike kill, the signal is delivered to that very thread, so Go threads
// running goroutines get it too. A thread that blocks the signal is skipped (Node has one): the
// signal would stay pending on it and could be delivered once its handler is gone, at exit. It
// returns the number of signals sent.
func signalAllThreads(signo int) (int, error) {
	entries, err := os.ReadDir(procTaskDir)
	if err != nil {
		return 0, err
	}
	pid, self := syscall.Getpid(), syscall.Gettid()
	sent := 0
	for _, e := range entries {
		tid, err := strconv.Atoi(e.Name())
		if err != nil || tid == self || threadBlocksSignal(tid, signo) {
			continue
		}
		// ESRCH: the thread exited after the directory was read.
		if err := syscall.Tgkill(pid, tid, syscall.Signal(signo)); err != nil && !errors.Is(err, syscall.ESRCH) {
			return sent, err
		}
		sent++
	}
	return sent, nil
}

func handlerFlags(signo int) (int64, error) {
	f := int64(C.handler_flags(C.int(signo)))
	if f < 0 {
		return 0, errors.New("sigaction failed")
	}
	return f, nil
}

// saOnStack is SA_ONSTACK, for the JS side.
const saOnStack = int64(C.SA_ONSTACK)

// Exit codes of a process stopped by the watchdog: with its report on stderr, or without it
// (writing stderr failed).
const (
	watchdogExit           = 70
	watchdogReportLostExit = 71
)

// stackDumpBytes bounds the goroutine dump of the watchdog.
const stackDumpBytes = 1 << 20

// goWatchdogDelay is how long after the C watchdog the Go one runs: the samples of the C
// watchdog are written first.
const goWatchdogDelay = 5 * time.Second

// startWatchdog reports on stderr a process still running after d, to diagnose hangs that happen
// only on CI machines:
//   - a C thread (watchdog_linux.c) prints where every thread is executing; it does not need
//     the Go runtime;
//   - goWatchdogDelay later a Go timer prints the state of every thread from /proc, then the
//     stacks of all goroutines, and exits with watchdogExit. Collecting the stacks stops the
//     world: if the runtime is stuck that never completes (the report up to there is already
//     written) and the test kills the process.
func startWatchdog(d time.Duration) error {
	if errno := C.gn_start_watchdog(C.uint(d.Milliseconds())); errno != 0 {
		return fmt.Errorf("watchdog thread: %w", syscall.Errno(errno))
	}
	time.AfterFunc(d+goWatchdogDelay, func() {
		write := func(s string) {
			if _, err := os.Stderr.WriteString(s); err != nil {
				// The report is lost: a distinct exit code tells the test to rely on the
				// threads it reads from /proc itself.
				os.Exit(watchdogReportLostExit)
			}
		}
		write(fmt.Sprintf("napi-go watchdog (Go): %d goroutines, %d burning\n%s", runtime.NumGoroutine(), burners.Load(), threadReport(os.Getpid())))
		buf := make([]byte, stackDumpBytes)
		write(string(buf[:runtime.Stack(buf, true)]))
		os.Exit(watchdogExit)
	})
	return nil
}

// threadReport describes the threads of process pid from /proc: one line per thread.
func threadReport(pid int) string {
	dir := "/proc/" + strconv.Itoa(pid) + "/task"
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "threads: " + err.Error() + "\n"
	}
	var b strings.Builder
	for _, e := range entries {
		base := dir + "/" + e.Name() + "/"
		status := procText(base + "status")
		fmt.Fprintf(&b, "thread %s name=%q state=%q blocked=%s pending=%s cpu=%s wchan=%q syscall=%q\n", e.Name(),
			statusField(status, "Name:"), statusField(status, "State:"), statusField(status, "SigBlk:"),
			statusField(status, "SigPnd:"), cpuTicks(procText(base+"stat")), procText(base+"wchan"), procText(base+"syscall"))
	}
	return b.String()
}

// procText returns a /proc file without its trailing newline, or the read error.
func procText(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return "error: " + err.Error()
	}
	return strings.TrimSpace(string(b))
}

// statusField returns the value of a "Key:\tvalue" line of a /proc status file.
func statusField(status, key string) string {
	for _, line := range strings.Split(status, "\n") {
		if v, ok := strings.CutPrefix(line, key); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// Positions of utime and stime among the fields of /proc/<pid>/stat that follow the command
// name (fields 14 and 15 of the file, proc(5)).
const (
	statUtime = 11
	statStime = 12
)

// cpuTicks returns the user and system CPU time of a thread, in clock ticks, from its stat file:
// a thread whose times grow between two reports is running, not blocked.
func cpuTicks(stat string) string {
	// The command name, in parentheses, may contain spaces: the fields start after it.
	if i := strings.LastIndexByte(stat, ')'); i >= 0 {
		if f := strings.Fields(stat[i+1:]); len(f) > statStime {
			return f[statUtime] + "u/" + f[statStime] + "s"
		}
	}
	return "?"
}
