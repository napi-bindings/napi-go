package main

// Exports for tests/e2e/process.test.mjs: Go runtime load (preemption, GC) while V8 handles
// signals, plus the hooks that reproduce and repair the macOS loss of SA_RESETHAND.

import (
	"runtime"
	"sync/atomic"
	"time"

	"github.com/napi-bindings/napi-go/sys"
)

const (
	// burnChunkBytes is allocated by each burning goroutine on every iteration, so the Go GC runs
	// while the goroutines also keep every P busy (asynchronous preemption via SIGURG on Unix,
	// SuspendThread on Windows).
	burnChunkBytes = 64 << 10
	// burnSink keeps the allocations reachable for one iteration, so the compiler cannot drop them.
	burnSinkSlots = 8
)

// burners counts the goroutines started by burnCPU that are still running.
var burners atomic.Int64

// burnCPU(ms) starts one CPU-bound, allocating goroutine per P for ms milliseconds and returns
// immediately; burning() reports how many are still running.
func burnCPU(env sys.Env, _ sys.Value, args []sys.Value) sys.Value {
	ms, ok := argNum(env, args, 0)
	if !ok || ms < 0 {
		return throwType(env, "burnCPU(ms)")
	}
	deadline := time.Now().Add(time.Duration(ms * float64(time.Millisecond)))
	n := runtime.GOMAXPROCS(0)
	burners.Add(int64(n))
	for i := 0; i < n; i++ {
		go burn(deadline)
	}
	return newNum(env, float64(n))
}

func burn(deadline time.Time) {
	defer burners.Add(-1)
	var sink [burnSinkSlots][]byte
	for i := 0; time.Now().Before(deadline); i++ {
		sink[i%burnSinkSlots] = make([]byte, burnChunkBytes)
	}
	runtime.KeepAlive(sink)
}

// burning() returns the number of burnCPU goroutines still running.
func burning(env sys.Env, _ sys.Value, _ []sys.Value) sys.Value {
	return newNum(env, float64(burners.Load()))
}

// goGC() runs a full Go garbage collection.
func goGC(env sys.Env, _ sys.Value, _ []sys.Value) sys.Value {
	runtime.GC()
	return undefined(env)
}

// dropResetHand(signo) clears SA_RESETHAND from the handler of signo, as the Go runtime does on
// macOS when it reinstalls existing handlers. It throws if sigaction fails or on Windows.
func dropResetHand(env sys.Env, _ sys.Value, args []sys.Value) sys.Value {
	signo, ok := argNum(env, args, 0)
	if !ok {
		return throwType(env, "dropResetHand(signo)")
	}
	if err := clearResetHand(int(signo)); err != nil {
		return throwType(env, "dropResetHand: "+err.Error())
	}
	return undefined(env)
}

// repairSignalHandlers() runs the same repair as the module entry point (sys/sigfix.c).
func repairSignalHandlers(env sys.Env, _ sys.Value, _ []sys.Value) sys.Value {
	sys.RepairNodeSignalHandlers()
	return undefined(env)
}

// signalThreads(signo) sends signo to every other thread of the process with tgkill, Go threads
// included, and returns how many were signaled (Linux only, it throws elsewhere).
func signalThreads(env sys.Env, _ sys.Value, args []sys.Value) sys.Value {
	signo, ok := argNum(env, args, 0)
	if !ok {
		return throwType(env, "signalThreads(signo)")
	}
	n, err := signalAllThreads(int(signo))
	if err != nil {
		return throwType(env, "signalThreads: "+err.Error())
	}
	return newNum(env, float64(n))
}

// onStack(signo) reports whether the handler of signo has SA_ONSTACK (Linux only, it throws
// elsewhere).
func onStack(env sys.Env, _ sys.Value, args []sys.Value) sys.Value {
	signo, ok := argNum(env, args, 0)
	if !ok {
		return throwType(env, "onStack(signo)")
	}
	flags, err := handlerFlags(int(signo))
	if err != nil {
		return throwType(env, "onStack: "+err.Error())
	}
	return newBool(env, flags&saOnStack != 0)
}

// watchdog(ms) stops the process after ms milliseconds with a report of its threads and
// goroutines on stderr and exit code 70 (Linux only; a no-op elsewhere).
func watchdog(env sys.Env, _ sys.Value, args []sys.Value) sys.Value {
	ms, ok := argNum(env, args, 0)
	if !ok || ms <= 0 {
		return throwType(env, "watchdog(ms)")
	}
	if err := startWatchdog(time.Duration(ms * float64(time.Millisecond))); err != nil {
		return throwType(env, "watchdog: "+err.Error())
	}
	return undefined(env)
}
