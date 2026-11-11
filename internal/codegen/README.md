# napi-go gen

The code generator of the [`napi-go`](../../cmd/napi-go/README.md) CLI. Generates the Layer 2 registrations and the TypeScript declarations of a napi-go addon from
annotations in its Go source: the equivalent of `#[napi]` and of the type generation of napi-rs.

```go
package main

//go:generate go tool napi-go gen

import "context"

// User is a registered user.
type User struct {
	Name  string
	Email *string
}

// Greet returns a greeting for u.
//
//napi:export
func Greet(u User) string { return "hello " + u.Name }

// FindUser loads a user in a goroutine.
//
//napi:export
func FindUser(ctx context.Context, name string) (*User, error) { return &User{Name: name}, nil }

func main() {}
```

`napi-go gen` (run by `napi-go build`, or by `go generate`) writes three files:

| File | Contents |
|---|---|
| `zz_napi.go` | an `init` function calling `napi.Export` for every annotated declaration, with the wrapper that fits each signature (`Func1`, `Async1`, `MethodE2`, …) |
| `index.d.ts` | TypeScript declarations; the Go doc comments become JSDoc |
| `index.js` | CommonJS entry point loading the binary of the platform (see the [CLI](../../cmd/napi-go/README.md#the-entry-point)) |

```ts
/** User is a registered user. */
export interface User {
  name: string
  email?: string | null
}

/** Greet returns a greeting for u. */
export declare function greet(u: User): string

/** FindUser loads a user in a goroutine. */
export declare function findUser(name: string): Promise<User | null>
```

Problems are reported when generating, with file and line, instead of when the addon is loaded:

```
main.go:14: func Watch: parameter 1: type chan int has no JS conversion
```

## Annotations

Annotations are written like Go directives, with no space after `//`, in the doc comment of the
declaration. An unknown or misplaced annotation is an error.

| Annotation | On | Effect |
|---|---|---|
| `//napi:export [name]` | function | exported function; `name` defaults to the Go name in lowerCamelCase. A first parameter of type `context.Context` makes it asynchronous (it runs in a goroutine and returns a Promise) |
| `//napi:export Class.name` | function, constant | static method or static constant of a class |
| `//napi:export [name]` | constant, variable | exported constant (the value when the addon is loaded) |
| `//napi:class [Name]` | type | JS class whose instances wrap a `*T`. Every exported method of `*T` becomes a method, except `Finalize`, `FromJS`, `ToJS` and those marked `//napi:skip` |
| `//napi:constructor` | function returning `(*T, error)` | constructor of the class of `T`. Without one the class cannot be constructed from JS |
| `//napi:export name` | method | renames the method |
| `//napi:getter [name]` | method `func (*T) X() V` | property getter (`V, error` allowed) |
| `//napi:setter [name]` | method `func (*T) SetX(v V)` | setter of the property with the same name; the default name drops the `Set` prefix |
| `//napi:skip` | method | not exported |
| `//napi:object [Name]` | struct type | emits its interface even if no export uses it, optionally renamed. Structs used by an export are emitted without it |
| `//napi:type <TypeScript type>` | type | TypeScript type of a type that converts itself (`FromJS`/`ToJS`), `any` otherwise |
| `//napi:returns <TypeScript type>` | function, method | overrides the declared return type (e.g. `this`, or `Promise<string[]>` for a function returning a `napi.Promise`) |

Functions take up to 8 parameters and return nothing, a value, an error, or a value and an error;
asynchronous functions return `(R, error)` or `error`. Parameters of type `napi.Env` and
`napi.CallContext` are filled by napi and do not appear in the declarations (`CallContext` adds
`...args: any[]`). Variadic and generic functions, asynchronous methods and classes used by value
(`T` instead of `*T`) are rejected with an explanation.

## TypeScript mapping

| Go | TypeScript |
|---|---|
| `bool`, `string` | `boolean`, `string` |
| numeric types | `number`; `int64` and `uint64` parameters: `number \| bigint` |
| `[]byte` | parameter: `Uint8Array \| ArrayBuffer \| DataView`; result: `Buffer`; field: `Uint8Array` |
| `*big.Int`, `time.Time` | `bigint`, `Date` |
| `[]T`, `[N]T` | `T[]`; a tuple for N ≤ 8 |
| `map[K]V` | `Record<string, V>`, `Record<number, V>` for integer keys |
| struct of the package | an `interface` with its name; fields follow the `napi`/`json` tags; pointers, `any` and `omitempty` fields are optional |
| other structs | an inline object type |
| `*T` | `T \| null` (parameters also accept `undefined`) |
| `*T` of a class | `Class \| null` |
| `napi.Either[A, B]` | `A \| B` |
| `any`, `napi.Value` | `any` |
| `napi.Object`, `napi.Function`, `napi.Promise`, … | `object`, `(...args: any[]) => any`, `Promise<any>`, … |
| `error` only / no result | `void`; asynchronous: `Promise<…>` |

The declarations use `Buffer`, declared by `@types/node`.

## Usage

```
napi-go gen [flags]

  -C dir             project (package.json) or Go package directory (default .)
  -name name         base name of the .node files (default: binaryName of package.json, or the directory name)
  -package-name pkg  npm package of the platform packages (default: name of package.json)
  -js file           JS entry point (default index.js; empty: none)
  -dts file          TypeScript declarations (default index.d.ts; empty: none)
  -tags tags         build tags, as for go build
  -check             do not write: fail if the generated files are out of date (for CI)
```

In a project, the files go next to `package.json` and `zz_napi.go` into the Go package
(`napiGo.goPackage`); `go generate` in the Go package finds the `package.json` above it.

The generator uses only the Go standard library. It type-checks the package with the export data
of the Go build cache (`go list -export`), so it needs the same toolchain as the build, C compiler
included.

## What is generated, and what is not

The generated Go code registers the exports with the generic wrappers of package `napi`; the
conversions are the cached codecs of Layer 2. Generating Go conversion code for each struct was
measured and dropped: on a struct of five fields it was 15% faster from JS to Go and no faster from
Go to JS (about 2 µs either way), because the time goes into the Node-API calls for each property,
not into Go. Reading the properties of a struct in one C call is the optimization that would help.
