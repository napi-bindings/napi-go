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

// startWatchdog stops the process after d with a report on stderr: the state of every thread
// (name, state, kernel wait channel, current syscall) and the stacks of all goroutines. It
// diagnoses hangs that happen only on CI machines. If the Go runtime itself is stuck (a
// stop-the-world that never completes) the timer cannot run: the test reads /proc from the
// parent process then.
func startWatchdog(d time.Duration) {
	time.AfterFunc(d, func() {
		var b strings.Builder
		fmt.Fprintf(&b, "napi-go watchdog: process still running after %v\n", d)
		b.WriteString(threadReport(os.Getpid()))
		buf := make([]byte, stackDumpBytes)
		b.Write(buf[:runtime.Stack(buf, true)])
		if _, err := os.Stderr.WriteString(b.String()); err != nil {
			// The report is lost: a distinct exit code tells the test to rely on the threads it
			// reads from /proc itself.
			os.Exit(watchdogReportLostExit)
		}
		os.Exit(watchdogExit)
	})
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
		fmt.Fprintf(&b, "thread %s name=%q state=%q wchan=%q syscall=%q\n", e.Name(),
			procField(base+"status", "Name:"), procField(base+"status", "State:"),
			procText(base+"wchan"), procText(base+"syscall"))
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

// procField returns the value of a "Key:\tvalue" line of a /proc status file.
func procField(path, key string) string {
	for _, line := range strings.Split(procText(path), "\n") {
		if v, ok := strings.CutPrefix(line, key); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
