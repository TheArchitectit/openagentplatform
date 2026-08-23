# Contributing

Thank you for contributing to OpenAgentPlatform. Keep changes focused, tested, and easy to review.

## Development setup

### Prerequisites

- Go 1.25 or newer
- Node.js 22 or newer and npm
- Python 3.12 or newer
- Docker and Docker Compose
- GNU Make

### Local environment

1. Fork and clone the repository.
2. Create a branch from an up-to-date `main`:
   ```bash
   git switch main
   git pull --ff-only origin main
   git switch -c <type>/<short-description>
   ```
3. Review `CLAUDE.md`, `INDEX_MAP.md`, and the relevant architecture documents before changing code.
4. Install or start only the dependencies required for your change. Use the Makefile targets as the source of truth.
5. Run the existing tests once before editing so environmental failures are distinguishable from regressions.

Never commit credentials, `.env` files, generated build output, or editor-specific state.

## Development workflow

1. Open or reference an issue that describes the problem and acceptance criteria.
2. Make the smallest change that satisfies those criteria.
3. Add tests for new behavior and regressions.
4. Format and validate the affected stack:
   ```bash
   make fmt
   make lint
   make test
   ```
   For focused Go work, run the affected package directly with `go test` during development.
5. Review `git diff` for accidental changes and secrets.
6. Commit with an imperative, scoped message and push your branch.
7. Open a pull request. Keep follow-up commits focused and respond to review feedback.

Do not force-push shared branches. Prefer a new follow-up commit after review has begun.

## Code style

### Go

- Use the standard library unless an existing approved dependency clearly fits.
- Run `gofmt`; keep `go vet` clean.
- Document exported declarations and wrap errors with context using `%w`.
- Accept `context.Context` for operations that may block or be canceled.
- Make concurrent code race-safe and test state transitions deterministically.

### Python

- Add type hints to public functions.
- Keep `ruff` and `mypy` clean.
- Prefer async I/O in request paths; do not block the event loop.

### TypeScript and React

- Preserve strict TypeScript settings and avoid `any`.
- Use functional components and call hooks only at the top level.
- Follow the existing component and styling conventions.
- Include accessible names, keyboard behavior, focus states, and reduced-motion support.

### Documentation

- Keep documents under 500 lines.
- Update `INDEX_MAP.md` and `HEADER_MAP.md` when adding or substantially changing documentation.
- Explain decisions and trade-offs rather than restating implementation details.

## Commit messages

Use an imperative summary with an optional scope:

```text
<type>(<scope>): <imperative summary>

<body explaining why, when useful>
```

Common types are `feat`, `fix`, `docs`, `test`, `refactor`, and `chore`.

## Pull request template

Include the following in the pull request description:

```markdown
## Summary
- What changed
- Why the change is needed

## Validation
- [ ] Relevant automated tests pass
- [ ] Formatting and lint checks pass
- [ ] Documentation is updated where needed
- [ ] No secrets or generated artifacts are included

## Risk and rollback
- Risk level and affected components
- How to disable or revert the change

## Related work
Closes #<issue-number>
```

Reviewers should be able to understand the behavior change, reproduce validation, and identify a rollback path from the pull request alone.

## Reporting issues

Include expected and actual behavior, minimal reproduction steps, logs with sensitive data removed, and the relevant operating system and tool versions. Report security vulnerabilities privately through the repository security policy rather than a public issue.

## Code of conduct

Be respectful, assume good intent, and help maintain a welcoming environment for contributors of all experience levels.
