package napi

import (
	"errors"
	"math/big"
	"sync"
	"testing"
)

// Più di un segmento, per provare la crescita della directory.
const manyEntries = segmentSize*3 + 7

func TestTableAddGetRemove(t *testing.T) {
	var tb table[int]
	if tb.get(0) != nil || tb.remove(0) != nil {
		t.Fatal("id 0 must never be assigned")
	}
	if tb.get(1) != nil || tb.remove(1) != nil {
		t.Fatal("empty table must not return values")
	}
	ids := make([]uintptr, manyEntries)
	for i := range ids {
		v := i
		ids[i] = tb.add(&v)
		if ids[i] == 0 {
			t.Fatalf("add returned id 0")
		}
	}
	if tb.len() != manyEntries {
		t.Fatalf("len = %d, want %d", tb.len(), manyEntries)
	}
	for i, id := range ids {
		if p := tb.get(id); p == nil || *p != i {
			t.Fatalf("get(%d) = %v, want %d", id, p, i)
		}
	}
	// Id fuori dalla directory.
	if tb.get(ids[len(ids)-1]+segmentSize*2) != nil {
		t.Fatal("get beyond the directory must return nil")
	}
	removed := ids[segmentSize]
	if p := tb.remove(removed); p == nil || *p != segmentSize {
		t.Fatalf("remove returned %v", p)
	}
	if tb.get(removed) != nil || tb.remove(removed) != nil {
		t.Fatal("a removed id must be empty, and removing it twice must return nil")
	}
	if tb.len() != manyEntries-1 {
		t.Fatalf("len after remove = %d", tb.len())
	}
	// L'id liberato viene riusato.
	v := -1
	if id := tb.add(&v); id != removed {
		t.Fatalf("freed id not reused: got %d, want %d", id, removed)
	}
}

func TestTableConcurrent(t *testing.T) {
	const (
		writers   = 4
		readers   = 4
		perWriter = segmentSize * 2
	)
	var tb table[int]
	stable := make([]uintptr, perWriter)
	for i := range stable {
		v := i
		stable[i] = tb.add(&v)
	}
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				for i, id := range stable {
					if p := tb.get(id); p == nil || *p != i {
						t.Errorf("stable id %d changed: %v", id, p)
						return
					}
				}
			}
		}()
	}
	var ww sync.WaitGroup
	for w := 0; w < writers; w++ {
		ww.Add(1)
		go func(w int) {
			defer ww.Done()
			ids := make([]uintptr, 0, perWriter)
			for i := 0; i < perWriter; i++ {
				v := w*perWriter + i
				ids = append(ids, tb.add(&v))
			}
			for _, id := range ids {
				if tb.remove(id) == nil {
					t.Errorf("writer %d: id %d missing", w, id)
				}
			}
		}(w)
	}
	ww.Wait()
	close(stop)
	wg.Wait()
	if tb.len() != perWriter {
		t.Fatalf("len = %d, want %d", tb.len(), perWriter)
	}
}

func TestBigIntWords(t *testing.T) {
	cases := []string{"0", "1", "-1", "18446744073709551615", "18446744073709551616",
		"-340282366920938463463374607431768211457"}
	for _, c := range cases {
		b, ok := new(big.Int).SetString(c, 10)
		if !ok {
			t.Fatalf("bad case %s", c)
		}
		words := bigToWords(b)
		got := wordsToBig(words)
		if b.Sign() < 0 {
			got.Neg(got)
		}
		if got.Cmp(b) != 0 {
			t.Fatalf("%s: round trip gave %s (words %v)", c, got, words)
		}
	}
}

func TestErrorJSCode(t *testing.T) {
	cases := []struct {
		err  *Error
		want string
	}{
		{&Error{Status: StatusInvalidArg, Message: "Invalid argument"}, "ERR_NAPI_INVALID_ARG"},
		{&Error{Status: StatusUnavailable}, "ERR_NAPI_UNAVAILABLE"},
		{&Error{Status: StatusInvalidArg, Code: "MINE"}, "MINE"},
		{TypeError("", "x"), ""},
		{RangeError("E_RANGE", "x"), "E_RANGE"},
	}
	for _, c := range cases {
		if got := c.err.JSCode(); got != c.want {
			t.Errorf("JSCode(%+v) = %q, want %q", c.err, got, c.want)
		}
	}
	copyOfPending := *ErrPendingException
	if !errors.Is(&copyOfPending, ErrPendingException) {
		t.Error("a copy of ErrPendingException must match with errors.Is")
	}
	if errors.Is(&Error{Status: StatusInvalidArg}, ErrPendingException) {
		t.Error("only pending exceptions must match ErrPendingException")
	}
}
