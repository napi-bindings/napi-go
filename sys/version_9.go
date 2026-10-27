//go:build napi_go_v9 && !napi_go_v10 && !napi_go_experimental

package sys

// #cgo CFLAGS: -DNAPI_VERSION=9
import "C"

// DeclaredVersion è la versione Node-API che il modulo dichiara a Node.js.
// Build tag napi_go_v9.
const DeclaredVersion = 9
