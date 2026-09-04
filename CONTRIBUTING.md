# Contributing to anyform

Thanks for your interest in contributing! `anyform` is an open-source project,
but it is **maintainer-owned**: only the maintainer and collaborators chosen by
the maintainer can merge changes. Everyone else contributes by opening issues
and pull requests, which the maintainers review and decide on.

## Ways to contribute

- **Report a bug** — open an issue using the bug report template.
- **Request a feature** — open an issue using the feature request template.
- **Submit a fix or improvement** — open a pull request.
- **Improve documentation** — typos, examples, and clarifications are welcome.

## Getting started

1. Fork the repository.
2. Clone your fork.
3. Create a branch: `git checkout -b feat/your-brief-name`.
4. Make your changes.
5. Run the checks (below).
6. Open a pull request against `main`.

## Development checks

Before submitting, make sure everything passes:

```bash
go test -race ./...
go vet ./...
golangci-lint run
gosec ./...
govulncheck ./...
```

These same checks run in CI on every pull request.

## Code style

- Run `gofmt` (or `gofumpt`) on your changes.
- Match the surrounding code style and conventions.
- Keep the package dependency-free.
- Use clear, descriptive names.
- Avoid unnecessary comments; document exported identifiers.

## Tests

- All PRs must include tests for new behavior.
- Prefer table-driven tests in `*_test.go` files.
- Runnable examples in `examples_test.go` are encouraged for public APIs.
- If you change behavior, update affected tests.

## Commits

Use [Conventional Commits](https://www.conventionalcommits.org/):

- `feat:`, `fix:`, `docs:`, `test:`, `refactor:`, `chore:`, `perf:`, `build:`

Example: `feat: add support for custom tag priority`

## Versioning & releases

- `anyform` follows [Semantic Versioning](https://semver.org/).
- Releases are created by maintainers via git tags (`vX.Y.Z`) and the
  [release workflow](.github/workflows/release.yml). The Go module proxy picks
  up tagged versions automatically.
- Do not bump versions in a PR unless asked.

## Code of conduct

Be respectful and constructive. Harassment or abusive behavior is not
tolerated. The maintainers reserve the right to close or remove any
contribution that violates these principles.
