//go:build napi_go_test_version_mismatch

package sys

// #cgo CFLAGS: -DNAPI_VERSION=2147483646
import "C"

// DeclaredVersion è la versione Node-API che il modulo dichiara a Node.js.
//
// Build tag napi_go_test_version_mismatch, riservato ai test: dichiara VersionTestMismatch, una
// versione che nessun Node.js supporta (sopra NODE_API_SUPPORTED_VERSION_MAX e diversa da
// NAPI_VERSION_EXPERIMENTAL), per verificare che Node rifiuti il modulo con un errore
// catturabile da require() (test/node-api/test_module_version_mismatch). Un addon vero non lo usa:
// Node non lo carica mai.
const DeclaredVersion = VersionTestMismatch
