# Vendored Node-API Headers

Unmodified copy of `include/*.h` and `symbols.js` from the npm package
[`node-api-headers`](https://github.com/nodejs/node-api-headers) **1.9.0** (MIT license, see `LICENSE`).

- The headers are used for **types** only: functions are called through the generated function
  pointer table (`../zz_api.h`), resolved at runtime from the host process.
- `symbols.js` provides the minimum Node-API version for each function.
- The package does not include experimental APIs (`NAPI_EXPERIMENTAL`).

To update: replace these files with those from a newer version of the package, update the
version number above and in `internal/gen` (`headersVersion`), then run `go generate ./sys`.
