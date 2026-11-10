// Command framework is the e2e test addon of Layer 2: functions, conversions, async functions and
// classes declared with napi.Export and the generic wrappers, without hand-written callbacks.
package main

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/napi-bindings/napi-go/napi"
)

// ---- types used by the conversions ----

// Celsius is a named numeric type: converted like its underlying float64.
type Celsius float64

// Base is embedded in User: its fields are promoted.
type Base struct {
	ID int
}

// Address uses json tags, honored when there is no napi tag.
type Address struct {
	City string `json:"city"`
	ZIP  string `json:"zip,omitempty"`
}

// User covers tags, optional fields, nested structs and skipped fields.
type User struct {
	Base
	Name    string
	Age     uint8    `napi:"age"`
	Email   *string  // nullable
	Tags    []string `napi:",omitempty"`
	Address Address
	Secret  string `napi:"-"`
	private int
}

// Tree is recursive.
type Tree struct {
	Value    int
	Children []Tree `napi:",omitempty"`
}

// Point converts itself: a JS [x, y] array.
type Point struct{ X, Y float64 }

func (p Point) ToJS(env napi.Env) (napi.Value, error) { return napi.ToValue(env, [2]float64{p.X, p.Y}) }

func (p *Point) FromJS(v napi.Value) error {
	xy, err := napi.FromValue[[2]float64](v)
	if err != nil {
		return err
	}
	p.X, p.Y = xy[0], xy[1]
	return nil
}

// ---- classes ----

// finalized counts Counter values whose last instance was garbage-collected.
var finalized atomic.Int64

// retained is a Counter kept by Go (retain, retained, forget).
var retained atomic.Pointer[Counter]

// Counter is an exported class.
type Counter struct{ n int }

func newCounter(start int) (*Counter, error) {
	if start < 0 {
		return nil, napi.RangeError("E_NEGATIVE_START", "start must not be negative")
	}
	return &Counter{n: start}, nil
}

func (c *Counter) Inc()               { c.n++ }
func (c *Counter) Add(d int) int      { c.n += d; return c.n }
func (c *Counter) Self() *Counter     { return c }
func (c *Counter) Clone() *Counter    { return &Counter{n: c.n} }
func (c *Counter) Finalize(napi.Env)  { finalized.Add(1) }
func (c *Counter) Fail() (int, error) { return 0, errors.New("counter failure") }

// Token is a registered class that is not exported: its instances come only from Go.
type Token struct{ id string }

// ---- exports ----

const maxCounter = 100

var procCalls atomic.Int64

