# Contributing

Thanks for your interest in OpenAgentPlatform.

## Process

1. Fork the repo and create a branch from `main`.
2. Make your change. Keep PRs small and focused.
3. Run the checks locally:
   ```bash
   make fmt
   make lint
   make test
   ```
4. Open a pull request with a clear description and link to any issue.
5. Address review feedback. Squash-merge once approved.

## Coding standards

### Go

- `gofmt` and `go vet` clean.
- Errors wrapped with `fmt.Errorf("ctx: %w", err)`.
- `log/slog` only — no third-party loggers.
- Public symbols documented.

### Python

- Type hints on every public function.
- `ruff` clean (`make lint`).
- `mypy` clean.
- Async-first; no blocking I/O in request paths.

### TypeScript / React

- `strict: true` in tsconfig.
- No `any` in committed code.
- Components functional; hooks at the top level.
- Tailwind for styling; no inline `style` objects.

## Commit messages

```
<scope>: <imperative summary>

<body explaining why, not what>
```

Examples:
- `server: add mTLS support to NATS client`
- `web: redirect to /dashboard after login`
- `py: add 0001_init migration`

## Reporting issues

Open a GitHub issue with:
- What you expected
- What happened
- Reproduction steps
- Environment (OS, versions)

## Code of conduct

Be respectful. Assume good intent. Help newcomers.
