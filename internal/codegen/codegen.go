// Package codegen generates the Layer 2 registrations, the TypeScript declarations and the JS
// entry point of a napi-go addon from annotations in its Go source (the equivalent of #[napi] and
// of the type generation of napi-rs). It is the implementation of napi-go gen.
//
//	// Add returns a + b.
//	//napi:export
//	func Add(a, b float64) float64 { return a + b }
//
// It writes zz_napi.go (an init function calling napi.Export for every annotated declaration),
// index.d.ts and index.js. Types that cannot be converted are reported with their position,
// before the addon is built. See README.md for the annotations and the TypeScript mapping.
package codegen

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Default output file names, relative to the package directory.
const (
	DefaultGoFile  = "zz_napi.go"
	DefaultDTSFile = "index.d.ts"
	DefaultJSFile  = "index.js"
)

// Config is one run of the generator.
type Config struct {
	Dir     string // package directory
	GoFile  string // generated Go file, relative to Dir
	DTSFile string // TypeScript declarations, absolute or relative to Dir; empty: none
	JSFile  string // JS entry point, absolute or relative to Dir; empty: none
	// BinaryName is the base name of the .node files loaded by the JS entry point (default: the
	// base name of Dir).
	BinaryName string
	// PackageName is the npm package of the addon. If set, the JS entry point falls back to the
	// platform packages <PackageName>-<target>.
	PackageName string
	Tags        string // build tags, as for go build
}

// File is a generated file.
type File struct {
	Path    string
	Content []byte
}

// Generate loads the package of cfg and returns its generated files.
func Generate(cfg Config) ([]File, error) {
	dir, err := filepath.Abs(cfg.Dir)
	if err != nil {
		return nil, err
	}
	if cfg.GoFile == "" {
		return nil, errors.New("no generated Go file name")
	}
	if cfg.BinaryName == "" {
		cfg.BinaryName = filepath.Base(dir)
	}
	p, err := load(dir, cfg.Tags, cfg.GoFile)
	if err != nil {
		return nil, err
	}
	m, err := analyze(p)
	if err != nil {
		return nil, err
	}
	goSrc, err := m.goSource()
	if err != nil {
		return nil, err
	}
	outs := []File{{filepath.Join(dir, cfg.GoFile), goSrc}}
	if cfg.DTSFile != "" {
		outs = append(outs, File{resolve(dir, cfg.DTSFile), m.dts()})
	}
	if cfg.JSFile != "" {
		outs = append(outs, File{resolve(dir, cfg.JSFile), m.jsEntry(cfg.BinaryName, cfg.PackageName)})
	}
	return outs, nil
}

// resolve returns path, relative to dir unless absolute.
func resolve(dir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(dir, path)
}

// Write writes the generated files.
func Write(files []File) error {
	for _, f := range files {
		if err := os.WriteFile(f.Path, f.Content, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// ErrOutOfDate is returned by Check when a file on disk differs from its generated content.
var ErrOutOfDate = errors.New("out of date: run napi-go gen")

// Check compares the generated files with the files on disk.
func Check(files []File) error {
	for _, f := range files {
		cur, err := os.ReadFile(f.Path)
		if err != nil {
			return fmt.Errorf("%s: %w (run napi-go gen)", f.Path, err)
		}
		if !bytes.Equal(cur, f.Content) {
			return fmt.Errorf("%s is %w", f.Path, ErrOutOfDate)
		}
	}
	return nil
}
