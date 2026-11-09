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
	"errors"
	"os"
	"strconv"
	"syscall"
)

// procTaskDir lists the threads of the current process.
const procTaskDir = "/proc/self/task"

// signalAllThreads sends signo to every thread of the process except the caller, with tgkill:
// unlike kill, the signal is delivered to that very thread, so Go threads running goroutines get
// it too. It returns the number of signals sent.
func signalAllThreads(signo int) (int, error) {
	entries, err := os.ReadDir(procTaskDir)
	if err != nil {
		return 0, err
	}
	pid, self := syscall.Getpid(), syscall.Gettid()
	sent := 0
	for _, e := range entries {
		tid, err := strconv.Atoi(e.Name())
		if err != nil || tid == self {
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
