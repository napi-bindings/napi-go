//go:build napi_go_experimental && !napi_go_test_version_mismatch

package sys

// #cgo CFLAGS: -DNAPI_VERSION=2147483647 -DNAPI_EXPERIMENTAL
import "C"

// DeclaredVersion is the Node-API version that the module reports to Node.js.
// Build tag napi_go_experimental: Node treats the module as experimental (for example, finalizers
// may run synchronously during garbage collection). node-api-headers does not include experimental APIs.
const DeclaredVersion = VersionExperimental
