# Writing addons

An addon is a Go `main` package built with `-buildmode=c-shared`. The usual way to write one is
to annotate Go declarations and let `napi-go gen` generate the registrations (Layer 2). This
guide shows the common cases; the [`napi` reference](../napi/README.md) and the
[annotation reference](../internal/codegen/README.md) have the details.

```go
package main

//go:generate go tool napi-go gen

// Add returns a + b.
//
//napi:export
func Add(a, b float64) float64 { return a + b }

func main() {} // required by -buildmode=c-shared; it does not run
```

## Functions

`//napi:export` exports a function under its Go name in lowerCamelCase (`ParseURL` →
`parseURL`), or under the name that follows the annotation (`//napi:export parse`). Functions
take up to 8 JS parameters and return nothing, a value, an error, or a value and an error.

A parameter of type `napi.Env` or `napi.CallContext` is filled from the call and takes no JS
argument: use it to create JS values or read `this` and the raw arguments. A missing JS argument
is `undefined`; extra arguments are ignored.

```go
//napi:export
func Lines(env napi.Env, text string) (napi.Array, error) { … }
```

## Types

Arguments are converted from their Go type, and checked: a wrong type throws a TypeError with
code `ERR_INVALID_ARG_TYPE` and the position of the value (`argument 1: tags[2]: expected
string, got number`). Integers must be integers in range, never truncated.

| Go | JS |
|---|---|
| `bool`, `string` | boolean, string |
| `float64`, `float32`, integer types | number (64-bit integers also accept BigInt) |
| `*big.Int` | BigInt |
| `[]byte` | Buffer, typed array, ArrayBuffer: a view of the JS memory, valid during the call |
| `[]T`, `[N]T`, `map[string]T` | array, object |
| struct | object with the exported fields |
| `*T` | `null` or `undefined` is `nil` |
| `time.Time` | Date |
| `any` | the natural Go type of the value |
| `napi.Either[A, B]` | a value of either type |
| `napi.Function`, `napi.Object`, `napi.Value`, … | passed as is |

