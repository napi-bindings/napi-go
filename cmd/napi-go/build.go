package main

// build.go — napi-go build: go build -buildmode=c-shared for one or more targets, cross-compiling
// with zig.

import (
	"bytes"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Values of -zig.
const (
	zigAuto   = "auto"
	zigAlways = "always"
	zigNever  = "never"
)

// buildOptions are the flags of build that shape the go build command.
type buildOptions struct {
	release bool
	zig     string // zigAuto, zigAlways or zigNever
	cc      string // explicit C compiler, for every target
	outDir  string // absolute
	tags    string
	ldflags string // extra -ldflags
	verbose bool
	extra   []string // further go build arguments
}

func runBuild(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet(stderr, "build", "[-- go build flags]")
	dir := fs.String("C", ".", "project directory")
	var o buildOptions
	fs.BoolVar(&o.release, "release", false, "release build: strip symbols and debug information (-ldflags=-s -w)")
	targetSpec := fs.String("target", "", "comma-separated targets, or all for the targets of package.json (default: this machine)")
	fs.StringVar(&o.zig, "zig", zigAuto, "use zig cc as C compiler: auto (cross builds and Linux release builds, for glibc "+zigGlibc+"), always or never")
	fs.StringVar(&o.cc, "cc", "", "C compiler for every target, instead of zig or $CC")
	out := fs.String("o", ".", "output directory, relative to the project")
	tags := fs.String("tags", "", "Go build tags (default: tags of package.json)")
	fs.StringVar(&o.ldflags, "ldflags", "", "further linker flags (go build -ldflags)")
	noGen := fs.Bool("no-gen", false, "do not run napi-go gen first")
	fs.BoolVar(&o.verbose, "v", false, "print the go build commands")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	o.extra = fs.Args()
	switch o.zig {
	case zigAuto, zigNever:
	case zigAlways:
		if o.cc != "" {
			return errors.New("-cc and -zig always exclude each other")
		}
	default:
		return fmt.Errorf("-zig %q: want %s, %s or %s", o.zig, zigAuto, zigAlways, zigNever)
	}
	p, err := loadProject(*dir)
	if err != nil {
		return err
	}
	o.tags = p.cfg.Tags
	if *tags != "" {
		o.tags = *tags
	}
	o.outDir = p.path(*out)
	targets, err := resolveTargets(*targetSpec, p.cfg.Targets)
	if err != nil {
		return err
	}
	if p.codegenEnabled() && !*noGen {
		cfg := p.codegenConfig()
		cfg.Tags = o.tags
		if err := generate(cfg, false, stdout); err != nil {
			return fmt.Errorf("gen: %w", err)
		}
	}
	if err := os.MkdirAll(o.outDir, 0o755); err != nil {
		return err
	}
	for _, t := range targets {
		if err := buildTarget(p, t, o, stdout, stderr); err != nil {
			return fmt.Errorf("%s: %w", t.name, err)
		}
	}
	return nil
}

// buildTarget builds the .node file of t.
func buildTarget(p *project, t target, o buildOptions, stdout, stderr io.Writer) error {
	start := time.Now()
	tc, err := toolchainFor(t, o)
	if err != nil {
		return err
	}
	defer func() {
		// A leftover temporary directory does not fail the build, but is reported.
		if err := tc.cleanup(); err != nil {
			_, _ = fmt.Fprintf(stderr, "warning: %s: %v\n", t.name, err)
		}
	}()
	for _, w := range tc.warnings {
		if _, err := fmt.Fprintf(stdout, "warning: %s: %s\n", t.name, w); err != nil {
			return err
		}
	}
	out := filepath.Join(o.outDir, t.binaryFile(p.cfg.BinaryName))
	args := goBuildArgs(t, o, tc, out, p.cfg.GoPackage)
	cmd := exec.Command("go", args...)
	cmd.Dir = p.dir
	cmd.Env = append(os.Environ(), tc.env...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if o.verbose {
		if _, err := fmt.Fprintf(stdout, "%s go %s\n", strings.Join(tc.env, " "), strings.Join(args, " ")); err != nil {
			return err
		}
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build: %w", err)
	}
	// c-shared also writes a C header for the exported functions, which an addon does not need.
	header := strings.TrimSuffix(out, ".node") + ".h"
	if err := os.Remove(header); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := checkBinary(out, t); err != nil {
		return err
	}
	st, err := os.Stat(out)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(p.dir, out)
	if err != nil {
		rel = out
	}
	_, err = fmt.Fprintf(stdout, "built %s (%.1f MB, %s, %.1fs)\n", filepath.ToSlash(rel), float64(st.Size())/bytesPerMB, tc.description, time.Since(start).Seconds())
	return err
}

const bytesPerMB = 1 << 20

// goBuildArgs returns the arguments of go build for t.
func goBuildArgs(t target, o buildOptions, tc *toolchain, out, goPackage string) []string {
	args := []string{"build", "-buildmode=c-shared", "-trimpath"}
	if o.tags != "" {
		args = append(args, "-tags="+o.tags)
	}
	var ld []string
	if o.release {
		ld = append(ld, "-s", "-w")
		// Go passes -s to the external linker for ELF, but zig ignores it: strip explicitly.
		if tc.zig && t.goos == "linux" {
			ld = append(ld, "-extldflags=-Wl,--strip-all")
		}
	}
	if o.ldflags != "" {
		ld = append(ld, o.ldflags)
	}
	if len(ld) > 0 {
		args = append(args, "-ldflags="+strings.Join(ld, " "))
	}
	if o.verbose {
		args = append(args, "-v")
	}
	args = append(args, "-o", out)
	args = append(args, o.extra...)
	pkg := filepath.ToSlash(filepath.Clean(goPackage))
	if pkg != "." {
		pkg = "./" + pkg
	}
	return append(args, pkg)
}

// toolchain is the C toolchain of one build.
type toolchain struct {
	env         []string // variables added to the environment of go build
	zig         bool
	description string // for the build report, e.g. "zig cc -target x86_64-linux-gnu.2.17"
	warnings    []string
	cleanup     func() error // removes the temporary files of the toolchain
}

// lookPath finds executables; tests replace it.
var lookPath = exec.LookPath

// toolchainFor chooses the C compiler for t and returns the environment of go build.
func toolchainFor(t target, o buildOptions) (*toolchain, error) {
	tc := &toolchain{cleanup: func() error { return nil }}
	tc.env = []string{"CGO_ENABLED=1", "GOOS=" + t.goos, "GOARCH=" + t.goarch}
	useZig := o.zig == zigAlways
	if o.zig == zigAuto && o.cc == "" {
		switch {
		case t.isCross():
			useZig = true
		case o.release && t.goos == "linux":
			// A native build links against the glibc of this machine and needs it at least as
			// new on the machines of the users: zig links against glibc 2.17.
			if _, err := lookPath("zig"); err == nil {
				useZig = true
			} else {
				tc.warnings = append(tc.warnings, "zig not found: the binary needs a glibc at least as new as the one of this machine (install zig, or pass -zig never)")
			}
		}
	}
	switch {
	case o.cc != "" && !useZig:
		tc.env = append(tc.env, "CC="+o.cc)
		tc.description = o.cc
	case useZig:
		if _, err := lookPath("zig"); err != nil {
			return nil, fmt.Errorf("cross-compiling for %s needs zig (https://ziglang.org/download/) or -cc: %w", t.name, err)
		}
		cc := "zig cc -target " + t.zig
		tc.env = append(tc.env, "CC="+cc)
		tc.zig = true
		tc.description = cc
		if t.goos == "darwin" && runtime.GOOS != "darwin" {
			dir, err := writeDarwinStubs()
			if err != nil {
				return nil, err
			}
			tc.cleanup = func() error { return os.RemoveAll(dir) }
			// Appended to the flags of the environment, if any.
			ldflags := strings.TrimSpace(os.Getenv("CGO_LDFLAGS") + " -L" + dir + " -F" + dir)
			tc.env = append(tc.env, "CGO_LDFLAGS="+ldflags)
		}
	default:
		if t.isCross() {
			return nil, fmt.Errorf("cross-compiling for %s needs zig: -zig never requires -cc", t.name)
		}
		if t.goos == "linux" {
			if musl, err := hostIsMusl(); err != nil {
				return nil, err
			} else if musl {
				return nil, fmt.Errorf("this machine uses musl libc: a native build does not load with glibc (%s); build with -zig always", t.name)
			}
		}
		tc.description = "system C compiler"
		if cc := os.Getenv("CC"); cc != "" {
			tc.description = cc
		}
	}
	return tc, nil
}

// hostIsMusl reports whether this Linux machine uses musl libc, from its ldd (a script or the
// musl loader itself), as the loader in index.js does. The musl loader alone is not proof: glibc
// systems can have it installed.
func hostIsMusl() (bool, error) {
	ldd, err := os.ReadFile("/usr/bin/ldd")
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return bytes.Contains(ldd, []byte("musl")) && !bytes.Contains(ldd, []byte("GNU C Library")), nil
}

// Stubs of the macOS libraries that Go links on darwin (zig ships libSystem only): libresolv
// (internal/syscall/unix) and the CoreFoundation framework (runtime/cgo on arm64, used on iOS
// only). They name the system libraries and the symbols Go imports; dyld loads the real ones.
const (
	libresolvTBD = `--- !tapi-tbd
tbd-version:     4
targets:         [ x86_64-macos, arm64-macos ]
install-name:    '/usr/lib/libresolv.9.dylib'
exports:
  - targets:         [ x86_64-macos, arm64-macos ]
    symbols:         [ _res_9_nclose, _res_9_ninit, _res_9_nsearch ]
...
`
	coreFoundationTBD = `--- !tapi-tbd
tbd-version:     4
targets:         [ x86_64-macos, arm64-macos ]
install-name:    '/System/Library/Frameworks/CoreFoundation.framework/Versions/A/CoreFoundation'
...
`
)

// writeDarwinStubs writes the macOS library stubs in a new temporary directory, usable with -L
// and -F.
func writeDarwinStubs() (string, error) {
	dir, err := os.MkdirTemp("", "napi-go-darwin-")
	if err != nil {
		return "", err
	}
	fw := filepath.Join(dir, "CoreFoundation.framework")
	err = errors.Join(
		os.WriteFile(filepath.Join(dir, "libresolv.tbd"), []byte(libresolvTBD), 0o644),
		os.Mkdir(fw, 0o755),
	)
	if err == nil {
		err = os.WriteFile(filepath.Join(fw, "CoreFoundation.tbd"), []byte(coreFoundationTBD), 0o644)
	}
	if err != nil {
		return "", errors.Join(err, os.RemoveAll(dir))
	}
	return dir, nil
}

// checkBinary verifies that path is a shared library for t: a wrong C compiler (for instance $CC
// of the host in a cross build) would otherwise go unnoticed until the addon fails to load.
func checkBinary(path string, t target) error {
	var err error
	switch t.goos {
	case "linux":
		err = checkELF(path, t)
	case "darwin":
		err = checkMachO(path, t)
	case "windows":
		err = checkPE(path, t)
	default:
		err = fmt.Errorf("no check for %s", t.goos)
	}
	if err != nil {
		return fmt.Errorf("%s is not a binary for %s: %w", path, t.name, err)
	}
	return nil
}

func checkELF(path string, t target) error {
	f, err := elf.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	want := map[string]elf.Machine{"amd64": elf.EM_X86_64, "arm64": elf.EM_AARCH64}[t.goarch]
	if f.Machine != want || f.Type != elf.ET_DYN {
		return fmt.Errorf("ELF %s %s, want %s shared object", f.Machine, f.Type, want)
	}
	return nil
}

func checkMachO(path string, t target) error {
	f, err := macho.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	want := map[string]macho.Cpu{"amd64": macho.CpuAmd64, "arm64": macho.CpuArm64}[t.goarch]
	if f.Cpu != want || f.Type != macho.TypeDylib {
		return fmt.Errorf("Mach-O %s type %d, want %s dylib", f.Cpu, f.Type, want)
	}
	return nil
}

func checkPE(path string, t target) error {
	f, err := pe.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	want := map[string]uint16{"amd64": pe.IMAGE_FILE_MACHINE_AMD64, "arm64": pe.IMAGE_FILE_MACHINE_ARM64}[t.goarch]
	if f.Machine != want || f.Characteristics&pe.IMAGE_FILE_DLL == 0 {
		return fmt.Errorf("PE machine %#x characteristics %#x, want DLL for machine %#x", f.Machine, f.Characteristics, want)
	}
	return nil
}
