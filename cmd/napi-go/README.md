# napi-go (CLI)

Creates, builds and packages Node.js addons written in Go: the equivalent of the `napi` CLI of
napi-rs.

```sh
go run github.com/napi-bindings/napi-go/cmd/napi-go@latest new my-addon
cd my-addon
npm run build:debug        # go tool napi-go build
npm test
```

A project created by `new` requires napi-go in `go.mod` both as a library and as a
[tool](https://go.dev/doc/modules/managing-dependencies#tools), so the CLI runs at the version of
the library with `go tool napi-go <command>`.

| Command | |
|---|---|
| `new <dir>` | create a project: `go.mod`, an annotated `main.go`, `package.json`, a `node:test` test, `.gitignore`, `README.md` and a GitHub Actions workflow |
| `gen` | generate `zz_napi.go`, `index.js` and `index.d.ts` from the `//napi:` annotations ([`internal/codegen`](../../internal/codegen/README.md)) |
| `build` | `go build -buildmode=c-shared` for this machine or other targets, after `gen` |
| `artifacts` | copy built binaries into the npm platform packages `npm/<target>/` |
| `prepublish` | sync the platform packages and `optionalDependencies` with the version; `-publish` publishes the platform packages |
| `version` | print the version |

Every command takes `-C <dir>` (the project, default `.`) and describes its flags with `-h`.

## Configuration

`package.json` holds the configuration under `napiGo`; every field is optional.

```json
{
  "name": "@acme/fast-thing",
  "version": "1.0.0",
  "main": "index.js",
  "types": "index.d.ts",
  "napiGo": {
    "binaryName": "fast-thing",
    "goPackage": ".",
    "targets": ["linux-x64-gnu", "linux-arm64-gnu", "darwin-x64", "darwin-arm64", "win32-x64-gnu"],
    "tags": "",
    "codegen": true,
    "js": "index.js",
    "dts": "index.d.ts",
    "npmDir": "npm"
  }
}
```

| Field | Default | |
|---|---|---|
| `binaryName` | the package name without scope | binaries are `<binaryName>.<target>.node` |
| `goPackage` | `.` | directory of the Go main package |
| `targets` | the five above | built by `build -target all` and published |
| `tags` | none | Go build tags |
| `codegen` | `true` | `build` runs `gen` first |
| `js`, `dts` | `index.js`, `index.d.ts` | generated entry point and declarations |
| `npmDir` | `npm` | directory of the platform packages |

Without `package.json`, `gen` and `build` work on the Go package in the directory, named after it.

## Targets

Targets are named like the platform packages of napi-rs: `<platform>-<arch>[-<abi>]`, from
`process.platform` and `process.arch`.

| Target | GOOS/GOARCH | zig target | Runner of the CI template |
|---|---|---|---|
| `linux-x64-gnu` | linux/amd64 | `x86_64-linux-gnu.2.17` | `ubuntu-latest` |
| `linux-arm64-gnu` | linux/arm64 | `aarch64-linux-gnu.2.17` | `ubuntu-24.04-arm` |
| `darwin-x64` | darwin/amd64 | `x86_64-macos.11.0` | `macos-15-intel` |
| `darwin-arm64` | darwin/arm64 | `aarch64-macos.11.0` | `macos-latest` |
| `win32-x64-gnu` | windows/amd64 | `x86_64-windows-gnu` | `windows-latest` |
| `win32-arm64-gnu` | windows/arm64 | `aarch64-windows-gnu` | `windows-11-arm` |

Windows targets end in `-gnu` because Go links with MinGW. musl (Alpine) is rejected: no Go
release builds a shared library that loads with musl (golang/go#71953). On macOS x64 only one
napi-go addon can be loaded per process (a limit of the Go runtime, see `sys/README.md`).

## Building and cross-compiling

```sh
go tool napi-go build                                   # debug build for this machine
go tool napi-go build -release                          # -ldflags="-s -w"
go tool napi-go build -release -target all              # every target of package.json
go tool napi-go build -target linux-arm64-gnu,win32-x64-gnu -o dist
go tool napi-go build -- -a                             # after --: further go build flags
```

Cross builds use [zig](https://ziglang.org/download/) as C compiler (`zig cc -target …`), from any
host to any target. `-zig` chooses when:

- `auto` (default): cross builds, and release builds for Linux, so that the binary needs glibc 2.17
  rather than the glibc of the build machine (without zig a warning says so);
- `always`: every build (for instance Windows without MinGW);
- `never`: the system C compiler (`$CC`); cross builds then need `-cc`.

`-cc <command>` sets the C compiler of every target instead. macOS builds the other architecture
with the system clang. For macOS targets built elsewhere, the CLI provides zig with stubs of
`libresolv` and of the CoreFoundation framework, which Go links and zig does not ship (the real
libraries are loaded at run time).

After each build the format of the binary is checked against the target (ELF, Mach-O or PE,
architecture, shared library), so a wrong `$CC` fails the build instead of the `require()`.

## The entry point

`gen` writes `index.js`, which loads the binary of the running platform, trying in order:

1. `NAPI_GO_NATIVE_LIBRARY_PATH`, if set (its errors are not caught);
2. `<binaryName>.<target>.node` and `<binaryName>.node` next to `index.js` (local builds);
3. the package `<name>-<target>` (published builds).

If none loads, the error lists each candidate with the reason (`not found`, `package not
installed`, or the error of `dlopen`), with the errors as `cause`. On musl it says that musl is not
supported. `index.js` ends with the export names in the form `0 && (module.exports = { … })`,
which Node reads (and never runs) to let ES modules import them by name.

## Publishing

As with napi-rs, every target has an npm package `<name>-<target>` holding its `.node` file, with
`os`, `cpu` and, on Linux, `libc`; the main package lists them in `optionalDependencies` and npm
installs only the one matching the machine.

```sh
go tool napi-go artifacts -d artifacts   # find <binaryName>.<target>.node files (recursively) and copy them to npm/<target>/
go tool napi-go prepublish -publish      # versions, optionalDependencies, npm publish of each platform package
npm publish                              # the main package
```

`artifacts` creates or updates `npm/<target>/package.json` (fields it does not manage are kept)
and rejects a binary whose format does not match its target. `prepublish` fails if a binary is
missing; with `-publish` it skips the packages already on the registry at this version, so a
failed release can be rerun. `-tag` and `-dry-run` are passed to `npm publish`; scoped packages
are published with `--access public` unless `package.json` has `publishConfig`.

The workflow created by `new` (`.github/workflows/CI.yml`) builds and tests each target on a
runner of that platform, and for a tag `v<version>` checks that the tag matches `package.json`,
then runs the three commands above (secret `NPM_TOKEN`, with npm provenance).
