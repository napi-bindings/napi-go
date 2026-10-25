# musltls — dlopen di un c-shared Go con musl

Riproduce, senza Node, il fallimento osservato sul job Alpine: un host musl carica con `dlopen`
un `c-shared` Go, come fa Node con un `.node`.

```bash
musl-gcc -O2 -o host host.c
CGO_ENABLED=1 CC=musl-gcc go build -buildmode=c-shared -buildvcs=false -o lib.so .
./host ./lib.so                       # dlopen dopo l'avvio (= require di un .node)
LD_PRELOAD=$PWD/lib.so ./host ./lib.so   # workaround noto
readelf -r lib.so | grep TPOFF        # R_X86_64_TPOFF64 = modello TLS initial-exec
```

Cross-compilazione da Windows (Go 1.27.1 + zig):
`set GOOS=linux&& set GOARCH=amd64&& set CGO_ENABLED=1&& set CC=zig cc -target x86_64-linux-musl&& go build -buildmode=c-shared -o lib127.so .`

| Go | Rilocazione TLS | `dlopen` | `LD_PRELOAD` |
|---|---|---|---|
| 1.24.7 (musl-gcc) | `R_X86_64_TPOFF64` (initial-exec) | ❌ *initial-exec TLS resolves to dynamic definition* | ❌ segfault |
| 1.26.10 (Alpine, CI) | — | ❌ stesso errore (da `require()`) | — |
| 1.27.1 (zig, musl) | `R_X86_64_TPOFF64` (initial-exec) | ❌ stesso errore | — |

Due difetti, entrambi nel runtime Go:
1. **TLS initial-exec:** il runtime accede al proprio TLS con il modello initial-exec, che musl non
   consente nelle librerie caricate dopo l'avvio (glibc lo tollera grazie al margine di TLS statico).
2. **`argc`/`argv`:** l'init del `c-shared` si aspetta `argc`/`argv`, che glibc passa alle funzioni
   di `.init_array` ma musl no. È la causa del segfault con `LD_PRELOAD`.

Correzione upstream (non ancora in una release): proposta golang/go#71953 (TLS "general dynamic")
e CL 696635 (#13492, #54805), che aggiunge `-tls=auto|LE|IE|GD` e i controlli su `argv`.
Quando arriverà, il job Alpine in CI tornerà verde e la rilocazione diventerà `TLSGD`/`TLSDESC`.
