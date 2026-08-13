# GitHub Actions Release Workflow Design

Date: 2026-08-13

## Summary

Add a GitHub Actions workflow that builds `fsvc` binaries for multiple OS/arch
targets when a version tag (`v*`) is pushed, and attaches them to a GitHub
Release. No golangci-lint, tests, or other checks in this workflow — build and
release only.

## Context

- Go module `fsvc`, `go 1.26.5` (from `go.mod`).
- Binary is built from the repo root: `main.go` with a `version` var overridden
  at build time via `-ldflags="-X main.version=<tag>"` (see README Build).
- No existing `.github/workflows/`.
- Install path referenced in README: `go install github.com/dat267/fsvc@latest`.

## Design

### Workflow: `.github/workflows/release.yml`

- **Name:** `release`
- **Trigger:** `push` with `tags: ['v*']`.
- **Permissions:** `contents: write` (needed to create the release).

### Job: `build` (matrix)

| os | arch |
|---|---|
| linux | amd64, arm64, 386 |
| darwin | amd64, arm64 |
| windows | amd64, arm64 |

Steps:

1. `actions/checkout@v4` with `fetch-depth: 0` so version derivation works.
2. `actions/setup-go@v5` with `go-version-file: go.mod` (version comes from
   `go.mod`, no hardcoding).
3. Build the binary:
   - `VERSION` = `${{ github.ref_name }}` (the tag, e.g. `v1.2.3`).
   - Output name: `fsvc_<os>_<arch>` on unix, `fsvc_<os>_<arch>.exe` on
     windows, under `dist/`.
   - Flags: `-trimpath -ldflags="-s -w -X main.version=${VERSION}"`.
   - Run `go build` only. No `go vet`, no `go test`, no golangci-lint.
4. `actions/upload-artifact@v4` uploading `dist/` (helps debugging and
   cross-referencing the matrix).

### Job: `release`

- `needs: build`, `runs-on: ubuntu-latest`.
- `actions/download-artifact@v4` to gather the built binaries.
- `softprops/action-gh-release@v2`:
  - `tag_name: ${{ github.ref_name }}`
  - `files: dist/**`
  - `generate_release_notes: true`
  - `fail_on_unmatched_files: true`

Result: pushing `v1.2.3` produces a GitHub Release with
`fsvc_linux_amd64`, `fsvc_linux_arm64`, `fsvc_linux_386`,
`fsvc_darwin_amd64`, `fsvc_darwin_arm64`, `fsvc_windows_amd64.exe`,
`fsvc_windows_arm64.exe`.

## Out of scope

- golangci-lint / tests / other checks (explicitly excluded per user).
- goreleaser, checksums/signing, Docker images, Homebrew taps.
- Builds on `main` or PRs (tag-push only).
- Version stamps from `git describe`; tag name is used directly.

## Verification

- YAML syntax validation (e.g. `actionlint` if available, or a parser check).
- Workflow logic review; cannot run GH Actions locally.
