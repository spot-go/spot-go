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
- To change the Go version, update `mise.toml`, the `go` line in `go.mod` and `go.work`, and the Go requirement in `README.md` together.
