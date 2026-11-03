//go:build napi_go_v10 && !napi_go_experimental && !napi_go_test_version_mismatch

package sys

// #cgo CFLAGS: -DNAPI_VERSION=10
import "C"

// DeclaredVersion is the Node-API version that the module reports to Node.js.
// Build tag napi_go_v10.
const DeclaredVersion = 10
