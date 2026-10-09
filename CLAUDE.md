# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`ghOrgTool` is a single-binary Go CLI (`github.com/glow-mdsol/ghOrgTool`) for auditing and managing GitHub organization access: user prerequisite checks (public email, 2FA, SSO, org membership), team/repo access reports, admin-grant/removal auditing, Dependabot enablement, and Actions storage reporting. It's a flat, single-`package main` module — no internal packages or subdirectories to navigate.

## Commands

```bash
go build ./...                                  # build
go vet ./...                                     # vet
go test ./...                                     # run all tests
go test -v ./...                                  # verbose
go test -run TestNormalizePermission ./...        # run a single test by name
go test -race -count=1 -coverprofile=coverage.out ./...   # matches CI
go mod tidy                                       # after adding/removing deps; must leave no diff
gofmt -w .                                        # format before committing
```

CI (`.github/workflows/test.yml`) runs build, vet, and `go test -race` with coverage, and installs `libx11-dev libxfixes-dev xvfb` (needed because `golang.design/x/clipboard` is a real dependency) — run coverage-generating tests locally under `xvfb-run` on Linux if clipboard code is touched. Coverage threshold is currently a low 20% floor, not a target to defend.

No test framework beyond stdlib `testing` + `net/http/httptest`. Tests never hit the real network:
- `newTestClient(mux)` (`testhelper_test.go`) spins up an `httptest.Server` and points a `*github.Client` at it — use this for anything calling the REST (v3) API.
- GraphQL (v4) calls in `graphlike.go` are tested by pointing `githubv4.NewClient` at a similar local test server (see `main_test.go`).
- Use `t.Setenv("XDG_CONFIG_HOME", ...)` (or `APPDATA` on Windows) to sandbox config-file tests away from the real `~/.config/ghOrgTool`.
- Table-driven tests are the norm; `*_test.go` files mirror the source file they test.

## Architecture

Everything lives in `package main` at the repo root:

| File | Responsibility |
|---|---|
| `main.go` | Flag definitions (`flag` + `rsc.io/getopt` for short aliases), `connect()` (token resolution + GitHub client setup), entity-type detection (`detectEntityType`), GitHub-URL parsing (`decomposeGithubURL`/`normalizeEntityArgument`), and the top-level dispatch in `main()` |
| `repos.go` | The bulk of the logic: repo/team access reports, admin-grant and admin-removal audit log scanning, Dependabot enablement, Actions cache/storage reporting, CSV streaming for the all-org admin report |
| `teams.go` | Team lookup, membership, and summarization helpers |
| `users.go` | User validation (`userPrerequisites`, `meetsOrgPrequisites`, `meets2FAPrerequisites`, `meetsSSOPrequisites`), login resolution |
| `graphlike.go` | GraphQL (v4 API) queries: SSO status (`userIsSSO`), email→login lookup (`findUserByEmail`), team membership (`getUserTeams`) — things the REST API can't answer |
| `config.go` | Config file load/save/init/rotate-token, XDG-style config dir resolution, token-source priority |
| `clippy.go` | Clipboard helper (`prompt`) — copies suggested remediation text/links to the clipboard when a check fails |

**Entity resolution flow**: most commands take a bare argument that could be a username, an email, a repo name, or a pasted GitHub URL. `normalizeEntityArgument` unwraps URLs (including Slack's `<url|label>` wrapping) down to a bare slug, then `detectEntityType` decides user vs. repo vs. unknown — repos take priority over usernames, and a user must be a confirmed org member to count. Follow this pattern when adding a new command that accepts the same kind of argument.

**Auth/config priority** (`connect()` in `main.go`, resolution helpers in `config.go`): `GITHUB_AUTH_TOKEN` env var → `github_token` in the config file → `.netrc` entry for `api.github.com`. Org login and default team resolve the same way (env/config → hardcoded defaults `DefaultOrgLogin`/`DefaultTeamName`).

**Client duality**: the code carries both a REST client (`*github.Client` from `go-github/v84`) and a raw `*http.Client` (`tc`) for GraphQL calls via `shurcooL/githubv4`. Functions needing SSO/team-membership data take `tc`; most other functions take `client`. New functions should follow whichever the underlying GitHub API surface requires.

**Large all-org scans** (`--admin-user-report`, `--recent-admin-grants`, `--recent-admin-removals`): these page through every repo in the org with a bounded worker pool (`adminRepoWorkerCount`) and stream results in deterministic repo order so they're resumable. `--admin-user-report` supports `--resume-after-repo` and `--checkpoint-file` for restarting a long scan after a partial failure — the checkpoint is only advanced once a repo's rows are fully flushed to CSV, so re-running with the same checkpoint file is safe to append. Streamed CSV rows are `repo,user,access_type,team_name,access_url`, with `access_type` of `team` (team_name populated) or `collaborator` (team_name empty).

**Two different data sources for admin auditing** — don't conflate them when debugging a report that looks wrong:
- `--admin-user-report` enumerates *current* state via the REST API (repo collaborators + team membership) across every repo. No special token scope beyond normal repo/org read access.
- `--recent-admin-grants`/`--recent-admin-removals` scan the *organisation audit log* (`repo.add_member`/`repo.remove_member` events, `findRecentAdminGrants`/`findRecentAdminRemovals` in `repos.go`) within a lookback window, then re-confirm each match still holds (or still lacks) admin via the REST API to filter out grants/removals that were since reversed. The audit log requires the token to belong to an **org owner** — a plain repo-scoped token will fail here even though it works fine for `--admin-user-report`. The lookback window is 48h, extended to 72h on Monday runs (`adminGrantLookbackSince`) so a Friday-evening change is still caught after the weekend.

**CLI flag pattern**: flags are declared with `flag.Bool`/`flag.String` in `main()`, given a short alias via `getopt.Alias`, documented in the `--help` block, and dispatched via a big `switch`/`if` chain later in `main()`. When adding a new flag, touch all four spots (see `CONTRIBUTING.md` for the full checklist).

## Notes

- `log.Fatal` is reserved for `main.go` top-level dispatch — library-style functions in the other files should return errors instead.
- Untracked `admin-user-report.checkpoint` and `admin-users.csv` files in the repo root are local run artifacts from `--admin-user-report`/`--checkpoint-file`, not project files — don't treat them as source.
