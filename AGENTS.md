# Agent instructions

## Go toolchain

Always use [mise](https://mise.jdx.dev) to run Go. The Go version is pinned in `mise.toml`.

- Install the pinned version with `mise install`. If mise says the config file is not trusted, run `mise trust` first.
- Run every Go command through `mise exec --`, for example:
  - `mise exec -- go build ./...`
  - `mise exec -- go vet ./...`
  - `mise exec -- go test ./...`
  - `mise exec -- go mod tidy`
- Do not call the `go` binary found on `PATH` directly. It can be a different version from the one in `mise.toml`, or point to a mismatched `GOROOT`.
- To change the Go version, update `mise.toml`, the `go` line in `go.mod` and `go.work`, and the Go requirements in `README.md` (in [go install](README.md#go-install) and [Running](README.md#running)) together. The release workflows read the version from `go.mod`.
- If the new Go needs a newer macOS, also update `MACOSX_DEPLOYMENT_TARGET` in `.goreleaser.yaml`, the `minos` check in `.github/workflows/release-check.yml` and the macOS version in the README's Requirements. Go's linker has the oldest macOS it supports in `src/cmd/link/internal/ld/macho.go` (`macOS = macVersionFlag{...}`).
