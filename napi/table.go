package napi

import (
	"sync"
	"sync/atomic"
)

// table maps numeric IDs to Go values. IDs are passed to Node-API in place of Go pointers
// (callback data, napi_wrap native objects, finalizer hints, cleanup hook arguments), since cgo
// rules forbid passing Go pointers to C.
//
// Reads (get) are lock-free: a directory of fixed-size segments, replaced by copy only when a new
// segment is needed, with slots read via atomic loads. Writes (add, remove) are serialized under a
// mutex. Freed IDs are reused: an ID must only be removed once C can no longer reference it (in
// the finalizer of the JS object holding it).
//
// ID 0 is never assigned, so a zero data/hint in Node-API remains distinguishable.
type table[T any] struct {
	mu   sync.Mutex
	dir  atomic.Pointer[[]*segment[T]]
	free []uintptr // freed IDs, reused in LIFO order
	next uintptr   // highest ID ever assigned
	live atomic.Int64
}

const (
	segmentBits = 8
	segmentSize = 1 << segmentBits
	segmentMask = segmentSize - 1
)

type segment[T any] [segmentSize]atomic.Pointer[T]

// add stores v and returns its ID (≥ 1).
func (t *table[T]) add(v *T) uintptr {
	t.mu.Lock()
	defer t.mu.Unlock()
	var id uintptr
	if n := len(t.free); n > 0 {
		id = t.free[n-1]
		t.free = t.free[:n-1]
	} else {
		t.next++
		id = t.next
	}
	seg, slot := split(id)
	var dir []*segment[T]
	if p := t.dir.Load(); p != nil {
		dir = *p
	}
	if seg >= uintptr(len(dir)) {
		grown := make([]*segment[T], seg+1)
		copy(grown, dir)
		grown[seg] = new(segment[T])
		t.dir.Store(&grown)
		dir = grown
	}
	dir[seg][slot].Store(v)
	t.live.Add(1)
	return id
}

// get returns the value associated with id, or nil if id is not assigned.
func (t *table[T]) get(id uintptr) *T {
	if id == 0 {
		return nil
	}
	p := t.dir.Load()
	if p == nil {
		return nil
	}
	seg, slot := split(id)
	if seg >= uintptr(len(*p)) {
		return nil
	}
	return (*p)[seg][slot].Load()
}

// remove frees id and returns the value it held (or nil if id was not assigned).
func (t *table[T]) remove(id uintptr) *T {
	if id == 0 {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	p := t.dir.Load()
	if p == nil {
		return nil
	}
	seg, slot := split(id)
	if seg >= uintptr(len(*p)) {
		return nil
	}
	v := (*p)[seg][slot].Swap(nil)
	if v == nil {
		return nil
	}
	t.free = append(t.free, id)
	t.live.Add(-1)
	return v
}

// len returns the number of currently assigned IDs.
func (t *table[T]) len() int { return int(t.live.Load()) }

func split(id uintptr) (seg, slot uintptr) {
	i := id - 1
	return i >> segmentBits, i & segmentMask
}
