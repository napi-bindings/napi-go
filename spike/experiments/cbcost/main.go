// Command cbcost measures the cost of an empty C→Go callback when the calling thread
// was created by C (like Node's main thread) versus a Go runtime thread.
// Explains the Linux/Windows gap observed in the spike benchmark.
//
//	go run .            (CC=zig cc on Windows)
package main

/*
#include <stdint.h>
extern void goCb(void);
static void loop(int64_t n) { for (int64_t i = 0; i < n; i++) goCb(); }
#ifdef _WIN32
#include <windows.h>
static DWORD WINAPI thr(LPVOID p) { loop((int64_t)(intptr_t)p); return 0; }
static void run_in_c_thread(int64_t n) {
  HANDLE h = CreateThread(NULL, 0, thr, (LPVOID)(intptr_t)n, 0, NULL);
  WaitForSingleObject(h, INFINITE);
  CloseHandle(h);
}
#else
#include <pthread.h>
static void* thr(void* p) { loop((int64_t)(intptr_t)p); return NULL; }
static void run_in_c_thread(int64_t n) {
  pthread_t t;
  pthread_create(&t, NULL, thr, (void*)(intptr_t)n);
  pthread_join(t, NULL);
}
#endif
static void run_in_go_thread(int64_t n) { loop(n); }
static void nop(void) {}
*/
import "C"

import (
	"fmt"
	"runtime"
	"time"
)

func measure(name string, n int64, f func(int64)) {
	f(n / 10) // warmup
	best := time.Duration(1 << 62)
	for r := 0; r < 5; r++ {
		t0 := time.Now()
		f(n)
		if d := time.Since(t0); d < best {
			best = d
		}
	}
	fmt.Printf("%-44s %8.1f ns/op\n", name, float64(best.Nanoseconds())/float64(n))
}

func main() {
	fmt.Printf("%s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	const n = 200_000
	measure("Go→C (nop)", n*10, func(k int64) {
		for i := int64(0); i < k; i++ {
			C.nop()
		}
	})
	measure("C→Go from Go thread (inside a cgo call)", n, func(k int64) { C.run_in_go_thread(C.int64_t(k)) })
	measure("C→Go from C-created thread (like Node)", n, func(k int64) { C.run_in_c_thread(C.int64_t(k)) })
}
