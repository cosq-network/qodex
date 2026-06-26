# Contributing

## Getting Started

1. Fork and clone the repository.
2. Run `go mod tidy` to download dependencies.
3. Run `go build ./cmd/qodex` — the `qodex` binary should appear in the project root.
4. Run `go test -race ./...` — all tests must pass.

## Code Standards

- **Language**: Go 1.26.
- **Style**: Follow `gofmt` and `go vet`. The CI pipeline enforces both.
- **Testing**: All new code must have tests. Use `-race` for all test runs.
- **Packages**: Keep the `internal/` convention. Public packages go in `pkg/` only if they are imported outside the module.
- **Errors**: Wrap errors with `fmt.Errorf("context: %w", err)`. Do not use `errors.Wrap` or third-party error packages.
- **Logging**: Use `fmt.Fprintf(os.Stderr, ...)` for diagnostics. The agent has a `logError` helper.
- **Configuration**: New config keys go in `internal/config/config.go` with validation and defaults.

## Testing

Run the full suite before submitting:

```sh
go test -race -count=1 ./...
```

The CI pipeline runs on every push with the race detector enabled.

## Commit Messages

Write conventional commits:

```
feat: add lsp_diagnostics tool
fix: prevent panic on empty store path
docs: update security model
test: add DetectCapabilities tests
```

## Pull Requests

- Keep PRs focused on one concern.
- Include tests for new functionality.
- Update docs (user guide, roadmap) when adding features.

## Release Process

See [Release Management](docs/release-management.md) for the full process, including:

1. GPG key setup and signing.
2. Tagging and pushing a release.
3. Verification of signatures.
4. Updating the Homebrew formula.
5. Rollback procedures.
