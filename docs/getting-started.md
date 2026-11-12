# Getting started

## Requirements

- **Go 1.24** or later, with cgo.
- **A C compiler**: gcc or clang on Linux and macOS; on Windows MinGW gcc, or
  [zig](https://ziglang.org/download/) (`napi-go build -zig always`).
- **Node.js 22** or later.
- Optional: **zig**, to build for other platforms (`napi-go build -target …`) and to make Linux
  release binaries that run on older distributions (glibc 2.17).

## Create a project

```sh
go run github.com/napi-bindings/napi-go/cmd/napi-go@latest new my-addon
cd my-addon
npm run build:debug
npm test
```

`napi-go new` creates:

| File | |
|---|---|
| `main.go` | the addon: annotated Go functions and a class |
| `go.mod` | requires napi-go as a library and as a tool, so `go tool napi-go` runs the CLI at the version of the library |
| `package.json` | the npm package; `napiGo` holds the napi-go configuration |
| `test/index.test.mjs` | tests with `node:test` |
| `.github/workflows/CI.yml` | builds and tests every target, publishes on a version tag |
| `.gitignore`, `README.md` | |

`npm run build:debug` runs `go tool napi-go build`, which first generates `zz_napi.go`,
`index.js` and `index.d.ts` from the annotations, then builds `my-addon.<target>.node` for this
machine. Commit the generated files: they are part of the package.

## Write a function

```go
// Greet returns a greeting for name.
//
//napi:export
func Greet(name string) string { return "hello " + name }
```

```sh
npm run build:debug
node -e "console.log(require('.').greet('Ann'))"
```

From TypeScript and ES modules the exports have types and names:

```ts
import { greet } from 'my-addon'
greet('Ann')   // greet(name: string): string, with the Go comment as JSDoc
```

The [guide](guide.md) covers the parameter and result types, errors, async functions and classes.

## Build for other platforms

```sh
go tool napi-go build -release                        # this machine
go tool napi-go build -release -target linux-arm64-gnu
go tool napi-go build -release -target all            # the targets of package.json
```

Builds for another platform use zig as C compiler. The targets are `linux-x64-gnu`,
`linux-arm64-gnu`, `darwin-x64`, `darwin-arm64`, `win32-x64-gnu` and `win32-arm64-gnu`; see the
[CLI reference](../cmd/napi-go/README.md#targets).

## Publish

The package follows the layout of napi-rs: the main package holds `index.js` and `index.d.ts`, and
each target has a package `my-addon-<target>` with its `.node` file, installed by npm only on
matching machines.

The workflow created by `new` does it all: push a tag `v<version>` equal to the version of
`package.json`, with the repository secret `NPM_TOKEN`. By hand:

```sh
go tool napi-go artifacts -d <directory with the built .node files>
go tool napi-go prepublish -publish
npm publish
```

## Without the template

An existing Go module can become an addon without `napi-go new`:

```sh
go get github.com/napi-bindings/napi-go@latest
go get -tool github.com/napi-bindings/napi-go/cmd/napi-go
```

Write a `main` package with an empty `func main() {}` and annotated declarations, then
`go tool napi-go build` (without `package.json` the binary is named after the directory). Or skip
the generator and register with `napi.Export` in an `init` function
([`napi` package](../napi/README.md)), and build with
`go build -buildmode=c-shared -o addon.node .`.
