//go:build napi_go_v9 && !napi_go_v10 && !napi_go_experimental && !napi_go_test_version_mismatch

package sys

// #cgo CFLAGS: -DNAPI_VERSION=9
import "C"

// DeclaredVersion is the Node-API version that the module reports to Node.js.
// Build tag napi_go_v9.
const DeclaredVersion = 9
