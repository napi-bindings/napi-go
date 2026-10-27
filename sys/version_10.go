//go:build napi_go_v10 && !napi_go_experimental

package sys

// #cgo CFLAGS: -DNAPI_VERSION=10
import "C"

// DeclaredVersion è la versione Node-API che il modulo dichiara a Node.js.
// Build tag napi_go_v10.
const DeclaredVersion = 10
