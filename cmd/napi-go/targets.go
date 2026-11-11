package main

// targets.go — the platforms an addon can be built for.

import (
	"fmt"
	"runtime"
	"strings"
)

// target is a platform a .node file is built for. Its name is the suffix of the binary and of the
// npm platform package, as in napi-rs: <platform>-<arch>[-<abi>], with the values of
// process.platform and process.arch (the same rule as the loader in index.js).
type target struct {
	name     string
	goos     string
	goarch   string
	zig      string // zig cc target: the minimum glibc and macOS versions are part of it
	platform string // process.platform, the "os" of the platform package
	arch     string // process.arch, the "cpu" of the platform package
	libc     string // the "libc" of the platform package, if any
}

// Minimum versions of the C library and of macOS for builds made with zig. glibc 2.17 is the floor
// of napi-rs (CentOS 7); macOS 11 is the minimum of Go 1.24 and Node.js 22.
const (
	zigGlibc = "2.17"
	zigMacOS = "11.0"
)

// allTargets is the list of supported targets. Go also builds c-shared libraries for other
// platforms, but these are the ones whose binaries are loaded and tested in CI.
var allTargets = []target{
	{"linux-x64-gnu", "linux", "amd64", "x86_64-linux-gnu." + zigGlibc, "linux", "x64", "glibc"},
	{"linux-arm64-gnu", "linux", "arm64", "aarch64-linux-gnu." + zigGlibc, "linux", "arm64", "glibc"},
	{"darwin-x64", "darwin", "amd64", "x86_64-macos." + zigMacOS, "darwin", "x64", ""},
	{"darwin-arm64", "darwin", "arm64", "aarch64-macos." + zigMacOS, "darwin", "arm64", ""},
	{"win32-x64-gnu", "windows", "amd64", "x86_64-windows-gnu", "win32", "x64", ""},
	{"win32-arm64-gnu", "windows", "arm64", "aarch64-windows-gnu", "win32", "arm64", ""},
}

// defaultTargets are built by -target all when package.json does not list targets.
var defaultTargets = []string{"linux-x64-gnu", "linux-arm64-gnu", "darwin-x64", "darwin-arm64", "win32-x64-gnu"}

// errMusl explains why musl targets are rejected.
const errMusl = "musl libc (Alpine Linux) is not supported: no release of Go builds a shared library that loads with musl (golang/go#71953)"

// lookupTarget returns the target called name.
func lookupTarget(name string) (target, error) {
	for _, t := range allTargets {
		if t.name == name {
			return t, nil
		}
	}
	if strings.HasSuffix(name, "-musl") {
		return target{}, fmt.Errorf("target %s: %s", name, errMusl)
	}
	names := make([]string, len(allTargets))
	for i, t := range allTargets {
		names[i] = t.name
	}
	return target{}, fmt.Errorf("unknown target %q (supported: %s)", name, strings.Join(names, ", "))
}

// hostTarget returns the target of the machine running napi-go.
func hostTarget() (target, error) {
	for _, t := range allTargets {
		if t.goos == runtime.GOOS && t.goarch == runtime.GOARCH {
			return t, nil
		}
	}
	return target{}, fmt.Errorf("no target for this machine (%s/%s): pass -target", runtime.GOOS, runtime.GOARCH)
}

// isCross reports whether building t needs a cross compiler. macOS builds for the other
// architecture with the system clang (Go passes -arch), so only a change of OS is a cross build
// there.
func (t target) isCross() bool {
	if t.goos != runtime.GOOS {
		return true
	}
	return t.goarch != runtime.GOARCH && runtime.GOOS != "darwin"
}

// binaryFile returns the name of the .node file of t.
func (t target) binaryFile(binaryName string) string {
	return binaryName + "." + t.name + ".node"
}

// packageName returns the name of the npm platform package of t.
func (t target) packageName(mainPackage string) string {
	return mainPackage + "-" + t.name
}

// resolveTargets returns the targets named in spec, a comma-separated list where "all" stands for
// the targets of the project. An empty spec is the host target.
func resolveTargets(spec string, project []string) ([]target, error) {
	if spec == "" {
		t, err := hostTarget()
		if err != nil {
			return nil, err
		}
		return []target{t}, nil
	}
	var out []target
	seen := map[string]bool{}
	for _, name := range strings.Split(spec, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, fmt.Errorf("empty target name in %q", spec)
		}
		names := []string{name}
		if name == "all" {
			names = project
		}
		for _, n := range names {
			if seen[n] {
				continue
			}
			t, err := lookupTarget(n)
			if err != nil {
				return nil, err
			}
			seen[n] = true
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no target in %q", spec)
	}
	return out, nil
}
