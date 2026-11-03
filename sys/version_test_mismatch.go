//go:build napi_go_test_version_mismatch

package sys

// #cgo CFLAGS: -DNAPI_VERSION=2147483646
import "C"

// DeclaredVersion is the Node-API version that the module reports to Node.js.
//
// Build tag napi_go_test_version_mismatch, reserved for tests: declares VersionTestMismatch, a
// version that no Node.js release supports (above NODE_API_SUPPORTED_VERSION_MAX and distinct from
// NAPI_VERSION_EXPERIMENTAL), to verify that Node rejects the module with an error catchable by
// require() (test/node-api/test_module_version_mismatch). Real addons must never use this tag:
// Node will always refuse to load the module.
const DeclaredVersion = VersionTestMismatch
