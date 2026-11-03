# musltls — dlopen of a Go c-shared library with musl

Reproduces, without Node, the failure observed on the Alpine CI job: a musl host loads a Go
`c-shared` library via `dlopen`, just as Node does with a `.node` addon.

```bash
musl-gcc -O2 -o host host.c
CGO_ENABLED=1 CC=musl-gcc go build -buildmode=c-shared -buildvcs=false -o lib.so .
./host ./lib.so                       # dlopen after startup (= require of a .node)
LD_PRELOAD=$PWD/lib.so ./host ./lib.so   # known workaround
readelf -r lib.so | grep TPOFF        # R_X86_64_TPOFF64 = initial-exec TLS model
```

Cross-compilation from Windows (Go 1.27.1 + zig):
`set GOOS=linux&& set GOARCH=amd64&& set CGO_ENABLED=1&& set CC=zig cc -target x86_64-linux-musl&& go build -buildmode=c-shared -o lib127.so .`

| Go | TLS Relocation | `dlopen` | `LD_PRELOAD` |
|---|---|---|---|
| 1.24.7 (musl-gcc) | `R_X86_64_TPOFF64` (initial-exec) | ❌ *initial-exec TLS resolves to dynamic definition* | ❌ segfault |
| 1.26.10 (Alpine, CI) | — | ❌ same error (from `require()`) | — |
| 1.27.1 (zig, musl) | `R_X86_64_TPOFF64` (initial-exec) | ❌ same error | — |

Two defects, both in the Go runtime:
1. **initial-exec TLS:** the runtime accesses its TLS using the initial-exec model, which musl does not
   allow in libraries loaded after startup (glibc tolerates it thanks to static TLS surplus).
2. **`argc`/`argv`:** `c-shared` initialization expects `argc`/`argv`, which glibc passes to
   `.init_array` functions but musl does not. This causes the segfault under `LD_PRELOAD`.

Upstream fix (not yet in a release): proposal golang/go#71953 ("general dynamic" TLS)
and CL 696635 (#13492, #54805), which adds `-tls=auto|LE|IE|GD` and `argv` checks.
Once landed, the Alpine job in CI will turn green and the relocation will become `TLSGD`/`TLSDESC`.
