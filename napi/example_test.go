package napi_test

// Examples for the package documentation. They are compiled by go test (not run: an addon runs
// only inside Node.js); tests/e2e has the same patterns tested end to end.

import (
	"context"
	"errors"
	"time"

	"github.com/napi-bindings/napi-go/napi"
)

// Functions and constants with Layer 2: arguments and results are converted from their Go types.
// napi-go gen writes the same registrations from //napi:export annotations.
func ExampleExport() {
	type Point struct {
		X, Y float64
	}
	napi.Export("add", napi.Func2(func(a, b float64) float64 { return a + b }))
	napi.Export("midpoint", napi.Func2(func(p, q Point) Point {
		return Point{(p.X + q.X) / 2, (p.Y + q.Y) / 2}
	}))
	// A returned error is thrown in JS.
	napi.Export("parsePort", napi.FuncE1(func(n int) (uint16, error) {
		if n <= 0 || n > 65535 {
			return 0, napi.RangeError("ERR_PORT", "port out of range")
		}
		return uint16(n), nil
	}))
	napi.Export("VERSION", napi.Const("1.0.0"))
}

// Counter is the Go value behind the JS class of ExampleNewClass.
type Counter struct{ n int }

func (c *Counter) Add(delta int) int {
	c.n += delta
	return c.n
}

// A JS class backed by a Go value: new Counter(start), counter.add(n), counter.value.
func ExampleNewClass() {
	newCounter := func(start int) (*Counter, error) {
		if start < 0 {
			return nil, errors.New("start must not be negative")
		}
		return &Counter{n: start}, nil
	}
	napi.Export("Counter", napi.NewClass(napi.Ctor1(newCounter)).
		Method("add", napi.Method1((*Counter).Add)).
		Accessor("value", napi.Accessor(func(c *Counter) int { return c.n }, nil)))
}

// An asynchronous function: JS gets a Promise, the Go function runs in a goroutine, and ctx is
// canceled when the env is destroyed (worker terminated, process exiting).
func ExampleAsync1() {
	napi.Export("sleep", napi.Async1(func(ctx context.Context, ms int) (string, error) {
		select {
		case <-time.After(time.Duration(ms) * time.Millisecond):
			return "done", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}))
}

// Calling a JS function from goroutines and receiving its converted result: watch(fn) calls fn
// with the numbers 1 to 3 from a goroutine and resolves with the sum of the results.
func ExampleNewJSFunc() {
	napi.Export("watch", napi.FuncE2(func(env napi.Env, fn napi.Function) (napi.Promise, error) {
		square, err := napi.NewJSFunc[float64, float64](env, fn, napi.ThreadsafeFunctionOptions{})
		if err != nil {
			return napi.Promise{}, err
		}
		return env.Async(func(ctx context.Context) (napi.Resolver, error) {
			defer square.Release()
			sum := 0.0
			for i := 1; i <= 3; i++ {
				r, err := square.Call(ctx, float64(i))
				if err != nil {
					return nil, err
				}
				sum += r
			}
			return func(env napi.Env) (napi.Value, error) { return napi.ToValue(env, sum) }, nil
		})
	}))
}

// Layer 1: values, functions and errors by hand, as with node-addon-api.
func ExampleInit() {
	napi.Init(func(env napi.Env, exports napi.Object) (napi.Value, error) {
		hello, err := env.NewFunction("hello", func(c napi.CallContext) (napi.Value, error) {
			name, err := c.Arg(0).GoString()
			if err != nil {
				return napi.Value{}, err // a TypeError in JS
			}
			s, err := c.Env().NewString("hello " + name)
			return s.Value, err
		})
		if err != nil {
			return napi.Value{}, err
		}
		return napi.Value{}, exports.Set("hello", hello.Value)
	})
}

// A Promise settled from a goroutine with Layer 1: the work runs without Env and returns a
// Resolver, which builds the JS result on the JS thread.
func ExampleEnv_Async() {
	napi.Init(func(env napi.Env, exports napi.Object) (napi.Value, error) {
		compute, err := env.NewFunction("compute", func(c napi.CallContext) (napi.Value, error) {
			p, err := c.Env().Async(func(ctx context.Context) (napi.Resolver, error) {
				result := 6 * 7 // work without napi
				return func(env napi.Env) (napi.Value, error) {
					return env.NewInt64(int64(result))
				}, nil
			})
			return p.Value, err
		})
		if err != nil {
			return napi.Value{}, err
		}
		return napi.Value{}, exports.Set("compute", compute.Value)
	})
}
