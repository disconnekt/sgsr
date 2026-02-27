# Repository Guidelines

## Project Structure & Module Organization
- This repository is a small Go library (`module github.com/disconnekt/sgsr`) with source files at the root.
- Core runtime lifecycle lives in `controller.go` (`Config`, `App`, graceful `Run`).
- Embedded static serving lives in `static_embed.go` (preload, precompression, `Accept-Encoding` negotiation).
- Tests are colocated as `*_test.go` files (`controller_test.go`, `static_embed_test.go`).
- Static test fixtures are in `testdata/static/` and are used via `go:embed`.
- Dependency definitions are in `go.mod`/`go.sum`; `vendor/` is present and should stay consistent when vendoring is updated.

## Build, Test, and Development Commands
- `go test ./...` - run all unit tests.
- `go test -v ./...` - run tests with verbose output.
- `go test -cover ./...` - run tests with coverage.
- `go build ./...` - verify the module compiles.
- `go vet ./...` - run Go static checks.
- `gofmt -w $(rg --files -g '*.go')` - format all Go files.

## Coding Style & Naming Conventions
- Follow standard Go style and keep code `gofmt`-clean.
- Exported identifiers use `PascalCase`; unexported identifiers use `camelCase`.
- Add GoDoc comments for exported constants, types, and functions.
- Keep error strings lowercase and concise (no trailing punctuation).
- Prefer small, focused functions and explicit validation for public APIs.

## Testing Guidelines
- Use the standard `testing` package with table-driven tests where useful.
- Test names should follow `Test<Behavior>` conventions.
- Cover both success and failure paths (e.g., invalid config, unsupported encoding, 404/406 flows).
- Keep reusable fixtures under `testdata/`.
- Before opening a PR, run at least: `go test ./...` and `go vet ./...`.

## Commit & Pull Request Guidelines
- Existing history uses short imperative subjects (for example: `switch to fiber`, `fix log message`, `add static handler`).
- Prefer clear one-line commit messages; avoid vague messages like `v1`.
- Recommended commit format: `<area>: <action>` (example: `static: add zstd negotiation`).
- PRs should include a short summary, behavior/API impact, validation commands run, and linked issue (if any).

## Security & Configuration Tips
- Do not commit secrets or private keys in source or `testdata/`.
- Validate route prefixes, filesystem roots, and handler options at API boundaries.
- Be explicit with caching/compression defaults when exposing static assets.
