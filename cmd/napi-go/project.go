package main

// project.go — the addon project: package.json and its "napiGo" configuration.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/napi-bindings/napi-go/internal/codegen"
)

// packageJSONFile is the npm manifest of a project.
const packageJSONFile = "package.json"

// configKey is the key of package.json holding the napi-go configuration.
const configKey = "napiGo"

// config is the "napiGo" object of package.json. Every field is optional.
type config struct {
	// BinaryName is the base name of the .node files (default: the package name without scope).
	BinaryName string `json:"binaryName"`
	// GoPackage is the directory of the Go main package, relative to package.json (default ".").
	GoPackage string `json:"goPackage"`
	// Targets are built by build -target all and published (default: defaultTargets).
	Targets []string `json:"targets"`
	// Tags are Go build tags, as for go build -tags.
	Tags string `json:"tags"`
	// Codegen runs napi-go gen before every build (default true).
	Codegen *bool `json:"codegen"`
	// JS and DTS are the generated entry point and declarations, relative to package.json
	// (defaults index.js and index.d.ts).
	JS  string `json:"js"`
	DTS string `json:"dts"`
	// NpmDir holds the platform packages (default "npm").
	NpmDir string `json:"npmDir"`
}

// project is an addon: the directory of its package.json, or a bare Go package.
type project struct {
	dir     string      // absolute
	pkg     *jsonObject // package.json, nil if there is none
	name    string      // npm package name, empty without package.json
	version string
	cfg     config
}

// loadProject reads the project rooted at dir. Without package.json the project is the Go package
// in dir, named after the directory.
func loadProject(dir string) (*project, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	p := &project{dir: abs}
	pkg, err := readJSONObject(filepath.Join(abs, packageJSONFile))
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		p.pkg = pkg
		if _, err := pkg.get("name", &p.name); err != nil {
			return nil, err
		}
		if _, err := pkg.get("version", &p.version); err != nil {
			return nil, err
		}
		if _, err := pkg.get(configKey, &p.cfg); err != nil {
			return nil, fmt.Errorf("%s: %w", packageJSONFile, err)
		}
	}
	p.applyDefaults()
	if err := p.validate(); err != nil {
		return nil, fmt.Errorf("%s: %s: %w", filepath.Join(abs, packageJSONFile), configKey, err)
	}
	return p, nil
}

func (p *project) applyDefaults() {
	c := &p.cfg
	if c.BinaryName == "" {
		c.BinaryName = defaultBinaryName(p.name, p.dir)
	}
	if c.GoPackage == "" {
		c.GoPackage = "."
	}
	if len(c.Targets) == 0 {
		c.Targets = defaultTargets
	}
	if c.JS == "" {
		c.JS = codegen.DefaultJSFile
	}
	if c.DTS == "" {
		c.DTS = codegen.DefaultDTSFile
	}
	if c.NpmDir == "" {
		c.NpmDir = "npm"
	}
}

func (p *project) validate() error {
	if !validBinaryName(p.cfg.BinaryName) {
		return fmt.Errorf("binaryName %q: use letters, digits, '-', '_' and '.' only", p.cfg.BinaryName)
	}
	for _, t := range p.cfg.Targets {
		if _, err := lookupTarget(t); err != nil {
			return err
		}
	}
	for _, rel := range []string{p.cfg.GoPackage, p.cfg.JS, p.cfg.DTS, p.cfg.NpmDir} {
		if filepath.IsAbs(rel) {
			return fmt.Errorf("%q: paths are relative to %s", rel, packageJSONFile)
		}
	}
	return nil
}

// defaultBinaryName is the package name without its scope, or the directory name.
func defaultBinaryName(pkgName, dir string) string {
	if pkgName == "" {
		return filepath.Base(dir)
	}
	if i := strings.LastIndexByte(pkgName, '/'); i >= 0 {
		return pkgName[i+1:]
	}
	return pkgName
}

// validBinaryName accepts the names that are safe as a file name and in a JS string.
func validBinaryName(s string) bool {
	if s == "" || s == "." || s == ".." {
		return false
	}
	for _, r := range s {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.'
		if !ok {
			return false
		}
	}
	return true
}

// path returns rel, relative to the project directory.
func (p *project) path(rel string) string {
	return filepath.Join(p.dir, rel)
}

// goPackageDir is the directory of the Go main package.
func (p *project) goPackageDir() string { return p.path(p.cfg.GoPackage) }

// codegenEnabled reports whether build runs gen first.
func (p *project) codegenEnabled() bool {
	return p.cfg.Codegen == nil || *p.cfg.Codegen
}

// codegenConfig is the generator configuration of the project.
func (p *project) codegenConfig() codegen.Config {
	return codegen.Config{
		Dir:         p.goPackageDir(),
		GoFile:      codegen.DefaultGoFile,
		DTSFile:     p.path(p.cfg.DTS),
		JSFile:      p.path(p.cfg.JS),
		BinaryName:  p.cfg.BinaryName,
		PackageName: p.name,
		Tags:        p.cfg.Tags,
	}
}

// requirePackageJSON fails for commands that need the npm manifest.
func (p *project) requirePackageJSON(command string) error {
	switch {
	case p.pkg == nil:
		return fmt.Errorf("%s needs a %s in %s", command, packageJSONFile, p.dir)
	case p.name == "":
		return fmt.Errorf("%s: %s has no name", command, packageJSONFile)
	case p.version == "":
		return fmt.Errorf("%s: %s has no version", command, packageJSONFile)
	}
	return nil
}

// writePackageJSON writes back package.json, keeping the order of its keys.
func (p *project) writePackageJSON() error {
	return writeJSONObject(p.path(packageJSONFile), p.pkg)
}

// fileExists reports whether path is an existing regular file.
func fileExists(path string) (bool, error) {
	st, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return st.Mode().IsRegular(), nil
}
