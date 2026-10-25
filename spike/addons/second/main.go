// Command second è un secondo addon napi-go, compilato separatamente: caricarlo
// insieme a `basic` significa avere DUE runtime Go nello stesso processo Node (Q5).
package main

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/napi-bindings/napi-go/spike/napi"
)

var marker int

func init() {
	napi.Export("name", func(c napi.CallContext) (napi.Value, error) {
		return c.Env().String("second")
	})
	napi.Export("mul", func(c napi.CallContext) (napi.Value, error) {
		a, err := c.Arg(0).Float64()
		if err != nil {
			return napi.Value{}, err
		}
		b, err := c.Arg(1).Float64()
		if err != nil {
			return napi.Value{}, err
		}
		return c.Env().Float64(a * b)
	})
	napi.Export("sleep", func(c napi.CallContext) (napi.Value, error) {
		ms, _ := c.Arg(0).Int32()
		return c.Promise(func(ctx context.Context) (napi.Resolver, error) {
			time.Sleep(time.Duration(ms) * time.Millisecond)
			return func(e napi.Env) (napi.Value, error) { return e.Int32(ms) }, nil
		})
	})
	napi.Export("churn", func(c napi.CallContext) (napi.Value, error) {
		// Alloca molto e forza GC: stressa il secondo runtime mentre l'altro lavora.
		n, _ := c.Arg(0).Int32()
		return c.Promise(func(ctx context.Context) (napi.Resolver, error) {
			var keep [][]byte
			for i := int32(0); i < n; i++ {
				keep = append(keep, make([]byte, 1<<14))
				if len(keep) > 256 {
					keep = keep[:0]
					runtime.GC()
				}
			}
			return func(e napi.Env) (napi.Value, error) { return e.Bool(true) }, nil
		})
	})
	napi.Export("goAddr", func(c napi.CallContext) (napi.Value, error) {
		return c.Env().String(fmt.Sprintf("%p %s", &marker, runtime.Version()))
	})
}

func main() {}
