package napitest

import (
	"runtime"
	"time"
)

// Sostituti Go delle utilità di libuv che i test C usano solo come strumento (uv_thread_*,
// uv_sleep, uv_hrtime): Node-API non dipende da come è nato il thread chiamante.

// uvOK e uvEINVAL sono i valori di ritorno di uv_thread_create / uv_thread_join usati dai test.
const (
	uvOK     = 0
	uvEINVAL = -22
)

// Thread equivale a uv_thread_t: una goroutine legata al proprio thread di sistema, che termina
// insieme alla goroutine come un thread creato da uv_thread_create.
type Thread struct {
	done chan struct{}
}

// Create equivale a uv_thread_create(&t, fn, data): restituisce 0.
func (t *Thread) Create(fn func()) int {
	t.done = make(chan struct{})
	go func() {
		// Senza UnlockOSThread il thread termina con la goroutine.
		runtime.LockOSThread()
		defer close(t.done)
		fn()
	}()
	return uvOK
}

// Join equivale a uv_thread_join(&t): attende la fine del thread e restituisce 0, oppure
// UV_EINVAL se il thread non è mai stato creato.
func (t *Thread) Join() int {
	if t.done == nil {
		return uvEINVAL
	}
	<-t.done
	return uvOK
}

// Sleep equivale a uv_sleep(msec).
func Sleep(msec uint) {
	time.Sleep(time.Duration(msec) * time.Millisecond)
}

// HRTime equivale a uv_hrtime(): un tempo monotono in nanosecondi.
func HRTime() uint64 {
	return uint64(time.Since(hrtimeBase).Nanoseconds())
}

var hrtimeBase = time.Now()
