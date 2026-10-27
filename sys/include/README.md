# Header Node-API vendorizzati

Copia non modificata di `include/*.h` e `symbols.js` dal pacchetto npm
[`node-api-headers`](https://github.com/nodejs/node-api-headers) **1.9.0** (licenza MIT, vedi `LICENSE`).

- Gli header servono solo per i **tipi**: le funzioni vengono chiamate tramite la tabella di puntatori
  generata (`../zz_api.h`), risolta a runtime dal processo host.
- `symbols.js` dà la versione Node-API minima di ogni funzione.
- Il pacchetto non contiene API sperimentali (`NAPI_EXPERIMENTAL`).

Per aggiornare: sostituire i file con quelli di una nuova versione del pacchetto, aggiornare la
versione qui sopra e in `internal/gen` (`headersVersion`), poi `go generate ./sys`.
