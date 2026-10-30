package napi

import (
	"sync"
	"sync/atomic"
)

// table associa id numerici a valori Go. Gli id sono ciò che passa a Node-API al posto dei
// puntatori Go (data delle callback, native object di napi_wrap, hint dei finalizer, arg dei
// cleanup hook): le regole di cgo vietano di affidare a C puntatori Go.
//
// La lettura (get) è senza lock: una directory di segmenti a dimensione fissa, sostituita per
// copia solo quando serve un nuovo segmento, e slot letti con load atomici. Scrittura (add,
// remove) sotto mutex. Gli id liberati vengono riusati: un id va rimosso solo quando C non può più
// usarlo (nel finalizer dell'oggetto JS che lo conserva).
//
// L'id 0 non è mai assegnato: per Node-API un data/hint nullo resta distinguibile.
type table[T any] struct {
	mu   sync.Mutex
	dir  atomic.Pointer[[]*segment[T]]
	free []uintptr // id liberati, riusati in ordine LIFO
	next uintptr   // prossimo id mai assegnato
	live atomic.Int64
}

const (
	segmentBits = 8
	segmentSize = 1 << segmentBits
	segmentMask = segmentSize - 1
)

type segment[T any] [segmentSize]atomic.Pointer[T]

// add memorizza v e restituisce il suo id (≥ 1).
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

// get restituisce il valore associato a id, nil se l'id non è assegnato.
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

// remove libera id e restituisce il valore che conteneva (nil se l'id non era assegnato).
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

// len restituisce il numero di id assegnati.
func (t *table[T]) len() int { return int(t.live.Load()) }

func split(id uintptr) (seg, slot uintptr) {
	i := id - 1
	return i >> segmentBits, i & segmentMask
}
