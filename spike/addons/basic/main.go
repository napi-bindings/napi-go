// Command basic è l'addon di test principale dello spike napi-go.
// Build: go build -buildmode=c-shared -o basic.node .
package main

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/napi-bindings/napi-go/spike/napi"
)

type codedErr struct{ code, msg string }

func (e codedErr) Error() string  { return e.msg }
func (e codedErr) JSCode() string { return e.code }

func init() {
	// --- Q1/Q3: funzioni sincrone base ---
	napi.Export("hello", func(c napi.CallContext) (napi.Value, error) {
		return c.Env().String("hello from Go")
	})
	napi.Export("noop", func(c napi.CallContext) (napi.Value, error) {
		return napi.Value{}, nil
	})
	napi.Export("add", func(c napi.CallContext) (napi.Value, error) {
		a, err := c.Arg(0).Float64()
		if err != nil {
			return napi.Value{}, err
		}
		b, err := c.Arg(1).Float64()
		if err != nil {
			return napi.Value{}, err
		}
		return c.Env().Float64(a + b)
	})
	napi.Export("echo", func(c napi.CallContext) (napi.Value, error) {
		s, err := c.Arg(0).GoString()
		if err != nil {
			return napi.Value{}, err
		}
		return c.Env().String(s)
	})
	napi.Export("concat", func(c napi.CallContext) (napi.Value, error) {
		a, err := c.Arg(0).GoString()
		if err != nil {
			return napi.Value{}, err
		}
		b, err := c.Arg(1).GoString()
		if err != nil {
			return napi.Value{}, err
		}
		return c.Env().String(a + b)
	})
	// --- Q3: dispatch corretto su molte funzioni (il bug dei tentativi precedenti) ---
	for i := 0; i < 12; i++ {
		i := i
		napi.Export("f"+strconv.Itoa(i), func(c napi.CallContext) (napi.Value, error) {
			return c.Env().Int32(int32(i))
		})
	}
	napi.Export("countArgs", func(c napi.CallContext) (napi.Value, error) {
		return c.Env().Int32(int32(c.Len()))
	})
	napi.Export("sumArgs", func(c napi.CallContext) (napi.Value, error) {
		var sum float64
		for i := 0; i < c.Len(); i++ {
			f, err := c.Arg(i).Float64()
			if err != nil {
				return napi.Value{}, err
			}
			sum += f
		}
		return c.Env().Float64(sum)
	})
	// --- Q3: panic ed errori ---
	napi.Export("panicNow", func(c napi.CallContext) (napi.Value, error) {
		msg, _ := c.Arg(0).GoString()
		panic("boom: " + msg)
	})
	napi.Export("panicNil", func(c napi.CallContext) (napi.Value, error) {
		var m map[string]int
		m["x"] = 1 // runtime error: assignment to entry in nil map
		return napi.Value{}, nil
	})
	napi.Export("fail", func(c napi.CallContext) (napi.Value, error) {
		msg, _ := c.Arg(0).GoString()
		return napi.Value{}, codedErr{code: "E_TEST", msg: msg}
	})
	napi.Export("wrongType", func(c napi.CallContext) (napi.Value, error) {
		_, err := c.Arg(0).Float64() // napi_number_expected
		return napi.Value{}, err
	})
	// Rientranza: Go → JS → Go
	napi.Export("callMe", func(c napi.CallContext) (napi.Value, error) {
		return c.Arg(0).Call(c.Env().Undefined(), c.Arg(1))
	})
	napi.Export("makeObject", func(c napi.CallContext) (napi.Value, error) {
		n, err := c.Arg(0).Int32()
		if err != nil {
			return napi.Value{}, err
		}
		e := c.Env()
		obj, err := e.Object()
		if err != nil {
			return napi.Value{}, err
		}
		arr, err := e.Array(int(n))
		if err != nil {
			return napi.Value{}, err
		}
		for i := int32(0); i < n; i++ {
			v, _ := e.Int32(i * i)
			if err := arr.SetIndex(uint32(i), v); err != nil {
				return napi.Value{}, err
			}
		}
		name, _ := e.String("squares")
		if err := obj.Set("name", name); err != nil {
			return napi.Value{}, err
		}
		if err := obj.Set("values", arr); err != nil {
			return napi.Value{}, err
		}
		return obj, nil
	})

	// --- Q4: Promise da goroutine ---
	napi.Export("sleep", func(c napi.CallContext) (napi.Value, error) {
		ms, err := c.Arg(0).Int32()
		if err != nil {
			return napi.Value{}, err
		}
		jsThread := c.JSThreadID()
		return c.Promise(func(ctx context.Context) (napi.Resolver, error) {
			workThread := napi.ThreadID()
			select {
			case <-time.After(time.Duration(ms) * time.Millisecond):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return func(e napi.Env) (napi.Value, error) {
				o, err := e.Object()
				if err != nil {
					return napi.Value{}, err
				}
				set := func(k string, f float64) {
					v, _ := e.Float64(f)
					_ = o.Set(k, v)
				}
				set("ms", float64(ms))
				set("jsThread", float64(jsThread))
				set("resolveThread", float64(napi.ThreadID())) // deve essere == jsThread
				set("workThread", float64(workThread))         // thread di una goroutine
				return o, nil
			}, nil
		})
	})
	napi.Export("parallelSum", func(c napi.CallContext) (napi.Value, error) {
		workers, _ := c.Arg(0).Int32()
		iters, _ := c.Arg(1).Int32()
		return c.Promise(func(ctx context.Context) (napi.Resolver, error) {
			res := make(chan int64, workers)
			for w := int32(0); w < workers; w++ {
				go func(w int64) {
					var s int64
					for i := int64(0); i < int64(iters); i++ {
						s += (i ^ w) & 7
					}
					res <- s
				}(int64(w))
			}
			var total int64
			for w := int32(0); w < workers; w++ {
				total += <-res
			}
			return func(e napi.Env) (napi.Value, error) { return e.Float64(float64(total)) }, nil
		})
	})
	napi.Export("rejectAsync", func(c napi.CallContext) (napi.Value, error) {
		msg, _ := c.Arg(0).GoString()
		return c.Promise(func(ctx context.Context) (napi.Resolver, error) {
			return nil, codedErr{code: "E_ASYNC", msg: msg}
		})
	})
	napi.Export("panicAsync", func(c napi.CallContext) (napi.Value, error) {
		return c.Promise(func(ctx context.Context) (napi.Resolver, error) {
			panic("async boom")
		})
	})
	// Brucia CPU su tutte le P per ms millisecondi: provoca segnali di preemption (SIGURG).
	napi.Export("burnCPU", func(c napi.CallContext) (napi.Value, error) {
		ms, _ := c.Arg(0).Int32()
		return c.Promise(func(ctx context.Context) (napi.Resolver, error) {
			deadline := time.Now().Add(time.Duration(ms) * time.Millisecond)
			done := make(chan uint64)
			for i := 0; i < runtime.GOMAXPROCS(0); i++ {
				go func() {
					var x uint64
					for time.Now().Before(deadline) {
						for j := 0; j < 1e5; j++ {
							x = x*6364136223846793005 + 1442695040888963407
						}
						_ = make([]byte, 4096) // pressione sul GC Go
					}
					done <- x
				}()
			}
			for i := 0; i < runtime.GOMAXPROCS(0); i++ {
				<-done
			}
			return func(e napi.Env) (napi.Value, error) { return e.Bool(true) }, nil
		})
	})
	// Chiama ripetutamente una funzione JS da goroutine tramite il dispatcher.
	napi.Export("tick", func(c napi.CallContext) (napi.Value, error) {
		n, _ := c.Arg(0).Int32()
		post := c.Poster()
		// La funzione JS va usata solo sul thread JS: qui la catturiamo come napi_value
		// valida solo nello scope corrente, quindi la rileggiamo da un oggetto globale.
		return c.Promise(func(ctx context.Context) (napi.Resolver, error) {
			errs := 0
			for i := int32(0); i < n; i++ {
				i := i
				if err := post(func(e napi.Env) {
					g, _ := e.Global()
					fn, err := g.Get("__napiGoTick")
					if err != nil {
						return
					}
					v, _ := e.Int32(i)
					_, _ = fn.Call(e.Undefined(), v)
				}); err != nil {
					errs++
				}
			}
			return func(e napi.Env) (napi.Value, error) { return e.Int32(int32(errs)) }, nil
		})
	})

	// --- diagnostica ---
	napi.Export("threadInfo", func(c napi.CallContext) (napi.Value, error) {
		e := c.Env()
		o, err := e.Object()
		if err != nil {
			return napi.Value{}, err
		}
		cur, _ := e.Float64(float64(napi.ThreadID()))
		js, _ := e.Float64(float64(c.JSThreadID()))
		runs, _ := e.Float64(float64(c.DispatchRuns()))
		_ = o.Set("current", cur)
		_ = o.Set("jsThread", js)
		_ = o.Set("dispatchRuns", runs)
		return o, nil
	})
	napi.Export("info", infoFn("basic"))
	napi.Export("goMallocs", func(c napi.CallContext) (napi.Value, error) {
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		return c.Env().Float64(float64(ms.Mallocs))
	})
	napi.Export("goGC", func(c napi.CallContext) (napi.Value, error) {
		runtime.GC()
		return napi.Value{}, nil
	})
}

func infoFn(name string) napi.Callback {
	return func(c napi.CallContext) (napi.Value, error) {
		e := c.Env()
		o, err := e.Object()
		if err != nil {
			return napi.Value{}, err
		}
		nv, err := e.NapiVersion()
		if err != nil {
			return napi.Value{}, err
		}
		node, err := e.NodeVersion()
		if err != nil {
			return napi.Value{}, err
		}
		req, opt := napi.SymbolStats()
		live, total := napi.EnvStats()
		str := func(k, v string) error {
			sv, err := e.String(v)
			if err != nil {
				return err
			}
			return o.Set(k, sv)
		}
		num := func(k string, f float64) error {
			fv, err := e.Float64(f)
			if err != nil {
				return err
			}
			return o.Set(k, fv)
		}
		boolean := func(k string, b bool) error {
			bv, err := e.Bool(b)
			if err != nil {
				return err
			}
			return o.Set(k, bv)
		}
		errs := []error{
			str("addon", name),
			num("napiVersion", float64(nv)),
			str("nodeVersion", node),
			str("goVersion", runtime.Version()),
			str("platform", runtime.GOOS+"/"+runtime.GOARCH),
			num("requiredSymbols", float64(req)),
			num("optionalSymbols", float64(opt)),
			boolean("hasSymbolFor", napi.HasSymbol("node_api_symbol_for")),
			num("liveEnvs", float64(live)),
			num("totalEnvs", float64(total)),
			str("goAddr", fmt.Sprintf("%p", &exportMarker)), // distingue i runtime Go
		}
		if err := errors.Join(errs...); err != nil {
			return napi.Value{}, err
		}
		return o, nil
	}
}

var exportMarker int

func main() {}

// Percorso tipizzato (conversioni in C, un solo attraversamento cgo).
func init() {
	napi.ExportTyped("addTyped", napi.Fn2(func(a, b float64) (float64, error) { return a + b, nil }))
	napi.ExportTyped("echoTyped", napi.Fn1(func(s string) (string, error) { return s, nil }))
	napi.ExportTyped("repeatTyped", napi.Fn2(func(s string, n int32) (string, error) {
		if n < 0 {
			return "", codedErr{code: "E_RANGE", msg: "n must be >= 0"}
		}
		return strings.Repeat(s, int(n)), nil
	}))
	napi.ExportTyped("notTyped", napi.Fn1(func(b bool) (bool, error) { return !b, nil }))
	napi.ExportTyped("panicTyped", napi.Fn1(func(i int32) (int32, error) { panic("typed boom") }))
}
