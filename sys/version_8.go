//go:build !napi_go_v9 && !napi_go_v10 && !napi_go_experimental && !napi_go_test_version_mismatch

package sys

// #cgo CFLAGS: -DNAPI_VERSION=8
import "C"

// DeclaredVersion is the Node-API version that the module reports to Node.js.
// Default: Node-API 8, supported by all maintained Node.js releases.
const DeclaredVersion = 8
