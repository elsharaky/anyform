# Contributing to goform

Thanks for your interest in contributing! `goform` is an open-source project,
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

`goform` follows [Semantic Versioning](https://semver.org/) and releases through
`semantic-release` in the [Version workflow](.github/workflows/main.yml). Two
rules make releases predictable:

1. **Work never releases by itself.** Merging `feat:`, `fix:`, or `perf:`
   commits only *accumulates* changes — no tag, no release. This lets several
   fixes land and ship together.
2. **A marker releases everything accumulated.** A release happens only when a
   PR merged into `main` includes a marker commit. Its scope picks the bump and
   its notes include **every** work commit since the previous release tag:

   | Marker | Bump |
   |---|---|
   | `release(patch): ...` | patch (e.g. `0.1.0 → 0.1.1`) |
   | `release(minor): ...` | minor (e.g. `0.1.x → 0.2.0`) |
   | `release(major): ...` | major (e.g. `0.x.y → 1.0.0`) |

3. **What lands in the notes.** The changelog lists all commits since the last
   tag, grouped by type (`feat:` → Features, `fix:` → Bug Fixes, `perf:` →
   Performance Improvements); `chore:`/`ci:`/`docs:` are invisible. So a marker
   ships exactly the user-facing work it waited for, and nothing else.

The marker commit should be message-only (no code changes). Do not bump
versions in a PR or on the CLI; the pipeline owns versioning.

### Release secret

The [Version workflow](.github/workflows/main.yml) authenticates its git tag
pushes with a fine-grained PAT stored as the `RELEASE_TOKEN` repo secret
(Contents: read/write, Workflows: read/write, scoped to this repo). This is
required — GitHub hard-blocks the default `GITHUB_TOKEN` from pushing any ref
that touches `.github/workflows/`, and the first-release baseline tag points at
the root commit. Without `RELEASE_TOKEN`, the release job fails.

## Code of conduct

Be respectful and constructive. Harassment or abusive behavior is not
tolerated. The maintainers reserve the right to close or remove any
contribution that violates these principles.
