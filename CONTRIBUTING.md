# Contributing to ghOrgTool

Thank you for taking the time to contribute! This document covers everything you need to get up and running.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Getting Started](#getting-started)
- [Project Structure](#project-structure)
- [Making Changes](#making-changes)
- [Testing](#testing)
- [Submitting a Pull Request](#submitting-a-pull-request)
- [Code Style](#code-style)

---

## Prerequisites

- [Go](https://golang.org/dl/) 1.21 or later (`brew install go` on macOS)
- A GitHub personal access token with **read:org**, **read:user**, and **repo** scopes (needed to run the tool manually against the real API)

## Getting Started

1. **Fork** the repository on GitHub, then clone your fork:

   ```bash
   git clone https://github.com/<your-username>/ghOrgTool.git
   cd ghOrgTool
   ```

2. **Install dependencies:**

   ```bash
   go mod download
   ```

3. **Build the tool** to confirm everything compiles:

   ```bash
   go build ./...
   ```

4. **Run the tests** to confirm the baseline is green:

   ```bash
   go test ./...
   ```

## Project Structure

```
ghOrgTool/
├── main.go          # Entry point, flag definitions, top-level dispatch
├── repos.go         # Repository helpers and access-report logic
├── teams.go         # Team helpers and summarisation
├── users.go         # User validation and login resolution
├── graphlike.go     # GraphQL queries (SSO, team membership via GitHub v4 API)
├── config.go        # Configuration file management (load, save, init, rotate-token)
├── clippy.go        # Clipboard helper (prompt output)
├── *_test.go        # Unit tests – mirror the file they test
└── go.mod / go.sum  # Module definition
```

Key packages used:

| Package | Purpose |
|---|---|
| `github.com/google/go-github/v84` | GitHub REST API (v3) |
| `github.com/shurcooL/githubv4` | GitHub GraphQL API (v4) – SSO, team membership |
| `golang.org/x/oauth2` | Token-based HTTP client |
| `rsc.io/getopt` | POSIX-style flag aliases |

## Making Changes

1. Create a feature branch from `main`:

   ```bash
   git checkout main
   git pull upstream main
   git checkout -b feat/my-feature
   ```

2. Keep changes focused – one logical concern per PR.

3. If you add a new public function, add a corresponding test in the matching `_test.go` file.

4. After your changes, run `go mod tidy` to keep `go.mod` / `go.sum` clean.

5. Bump `version` in `main.go` following [semver](https://semver.org): minor for a new feature or flag, patch for a fix, major for a breaking CLI change.

### Adding a new CLI flag

1. Declare it with `flag.Bool` / `flag.String` etc. near the top of `main()` in `main.go`.
2. Optionally add a short alias with `getopt.Alias`.
3. Add a help-text entry to the `--help` block.
4. Add the handler block in the appropriate section of `main()`, following the existing pattern.
5. Implement the logic in the most relevant `*.go` file.

## Testing

Tests use only the standard library (`testing`, `net/http/httptest`) – no external test frameworks are required.

```bash
# Run all tests
go test ./...

# Run with verbose output
go test -v ./...

# Run a single test
go test -run TestNormalizePermission ./...

# Run with race detector
go test -race ./...
```

### Writing tests

- API-dependent functions should use `newTestClient` from `testhelper_test.go` to spin up a local `httptest.Server` – no real network calls in tests.
- Use `t.Setenv` to override `XDG_CONFIG_HOME` (or `APPDATA` on Windows) when testing config behaviour so tests never touch the real config file.
- Table-driven tests are preferred for functions with multiple input/output cases.

## Submitting a Pull Request

1. Push your branch to your fork:

   ```bash
   git push origin feat/my-feature
   ```

2. Open a pull request against `glow-example-org/ghOrgTool`'s `main` branch.

3. In the PR description, explain:
   - **What** the change does
   - **Why** it is needed
   - Any manual testing steps (the unit tests cover pure logic, but anything requiring live API calls should be described)

4. A reviewer will check:
   - All existing tests still pass (`go test ./...`)
   - New behaviour is covered by tests
   - `go vet ./...` reports no issues
   - `go mod tidy` leaves no diff in `go.mod` / `go.sum`
   - `version` in `main.go` has been bumped appropriately

## Code Style

- Follow standard Go conventions: run `gofmt -w .` before committing (most editors do this automatically).
- Use `go vet ./...` to catch common mistakes.
- Prefer the stdlib `context` package over any third-party context package.
- Avoid `log.Fatal` inside library-style functions – reserve it for top-level dispatch in `main.go`.
- Keep functions small and focused; if a func is getting long, split the logic into a testable helper.
