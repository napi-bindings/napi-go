package napi

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/napi-bindings/napi-go/sys"
)

// fakeJSThread stands in for Node: notify records a wake-up, and drain plays the JS thread by
// taking the queue and running each closure (with a zero Env, unused by the test closures).
type fakeJSThread struct {
	d       *dispatcher
	notifs  atomic.Int64
	closed  atomic.Bool // set after close returns: notify must never run afterwards
	t       *testing.T
	failing atomic.Pointer[sys.Status]
}

func newFakeJSThread(t *testing.T) *fakeJSThread {
	f := &fakeJSThread{t: t}
	f.d = &dispatcher{notify: func() sys.Status {
		if f.closed.Load() {
			t.Error("notify called after close returned")
		}
		if st := f.failing.Load(); st != nil {
			return *st
		}
		f.notifs.Add(1)
		return sys.StatusOK
	}}
	return f
}

func (f *fakeJSThread) drain() int {
	q := f.d.take()
	for _, fn := range q {
		fn(Env{})
	}
	return len(q)
}

const (
	posters        = 8
	postsPerPoster = 2000
)

// TestDispatcherDeliversInOrderAndCoalesces: every post runs exactly once, in posting order per
// goroutine, and wake-ups are coalesced (at most one per batch taken by the JS thread).
func TestDispatcherDeliversInOrderAndCoalesces(t *testing.T) {
	f := newFakeJSThread(t)
	var last [posters]int
	var ran atomic.Int64
	var wg sync.WaitGroup
	for p := 0; p < posters; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 1; i <= postsPerPoster; i++ {
				if err := f.d.post(func(Env) {
					// Runs on the single "JS thread" goroutine below: no lock needed.
					if last[p] != i-1 {
						t.Errorf("poster %d: got %d after %d", p, i, last[p])
					}
					last[p] = i
					ran.Add(1)
				}); err != nil {
					t.Errorf("post: %v", err)
					return
				}
			}
		}()
	}
	done := make(chan struct{})
	batches := 0
	go func() { wg.Wait(); close(done) }()
	for finished := false; !finished; {
		select {
		case <-done:
			finished = true
		default:
		}
		if f.drain() > 0 {
			batches++
		}
	}
	for f.drain() > 0 {
		batches++
	}
	if got, want := ran.Load(), int64(posters*postsPerPoster); got != want {
		t.Fatalf("ran %d closures, want %d", got, want)
	}
	if n := f.notifs.Load(); n > int64(batches) || n == 0 {
		t.Fatalf("%d notifications for %d batches: not coalesced", n, batches)
	}
}

// TestDispatcherCloseStopsNotifications: posts racing with close either succeed before it or
// return ErrEnvClosed, and notify is never called once close has returned.
func TestDispatcherCloseStopsNotifications(t *testing.T) {
	f := newFakeJSThread(t)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for p := 0; p < posters; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < postsPerPoster; i++ {
				if err := f.d.post(func(Env) {}); err != nil {
					if !errors.Is(err, ErrEnvClosed) {
						t.Errorf("post: %v", err)
					}
					return
				}
				f.drain()
			}
		}()
	}
	close(start)
	f.d.close()
	f.closed.Store(true)
	wg.Wait()
	if err := f.d.post(func(Env) { t.Error("closure ran after close") }); !errors.Is(err, ErrEnvClosed) {
		t.Fatalf("post after close: %v, want ErrEnvClosed", err)
	}
	if n := f.drain(); n != 0 {
		t.Fatalf("%d closures left after close", n)
	}
	f.d.requeue([]func(Env){func(Env) { t.Error("requeued closure ran after close") }})
	f.drain()
}

// TestDispatcherRequeueKeepsOrder: closures put back by the JS thread run before newer posts.
func TestDispatcherRequeueKeepsOrder(t *testing.T) {
	f := newFakeJSThread(t)
	var order []int
	add := func(i int) func(Env) { return func(Env) { order = append(order, i) } }
	for i := 1; i <= 3; i++ {
		if err := f.d.post(add(i)); err != nil {
			t.Fatal(err)
		}
	}
	q := f.d.take()
	q[0](Env{})
	if err := f.d.post(add(4)); err != nil {
		t.Fatal(err)
	}
	before := f.notifs.Load()
	f.d.requeue(q[1:])
	if f.notifs.Load() != before {
		t.Fatal("requeue notified while a notification was already scheduled")
	}
	f.drain()
	if want := []int{1, 2, 3, 4}; len(order) != len(want) || order[1] != 2 || order[2] != 3 || order[3] != 4 {
		t.Fatalf("order %v, want %v", order, want)
	}
	// Requeue on an idle dispatcher must wake the JS thread.
	before = f.notifs.Load()
	f.d.requeue([]func(Env){add(5)})
	if f.notifs.Load() != before+1 {
		t.Fatal("requeue on an idle dispatcher did not notify")
	}
}

// TestDispatcherNotifyFailure: a failed wake-up is reported and keeps the closure queued for the
// next successful post; napi_closing is reported as ErrEnvClosed.
func TestDispatcherNotifyFailure(t *testing.T) {
	f := newFakeJSThread(t)
	ran := 0
	inc := func(Env) { ran++ }
	generic := sys.StatusGenericFailure
	f.failing.Store(&generic)
	var ne *Error
	if err := f.d.post(inc); !errors.As(err, &ne) || ne.Status != sys.StatusGenericFailure {
		t.Fatalf("post with failing notify: %v", err)
	}
	closing := sys.StatusClosing
	f.failing.Store(&closing)
	if err := f.d.post(inc); !errors.Is(err, ErrEnvClosed) {
		t.Fatalf("post with napi_closing: %v, want ErrEnvClosed", err)
	}
	f.failing.Store(nil)
	if err := f.d.post(inc); err != nil {
		t.Fatal(err)
	}
	if n := f.drain(); n != 3 || ran != 3 {
		t.Fatalf("drained %d, ran %d: want the 3 closures", n, ran)
	}
}

// TestDispatcherEdgeCases: an empty queue, an empty requeue and a single post.
func TestDispatcherEdgeCases(t *testing.T) {
	f := newFakeJSThread(t)
	if n := f.drain(); n != 0 {
		t.Fatalf("empty dispatcher drained %d closures", n)
	}
	f.d.requeue(nil)
	f.d.requeue([]func(Env){})
	if n := f.notifs.Load(); n != 0 {
		t.Fatalf("empty requeue notified %d times", n)
	}
	ran := 0
	if err := f.d.post(func(Env) { ran++ }); err != nil {
		t.Fatal(err)
	}
	if n := f.notifs.Load(); n != 1 {
		t.Fatalf("single post: %d notifications, want 1", n)
	}
	if n := f.drain(); n != 1 || ran != 1 {
		t.Fatalf("single post: drained %d, ran %d", n, ran)
	}
	// After a take the dispatcher is idle again: the next post notifies again.
	if err := f.d.post(func(Env) { ran++ }); err != nil {
		t.Fatal(err)
	}
	if n := f.notifs.Load(); n != 2 {
		t.Fatalf("post after take: %d notifications, want 2", n)
	}
	f.d.close()
	f.d.close() // idempotent
	f.closed.Store(true)
	if n := f.drain(); n != 0 {
		t.Fatalf("close kept %d closures", n)
	}
}
