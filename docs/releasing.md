# Releasing napi-go

napi-go is a Go module: a release is a git tag `v<major>.<minor>.<patch>`. Users get it with
`go get github.com/napi-bindings/napi-go@<tag>` and run the CLI of the same version with
`go tool napi-go` (or `go run github.com/napi-bindings/napi-go/cmd/napi-go@<tag>`).

## Checklist

1. **`main` is green**: the workflows `sys`, `cli` and `bench` on the commit to release.
2. **CHANGELOG.md**: the section of the version lists the changes and has the date of the release,
   `## [0.1.0] - 2026-11-20` (the release workflow refuses a section without a date).
3. **Commit** the changelog to `main`, then **tag** that commit and push the tag:

   ```sh
   git tag -a v0.1.0 -m "napi-go 0.1.0"
   git push origin v0.1.0
   ```

4. **The release workflow** ([`release.yml`](../.github/workflows/release.yml)) then:
   - checks the tag against CHANGELOG.md and extracts the release notes;
   - builds the CLI for Linux, macOS and Windows (x64 and arm64), with `SHA256SUMS`, and checks
     that it reports the tag as its version;
   - on Linux, macOS and Windows, creates a project with
     `go run github.com/napi-bindings/napi-go/cmd/napi-go@<tag> new`, fetching the tag from
     GitHub as a user would, then builds and tests it;
   - creates the GitHub release with the notes and the binaries (a pre-release for tags such as
     `v0.2.0-rc.1`).
5. **Make the version known to the Go proxy and pkg.go.dev**:

   ```sh
   GOPROXY=https://proxy.golang.org go list -m github.com/napi-bindings/napi-go@v0.1.0
   ```

   then open `https://pkg.go.dev/github.com/napi-bindings/napi-go@v0.1.0` (the page appears within
   minutes, or request it there).
6. **Start the next section** of CHANGELOG.md: `## [Unreleased]`.

If the release workflow fails, fix the cause on `main` and release a new patch version: a tag
published to the Go proxy cannot be changed. Until the proxy has fetched it, a wrong tag can still
be deleted (`git push origin :refs/tags/v0.1.0`) and created again.

## Versions

- Before 1.0.0 a minor version may change the API; CHANGELOG.md lists the changes.
- napi-go supports the two latest Go releases (with cgo) and the Node.js versions tested in CI
  (currently 22, 24 and 26). Addons declare Node-API 8 unless built with `napi_go_v9` or
  `napi_go_v10`.
- The CLI and the library share the module version: projects pin both with `go.mod`.
