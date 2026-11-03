package napitest

import (
	"runtime"
	"time"
)

// Go replacements for libuv utilities that the C tests use only as test helpers (uv_thread_*,
// uv_sleep, uv_hrtime): Node-API does not depend on how the calling thread was spawned.

// uvOK and uvEINVAL are the uv_thread_create / uv_thread_join return values used by the tests.
const (
	uvOK     = 0
	uvEINVAL = -22
)

// Thread corresponds to uv_thread_t: a goroutine locked to its own OS thread, which terminates
// along with the goroutine like a thread created by uv_thread_create.
type Thread struct {
	done chan struct{}
}

// Create corresponds to uv_thread_create(&t, fn, data): returns 0.
func (t *Thread) Create(fn func()) int {
	t.done = make(chan struct{})
	go func() {
		// Without UnlockOSThread the OS thread terminates when the goroutine exits.
		runtime.LockOSThread()
		defer close(t.done)
		fn()
	}()
	return uvOK
}

// Join corresponds to uv_thread_join(&t): waits for the thread to finish and returns 0, or
// UV_EINVAL if the thread was never created.
func (t *Thread) Join() int {
	if t.done == nil {
		return uvEINVAL
	}
	<-t.done
	return uvOK
}

// Sleep corresponds to uv_sleep(msec).
func Sleep(msec uint) {
	time.Sleep(time.Duration(msec) * time.Millisecond)
}

// HRTime corresponds to uv_hrtime(): monotonic time in nanoseconds.
func HRTime() uint64 {
	return uint64(time.Since(hrtimeBase).Nanoseconds())
}

var hrtimeBase = time.Now()