func init() {
	// Numbers.
	napi.Export("add", napi.Func2(func(a, b float64) float64 { return a + b }))
	napi.Export("addInt32", napi.Func2(func(a, b int32) int32 { return a + b }))
	napi.Export("int8", napi.Func1(func(v int8) int8 { return v }))
	napi.Export("uint8", napi.Func1(func(v uint8) uint8 { return v }))
	napi.Export("uint32", napi.Func1(func(v uint32) uint32 { return v }))
	napi.Export("int64", napi.Func1(func(v int64) int64 { return v }))
	napi.Export("uint64", napi.Func1(func(v uint64) string { return fmt.Sprint(v) }))
	napi.Export("float32", napi.Func1(func(v float32) float32 { return v }))
	napi.Export("celsius", napi.Func1(func(c Celsius) float64 { return float64(c)*9/5 + 32 }))
	napi.Export("double", napi.Func1(func(b *big.Int) *big.Int { return new(big.Int).Lsh(b, 1) }))

	// Strings and booleans.
	napi.Export("greet", napi.Func1(func(s string) string { return "hello " + s }))
	napi.Export("not", napi.Func1(func(b bool) bool { return !b }))
	napi.Export("repeat", napi.Func2(strings.Repeat))

	// concat3 receives three strings: on the typed path they share one C buffer, and those that
	// do not fit are read by Go.
	napi.Export("concat3", napi.Func3(func(a, b, c string) string { return a + "|" + b + "|" + c }))
	napi.Export("sum8", napi.Func8(func(a, b, c, d, e, f, g, h int) int { return a + b + c + d + e + f + g + h }))

	// Bytes.
	napi.Export("sumBytes", napi.Func1(func(b []byte) int {
		n := 0
		for _, x := range b {
			n += int(x)
		}
		return n
	}))
	napi.Export("upper", napi.Func1(func(b []byte) []byte { return []byte(strings.ToUpper(string(b))) }))
	// zeroFirst writes into the Buffer it receives: the []byte aliases its memory.
	napi.Export("zeroFirst", napi.Proc1(func(b []byte) {
		if len(b) > 0 {
			b[0] = 0
		}
	}))

	// Collections.
	napi.Export("sum", napi.Func1(func(xs []float64) float64 {
		t := 0.0
		for _, x := range xs {
			t += x
		}
		return t
	}))
	napi.Export("doubleAll", napi.Func1(func(xs []int) []int {
		out := make([]int, len(xs))
		for i, x := range xs {
			out[i] = 2 * x
		}
		return out
	}))
	napi.Export("nilSlice", napi.Func0(func() []string { return nil }))
	napi.Export("reverse3", napi.Func1(func(a [3]string) [3]string { return [3]string{a[2], a[1], a[0]} }))
	napi.Export("sumValues", napi.Func1(func(m map[string]int) int {
		t := 0
		for _, v := range m {
			t += v
		}
		return t
	}))
	napi.Export("countWords", napi.Func1(func(s string) map[string]int {
		m := map[string]int{}
		for _, w := range strings.Fields(s) {
			m[w]++
		}
		return m
	}))
	napi.Export("invert", napi.Func1(func(m map[int]string) map[string]int {
		out := map[string]int{}
		for k, v := range m {
			out[v] = k
		}
		return out
	}))
	napi.Export("matrix", napi.Func1(func(m [][]int) int {
		t := 0
		for _, row := range m {
			for _, x := range row {
				t += x
			}
		}
		return t
	}))

	// Structs.
	napi.Export("echoUser", napi.Func1(func(u User) User { return u }))
	napi.Export("describeUser", napi.Func1(func(u User) string {
		email := "<none>"
		if u.Email != nil {
			email = *u.Email
		}
		return fmt.Sprintf("%d %s %d %s %v %s/%s %q", u.ID, u.Name, u.Age, email, u.Tags, u.Address.City, u.Address.ZIP, u.Secret)
	}))
	napi.Export("sumTree", napi.Func1(sumTree))
	napi.Export("makeTree", napi.Func1(func(depth int) Tree { return makeTree(depth) }))

	// Pointers, any, Either, time, custom conversions.
	napi.Export("maybe", napi.Func1(func(p *int) string {
		if p == nil {
			return "nil"
		}
		return fmt.Sprint(*p)
	}))
	napi.Export("maybeOut", napi.Func1(func(b bool) *string {
		if !b {
			return nil
		}
		s := "set"
		return &s
	}))
	napi.Export("roundTripAny", napi.Func1(func(v any) any { return v }))
	napi.Export("goTypeOf", napi.Func1(func(v any) string { return describeAny(v) }))
	napi.Export("mixed", napi.Func0(func() any {
		return map[string]any{"n": 1, "s": "x", "list": []any{true, nil, 2.5}, "nested": map[string]int{"a": 1}}
	}))
	napi.Export("stringOrBytes", napi.Func1(func(e napi.Either[string, []byte]) string {
		if s, ok := e.A(); ok {
			return "string:" + s
		}
		b, _ := e.B()
		return fmt.Sprintf("bytes:%d", len(b))
	}))
	napi.Export("eitherOut", napi.Func1(func(n int) napi.Either[int, string] {
		if n < 0 {
			return napi.EitherB[int]("negative")
		}
		return napi.EitherA[int, string](n)
	}))
	napi.Export("addHour", napi.Func1(func(t time.Time) time.Time { return t.Add(time.Hour) }))
	napi.Export("midpoint", napi.Func2(func(a, b Point) Point { return Point{(a.X + b.X) / 2, (a.Y + b.Y) / 2} }))

	// Injected parameters and napi values.
	napi.Export("exclaim", napi.FuncE2(func(env napi.Env, s string) (napi.Value, error) {
		r, err := env.NewString(s + "!")
		return r.Value, err
	}))
	napi.Export("argCount", napi.Func2(func(c napi.CallContext, first int) int { return c.Len()*100 + first }))
	napi.Export("callWith", napi.FuncE2(func(fn napi.Function, v napi.Value) (napi.Value, error) {
		return fn.Call(napi.Value{}, v)
	}))
	napi.Export("keysOf", napi.FuncE1(func(o napi.Object) ([]string, error) {
		keys, err := napi.FromValue[[]string](mustKeys(o))
		sort.Strings(keys)
		return keys, err
	}))

	// Errors.
	napi.Export("checkPositive", napi.FuncE1(func(n int) (int, error) {
		if n <= 0 {
			return 0, napi.RangeError("E_NOT_POSITIVE", fmt.Sprintf("%d is not positive", n))
		}
		return n, nil
	}))
	napi.Export("plainError", napi.ProcE0(func() error { return errors.New("plain failure") }))
	napi.Export("panics", napi.Func0(func() int { panic("framework boom") }))
	napi.Export("proc", napi.Proc1(func(n int) { procCalls.Add(int64(n)) }))
	napi.Export("procCalls", napi.Func0(procCalls.Load))
	napi.Export("badDefinitions", napi.FuncE1(badDefinitions))

	// Async.
	napi.Export("addAsync", napi.Async2(func(_ context.Context, a, b float64) (float64, error) { return a + b, nil }))
	napi.Export("failAsync", napi.Async1(func(_ context.Context, code string) (int, error) {
		return 0, napi.TypeError(code, "async failure")
	}))
	napi.Export("userAsync", napi.Async1(func(_ context.Context, name string) (User, error) {
		return User{Base: Base{ID: 7}, Name: name, Age: 30, Address: Address{City: "Rome"}}, nil
	}))
	// sumBytesLater reads the bytes after a delay: they are a copy, so later writes to the Buffer
	// in JS do not change the result.
	napi.Export("sumBytesLater", napi.Async2(func(ctx context.Context, b []byte, ms int) (int, error) {
		select {
		case <-time.After(time.Duration(ms) * time.Millisecond):
		case <-ctx.Done():
			return 0, ctx.Err()
		}
		n := 0
		for _, x := range b {
			n += int(x)
		}
		return n, nil
	}))
	napi.Export("procAsync", napi.AsyncProc1(func(_ context.Context, n int) error {
		if n < 0 {
			return errors.New("negative")
		}
		procCalls.Add(int64(n))
		return nil
	}))
	napi.Export("counterAsync", napi.Async1(func(_ context.Context, n int) (*Counter, error) { return &Counter{n: n}, nil }))

	// JSFunc: goroutines calling JS and receiving the converted result.
	napi.Export("callFromGo", napi.FuncE3(callFromGo))
	napi.Export("callNoArgs", napi.FuncE2(func(env napi.Env, fn napi.Function) (napi.Promise, error) {
		f, err := napi.NewJSFunc[struct{}, struct{}](env, fn, napi.ThreadsafeFunctionOptions{})
		if err != nil {
			return napi.Promise{}, err
		}
		return env.Async(func(ctx context.Context) (napi.Resolver, error) {
			defer release(f.Release)
			_, err := f.Call(ctx, struct{}{})
			return nil, err
		})
	}))
	// abortWhileQueued queues calls on a function whose JS thread is busy, aborts it and reports
	// the errors of the waiting calls.
	napi.Export("abortWhileQueued", napi.FuncE2(abortWhileQueued))
	napi.Export("badJSFuncs", napi.FuncE2(func(env napi.Env, fn napi.Function) ([]string, error) {
		var out []string
		if _, err := napi.NewJSFunc[napi.Value, int](env, fn, napi.ThreadsafeFunctionOptions{}); err != nil {
			out = append(out, err.Error())
		}
		if _, err := napi.NewJSFunc[int, napi.Object](env, fn, napi.ThreadsafeFunctionOptions{}); err != nil {
			out = append(out, err.Error())
		}
		if _, err := napi.NewJSFunc[chan int, int](env, fn, napi.ThreadsafeFunctionOptions{}); err != nil {
			out = append(out, err.Error())
		}
		return out, nil
	}))

	// Constants.
	napi.Export("version", napi.Const("1.0.0"))
	napi.Export("limits", napi.Const(map[string]int{"max": maxCounter, "min": 0}))

	// Classes.
	napi.Export("Counter", napi.NewClass(napi.Ctor1(newCounter)).
		Method("inc", napi.MethodProc0((*Counter).Inc)).
		Method("add", napi.Method1((*Counter).Add)).
		Method("self", napi.Method0((*Counter).Self)).
		Method("clone", napi.Method0((*Counter).Clone)).
		Method("fail", napi.MethodE0((*Counter).Fail)).
		Accessor("value", napi.Accessor(func(c *Counter) int { return c.n }, func(c *Counter, v int) { c.n = v })).
		Accessor("double", napi.Accessor(func(c *Counter) int { return 2 * c.n }, nil)).
		Static("from", napi.Func1(func(n int) *Counter { return &Counter{n: n} })).
		StaticConst("MAX", maxCounter))
	napi.Export("counterValue", napi.Func1(func(c *Counter) int {
		if c == nil {
			return -1
		}
		return c.n
	}))
	napi.Export("finalized", napi.Func0(finalized.Load))
	// retain keeps a *Counter in Go after JS drops its instance; retained returns it again.
	napi.Export("retain", napi.Proc1(func(c *Counter) { retained.Store(c) }))
	napi.Export("retained", napi.Func0(retained.Load))
	napi.Export("forget", napi.Proc0(func() { retained.Store(nil) }))
	napi.RegisterClass("Token", napi.NewClass(napi.CtorDef[Token]{}).
		Method("id", napi.Method0(func(t *Token) string { return t.id })))
	napi.Export("newToken", napi.Func1(func(id string) *Token { return &Token{id: id} }))
	napi.Export("tokens", napi.Func1(func(ids []string) []*Token {
		out := make([]*Token, len(ids))
		for i, id := range ids {
			out[i] = &Token{id: id}
		}
		return out
	}))

	// Measurements.
	napi.Export("goMallocs", napi.Func0(func() uint64 {
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		return ms.Mallocs
	}))
	napi.Export("noop", napi.Proc0(func() {}))
}