The [type mapping](../napi/README.md#type-mapping) lists every case. A type can convert itself
by implementing `FromJS` and `ToJS`.

## Objects

Struct fields are named with their `napi` tag, else their `json` tag, else their Go name in
lowerCamelCase. `napi:"-"` skips a field; `omitempty`, a pointer, or `any` make it optional.

```go
type User struct {
	ID    int64   `napi:"id"`
	Name  string
	Email *string // optional: undefined or null is nil
	token string  // unexported: not converted
}
```

A struct used by an exported function is declared in `index.d.ts` as an interface. Annotate it
with `//napi:object` to declare it even when no function uses it.

## Errors

A returned error is thrown in JS, and a panic becomes an Error with code `GO_PANIC`.

| Returned error | Thrown as |
|---|---|
| `napi.TypeError(code, msg)`, `napi.RangeError`, `napi.SyntaxError` | that constructor, with `code` |
| an error with a `JSCode() string` method | `Error` with that code |
| any other error | `Error` with message `err.Error()` |

Wrapped errors (`fmt.Errorf("…: %w", err)`) keep their kind and code.

```go
//napi:export
func ParsePort(s string) (uint16, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 || n > 65535 {
		return 0, napi.RangeError("ERR_INVALID_PORT", "invalid port "+s)
	}
	return uint16(n), nil
}
```

## Async functions

A function whose first parameter is a `context.Context` returns a Promise. Its arguments are
converted on the JS thread (a wrong argument throws right away), it runs in a goroutine, and its
result is converted on the JS thread when it returns.

```go
// Fetch downloads url.
//
//napi:export
func Fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
```

- `ctx` is canceled when the env is destroyed: a worker thread terminated, or the process
  exiting. Stop the work then; its result is dropped.
- The parameters and the result must not contain napi values: they are only valid on the JS
  thread. `[]byte` arguments are copied for the goroutine.
- Node.js keeps running until the Promise settles.
- Many Promises settled together cost little: napi-go wakes the JS thread once for a batch
  ([benchmarks](../bench/README.md)).

## Classes

```go
// Counter counts.
//
//napi:class
type Counter struct{ n int }

// NewCounter creates a counter starting at start.
//
//napi:constructor
func NewCounter(start int) (*Counter, error) { return &Counter{n: start}, nil }

// Add adds delta and returns the new value.
func (c *Counter) Add(delta int) int {
	c.n += delta
	return c.n
}

// Value is the current value.
//
//napi:getter
func (c *Counter) Value() int { return c.n }

// SetValue sets the value.
//
//napi:setter
func (c *Counter) SetValue(v int) { c.n = v }
```

```js
const c = new Counter(1)
c.add(2)       // 3
c.value = 10
```

- Every exported method of `*T` is a method of the class, unless marked `//napi:skip`.
  `//napi:export Counter.fromString` on a function makes a static method.
- A `*T` parameter accepts only instances of the class; a `*T` result returns the instance that
  wraps it, or creates one: in an env the same `*T` is always the same JS object.
- `func (c *T) Finalize(env napi.Env)` runs when the last instance wrapping the value is garbage
  collected. It must not call JS.
- JS classes can extend it (`class Sub extends Counter`).

## Constants

```go
// MaxItems is the largest cart.
//
//napi:export
const MaxItems = 100
```

In JS it is `maxItems`; `//napi:export MAX_ITEMS` keeps another name. `//napi:export` on a variable
exports its value when the addon is loaded.

## Calling JavaScript

A `napi.Function` parameter can be called during the call, on the JS thread:

```go
//napi:export
func Twice(fn napi.Function) error {
	for range 2 {
		if _, err := fn.Call(napi.Value{}); err != nil {
			return err // a JS exception propagates
		}
	}
	return nil
}
```

From goroutines, use `napi.NewJSFunc[A, R]`: `Call(ctx, a)` queues the call on the JS thread and
waits for the result converted to `R` (a returned Promise is awaited, a JS exception becomes an
error). Release it when done; it keeps Node.js running while open (`Unref` to avoid that).

```go
//napi:export
func Watch(env napi.Env, onEvent napi.Function) error {
	notify, err := napi.NewJSFunc[Event, struct{}](env, onEvent, napi.ThreadsafeFunctionOptions{})
	if err != nil {
		return err
	}
	ctx, err := env.Context() // canceled when the env is destroyed
	if err != nil {
		return err
	}
	go func() {
		defer notify.Release()
		for ev := range events(ctx) {
			if _, err := notify.Call(ctx, ev); err != nil {
				return
			}
		}
	}()
	return nil
}
```

## Threads and memory

- **napi values belong to the JS thread.** `napi.Env`, `napi.Value` and the types built on them
  must not be used in goroutines, and must not be kept after the call returns (to keep a JS value,
  create a reference with `Value.NewRef`).
- **A `[]byte` argument is a view of JS memory**, valid during the call: copy it to keep it.
- **Worker threads load the addon again**, with their own env, but share the Go runtime and its
  global variables: protect global state as for any concurrent Go code.
- Go values reach JS only through napi-go, which follows the cgo pointer rules: no Go pointer is
  handed to Node.js.

## Layer 1

Layer 2 covers most addons. For full control (creating functions at runtime, property
descriptors, references, handle scopes, external buffers, type tags), use the Layer 1 API in the
same addon: `napi.Init` registers a function that receives the env and the exports object, as in
node-addon-api.

```go
func init() {
	napi.Init(func(env napi.Env, exports napi.Object) (napi.Value, error) {
		fn, err := env.NewFunction("hello", func(c napi.CallContext) (napi.Value, error) {
			s, err := c.Env().NewString("world")
			return s.Value, err
		})
		if err != nil {
			return napi.Value{}, err
		}
		return napi.Value{}, exports.Set("hello", fn.Value)
	})
}
```

See the [Layer 1 design](../napi/README.md#layer-1-design-choices) and the
[threading APIs](../napi/README.md#threading).

## Testing

Test the addon from JavaScript with `node:test` (the template does). Go logic that does not
touch napi can be tested with `go test` as usual; napi code runs only inside Node.js.
