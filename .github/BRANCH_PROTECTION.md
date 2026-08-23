# Branch Protection

Configure the `main` branch ruleset in **Settings → Rules → Rulesets**. Repository rulesets are an administrative GitHub setting; this document is the source of truth for the required configuration.

## Required rules

- Require pull requests before merging.
- Require at least one approval and dismiss stale approvals when new commits are pushed.
- Require conversation resolution before merging.
- Require branches to be up to date before merging.
- Require signed commits.
- Block force pushes and branch deletion.
- Apply rules to administrators and automation tokens.

## Required status checks

Select these checks after each workflow has run on `main` at least once:

- `Lint (golangci-lint)`
- `Test (Go 1.22 on ubuntu-latest)`
- `Test (Go 1.23 on ubuntu-latest)`
- `Lint`
- `Test (Node 20)`
- `Test (Node 22)`
- `Build`
- `Lint (ruff)`
- `Test (Python 3.11 on ubuntu-latest)`
- `Test (Python 3.12 on ubuntu-latest)`
- `Guardrail Security Gate`
- `Go Vulnerability Scan`
- `Web Dependency Audit`
- `CodeQL (go)`
- `CodeQL (javascript-typescript)`
- `CodeQL (python)`
- `Scan for Leaked Secrets`

Keep release publishing outside the required checks. The release workflow runs only for semantic-version tags or an explicit manual dispatch.