// jsFuncArg is the argument passed to JS by callFromGo.
type jsFuncArg struct {
	N    int    `napi:"n"`
	Note string `napi:"note"`
}

// callFromGo calls fn from a goroutine count times with {n, note} and resolves with the results,
// converted to strings, in order.
func callFromGo(env napi.Env, fn napi.Function, count int) (napi.Promise, error) {
	f, err := napi.NewJSFunc[jsFuncArg, string](env, fn, napi.ThreadsafeFunctionOptions{})
	if err != nil {
		return napi.Promise{}, err
	}
	return env.Async(func(ctx context.Context) (napi.Resolver, error) {
		defer release(f.Release)
		out := make([]string, count)
		for i := range out {
			r, err := f.Call(ctx, jsFuncArg{N: i, Note: "from Go"})
			if err != nil {
				return nil, err
			}
			out[i] = r
		}
		return func(env napi.Env) (napi.Value, error) { return napi.ToValue(env, out) }, nil
	})
}

// release closes a JSFunc from a goroutine; a failure means it was already closed.
func release(fn func() error) {
	if err := fn(); err != nil && !errors.Is(err, napi.ErrClosing) {
		panic(err)
	}
}

// waitingCalls is how many calls abortWhileQueued leaves waiting in the queue.
const waitingCalls = 3

