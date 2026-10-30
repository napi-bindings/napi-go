# napi-go

Binding Go per [Node-API](https://nodejs.org/api/n-api.html) (N-API): moduli nativi Node.js
(`.node`) scritti in Go, sul modello di [napi-rs](https://napi.rs).

> **Stato:** in sviluppo. Completati la Fase 0 (spike di fattibilità) e la Fase 1 (Layer 0); in
> corso la Fase 2 (Layer 1).

| Percorso | Contenuto |
|---|---|
| [`sys/`](sys/README.md) | Layer 0: binding 1:1 generato di Node-API 1–10 |
| [`napi/`](napi/README.md) | Layer 1: API Go idiomatica (valori, errori, funzioni, classi, buffer) |
| `tests/e2e/` | addon di test e suite `node:test` |
| [`tests/nodesuite/`](tests/nodesuite/README.md) | test `js-native-api` e `node-api` di Node.js portati su `sys` |
| [`spike/`](spike/README.md) | Fase 0: prototipo e risultati di fattibilità |
| `.github/workflows/` | CI: `sys.yml` (Layer 0 e 1) e `spike.yml` (Fase 0) |

Requisiti: Go ≥ 1.24 con cgo, un compilatore C (gcc/clang, oppure `zig cc` su Windows),
Node.js ≥ 22 per i test.

```bash
go test ./sys/... ./napi/...           # test unitari, senza Node
node tests/e2e/build.mjs               # Windows senza gcc: set CC=zig cc
node --test tests/e2e/*.test.mjs
node tests/nodesuite/run.mjs          # suite di Node.js portata su sys
```

Piattaforme: Linux glibc x64/arm64, macOS arm64/x64 (x64: un solo addon Go per processo),
Windows x64. Alpine/musl non è supportato dalle release attuali di Go.
