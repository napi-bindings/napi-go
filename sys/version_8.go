//go:build !napi_go_v9 && !napi_go_v10 && !napi_go_experimental && !napi_go_test_version_mismatch

package sys

// #cgo CFLAGS: -DNAPI_VERSION=8
import "C"

// DeclaredVersion è la versione Node-API che il modulo dichiara a Node.js.
// Default: Node-API 8, supportata da tutte le versioni di Node.js mantenute.
const DeclaredVersion = 8