func abortWhileQueued(env napi.Env, fn napi.Function) (napi.Promise, error) {
	f, err := napi.NewJSFunc[int, int](env, fn, napi.ThreadsafeFunctionOptions{})
	if err != nil {
		return napi.Promise{}, err
	}
	return env.Async(func(ctx context.Context) (napi.Resolver, error) {
		errs := make(chan string, waitingCalls)
		for i := 0; i < waitingCalls; i++ {
			go func(n int) {
				_, err := f.Call(ctx, n)
				errs <- fmt.Sprint(err)
			}(i)
		}
		// The calls are queued: the JS thread is blocked in Atomics.wait until the Promise of
		// this function is awaited, which happens after the abort.
		time.Sleep(abortDelay)
		if err := f.Abort(); err != nil {
			return nil, err
		}
		out := make([]string, waitingCalls)
		for i := range out {
			out[i] = <-errs
		}
		sort.Strings(out)
		return func(env napi.Env) (napi.Value, error) { return napi.ToValue(env, out) }, nil
	})
}

// abortDelay lets the goroutines of abortWhileQueued queue their calls.
const abortDelay = 50 * time.Millisecond

func mustKeys(o napi.Object) napi.Value {
	k, err := o.Keys()
	if err != nil {
		return napi.Value{}
	}
	return k.Value
}

func sumTree(t Tree) int {
	n := t.Value
	for _, c := range t.Children {
		n += sumTree(c)
	}
	return n
}

func makeTree(depth int) Tree {
	t := Tree{Value: depth}
	if depth > 0 {
		t.Children = []Tree{makeTree(depth - 1), makeTree(depth - 1)}
	}
	return t
}

func describeAny(v any) string {
	switch x := v.(type) {
	case nil:
		return "nil"
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = describeAny(e)
		}
		return "[" + strings.Join(parts, " ") + "]"
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = k + ":" + describeAny(x[k])
		}
		return "{" + strings.Join(parts, " ") + "}"
	case time.Time:
		return "time.Time"
	case *big.Int:
		return "*big.Int(" + x.String() + ")"
	case napi.Function:
		return "napi.Function"
	}
	return fmt.Sprintf("%T", v)
}

// badDefinitions returns the errors of definitions whose types cannot be converted: they are
// reported when the function is created (for exports: when the module loads).
func badDefinitions(env napi.Env) ([]string, error) {
	defs := []napi.FuncDef{
		napi.Func1(func(chan int) int { return 0 }),
		napi.Func0(func() func() { return nil }),
		napi.Func1(func(map[float64]int) int { return 0 }),
		napi.Async1(func(_ context.Context, f napi.Function) (int, error) { return 0, nil }),
		napi.Async0(func(context.Context) (napi.Object, error) { return napi.Object{}, nil }),
		napi.Async1(func(_ context.Context, e napi.Env) (int, error) { return 0, nil }),
		napi.Func1(func(error) int { return 0 }),
	}
	out := make([]string, len(defs))
	for i, d := range defs {
		_, err := d.New(env, "bad")
		if err == nil {
			return nil, fmt.Errorf("definition %d was accepted", i)
		}
		out[i] = err.Error()
	}
	return out, nil
}

func main() {}
