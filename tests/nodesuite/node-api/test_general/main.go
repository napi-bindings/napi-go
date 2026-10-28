// Porting di test/node-api/test_general/test_general.c (NAPI_VERSION 9, vedi napi-go.json).
// NODE_API_NO_EXTERNAL_BUFFERS_ALLOWED del C toglie solo dichiarazioni dagli header: il file non
// usa buffer esterni, quindi non ha un equivalente.
package main

import (
	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

// versionFields è la lunghezza dell'array [major, minor, patch, release].
const versionFields = 4

func testGetNodeVersion(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var nodeVersion *sys.NodeVersion
	var result, major, minor, patch, release sys.Value

	if !nt.Call(env, sys.GetNodeVersion(env, &nodeVersion)) {
		return nil
	}
	if !nt.Call(env, sys.CreateUint32(env, nodeVersion.Major, &major)) {
		return nil
	}
	if !nt.Call(env, sys.CreateUint32(env, nodeVersion.Minor, &minor)) {
		return nil
	}
	if !nt.Call(env, sys.CreateUint32(env, nodeVersion.Patch, &patch)) {
		return nil
	}
	if !nt.Call(env, sys.CreateStringUTF8(env, nodeVersion.Release, sys.AutoLength, &release)) {
		return nil
	}
	if !nt.Call(env, sys.CreateArrayWithLength(env, versionFields, &result)) {
		return nil
	}
	for i, v := range [versionFields]sys.Value{major, minor, patch, release} {
		if !nt.Call(env, sys.SetElement(env, result, uint32(i), v)) {
			return nil
		}
	}
	return result
}

func getFilename(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var filename *byte
	var result sys.Value

	if !nt.Call(env, sys.GetModuleFileName(env, &filename)) {
		return nil
	}
	if !nt.Call(env, sys.CreateStringUTF8(env, filename, sys.AutoLength, &result)) {
		return nil
	}
	return result
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	descriptors := []sys.PropertyDescriptor{
		nt.DeclareProperty("testGetNodeVersion", testGetNodeVersion),
		nt.DeclareGetter("filename", getFilename),
	}
	if !nt.Call(env, nt.DefineProperties(env, exports, descriptors)) {
		return nil
	}
	return exports
}
