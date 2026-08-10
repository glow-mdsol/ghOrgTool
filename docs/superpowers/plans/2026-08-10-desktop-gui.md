# Desktop GUI + Tray Widget Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship an installable, cross-platform (macOS/Windows/Linux) tray application for `ghOrgTool` that gives full GUI parity with the existing CLI, plus login autostart.

**Architecture:** Split today's flat `package main` into `internal/ghorg` (business logic, no `log.Fatal`/`os.Exit`, callable from any process) and two thin front ends: `cmd/ghOrgTool` (today's CLI, unchanged behavior) and `cmd/ghOrgToolGUI` (new Fyne tray app). The GUI reuses `internal/ghorg`'s existing find/format/report layering — most report commands already separate data-fetch from string-formatting, so the GUI mostly just displays the same text the CLI prints today.

**Tech Stack:** Go 1.25, `fyne.io/fyne/v2` (GUI + system tray), existing deps (`go-github/v84`, `shurcooL/githubv4`, `golang.design/x/clipboard`, `rsc.io/getopt`).

## Global Constraints

- No change to any existing CLI flag, output text, or config file format (spec: Summary).
- `internal/ghorg` must never call `log.Fatal` or `os.Exit` — every failure path returns an `error` (spec: Error handling).
- `cmd/ghOrgTool` must preserve today's exact CLI behavior (same fatal-vs-continue semantics) after the refactor (spec: Error handling).
- `cmd/ghOrgToolGUI` never calls `log.Fatal`/`os.Exit`; all errors surface as an inline banner (spec: Error handling).
- V1 covers full CLI parity, including mutating commands, gated behind a confirm dialog (spec: Goals).
- Code signing/notarization and CI-automated packaging are explicitly out of scope (spec: Non-goals).
- Auto-launch at login defaults to **on**, toggleable from the tray menu (spec: Goals, Non-goals).
- `go test ./...` (with `-race`), `go vet ./...`, and `go build ./...` must stay green after every task — these are the CI gates (`.github/workflows/test.yml`).

---

## File Structure

```
internal/ghorg/
  config.go        # moved from root; + DefaultOrgLogin/DefaultTeamName/TokenEnvVar/DefaultAcceptableDomains/ORG
  config_test.go    # moved
  clippy.go         # moved; Prompt exported
  graphlike.go      # moved; + ReportUserTeams/formatUserTeamsReport
  users.go          # moved; + UserIsValid (relocated from main.go)
  users_test.go     # moved; + UserIsValid tests
  teams.go          # moved; GetTeamByName/CheckAndAddMember/SummarizeTeam exported
  teams_test.go     # moved
  repos.go          # moved; wrapper functions return (string, error); + ReportRepositoryTeams
  repos_test.go     # moved; wrapper-function tests updated for new signatures
  client.go         # NEW: Connect() (relocated + restructured from main.go's connect())
  client_test.go    # NEW

cmd/ghOrgTool/
  main.go           # moved from root; calls into ghorg.*, dispatch logic unchanged
  main_test.go      # moved

cmd/ghOrgToolGUI/
  main.go           # entry point: config load, tray menu, autostart bootstrap
  commands.go       # command registry (Command/Field structs + allCommands table)
  commands_test.go
  window.go         # shared output window: form rendering, goroutine dispatch, streaming
  formvalues.go     # pure form-values -> args mapping (extracted for testability)
  formvalues_test.go
  confirm.go         # confirmation-gating logic + dialog wiring
  confirm_test.go
  settings.go         # settings window bound to ghorg.LoadConfig/SaveConfig
  autostart.go         # Enable/Disable/IsEnabled via runtime.GOOS
  autostart_test.go

Makefile                # NEW: package target(s) using fyne package/fyne-cross
README.md               # updated: go install path, GUI install/usage section
CONTRIBUTING.md          # updated: project structure, GUI testing pattern
```

---

## Task 1: Scaffold `internal/ghorg`, move `config.go`

**Files:**
- Create: `internal/ghorg/config.go` (moved from `config.go`)
- Create: `internal/ghorg/config_test.go` (moved from `config_test.go`)
- Delete: `config.go`, `config_test.go`

**Interfaces:**
- Produces: `ghorg.Config`, `ghorg.LoadConfig() *Config`, `ghorg.SaveConfig(*Config) error`, `ghorg.GetDefaultTeam() string`, `ghorg.GetOrgLogin() string`, `ghorg.GetGithubToken() string`, `ghorg.GetAcceptableDomains() []string`, `ghorg.InitConfig() error`, `ghorg.RotateToken() error`, `ghorg.DefaultOrgLogin`, `ghorg.DefaultTeamName`, `ghorg.TokenEnvVar`, `ghorg.DefaultAcceptableDomains`, `ghorg.ORG` (package var).

- [ ] **Step 1: Create the package directory and move the files**

```bash
mkdir -p internal/ghorg
git mv config.go internal/ghorg/config.go
git mv config_test.go internal/ghorg/config_test.go
```

- [ ] **Step 2: Change the package declaration and add the relocated constants/var**

In `internal/ghorg/config.go`, change `package main` to `package ghorg`, then add these lines right after the `Config` struct (moved from `main.go`, where they currently live):

```go
// Default values
const DefaultOrgLogin = "example-org"
const DefaultTeamName = "Default Team"
const TokenEnvVar = "GITHUB_AUTH_TOKEN"

var DefaultAcceptableDomains = []string{"example.com", "example.org", "example.net"}

// ORG is a runtime-resolved organization login loaded from config.
var ORG = DefaultOrgLogin
```

- [ ] **Step 3: Capitalize every function that other packages will need to call**

Rename in `internal/ghorg/config.go` (definition + all internal call sites within the file): `loadConfig`→`LoadConfig`, `saveConfig`→`SaveConfig`, `getDefaultTeam`→`GetDefaultTeam`, `getOrgLogin`→`GetOrgLogin`, `getGithubToken`→`GetGithubToken`, `getAcceptableDomains`→`GetAcceptableDomains`, `initConfig`→`InitConfig`, `rotateToken`→`RotateToken`. Leave `getConfigDir` and `getConfigPath` lowercase (only used inside this file).

- [ ] **Step 4: Update `internal/ghorg/config_test.go`**

Change `package main` to `package ghorg`, then update every call site to the new capitalized names (e.g. `loadConfig()` → `LoadConfig()`, `DefaultOrgLogin` reference stays the same name since it didn't change case, `getDefaultTeam()` → `GetDefaultTeam()`, etc). Also rename any direct references to `getConfigDir`/`getConfigPath` — those stay lowercase, no change needed there.

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/ghorg/... -run TestConfig -v`
Expected: PASS (same assertions as before, just against the renamed functions)

- [ ] **Step 6: Commit**

```bash
git add internal/ghorg/config.go internal/ghorg/config_test.go
git commit -m "refactor: move config.go into internal/ghorg, export public API"
```

---

## Task 2: Move `clippy.go`, export `Prompt`

**Files:**
- Create: `internal/ghorg/clippy.go` (moved from `clippy.go`)
- Delete: `clippy.go`

**Interfaces:**
- Produces: `ghorg.Prompt(content string)`

- [ ] **Step 1: Move and rename**

```bash
git mv clippy.go internal/ghorg/clippy.go
```

- [ ] **Step 2: Update the file**

Change `package main` to `package ghorg`, and rename `func prompt(content string)` to `func Prompt(content string)`. No other logic changes — the file becomes:

```go
package ghorg

import (
	"fmt"

	"golang.design/x/clipboard"
)

// Prompt prints content and, if a clipboard is available, copies it too.
func Prompt(content string) {
	fmt.Println(content)
	err := clipboard.Init()
	if err == nil {
		clipboard.Write(clipboard.FmtText, []byte(content))
	}
}
```

- [ ] **Step 3: Verify it builds**

Run: `go build ./internal/ghorg/...`
Expected: succeeds (no other file references `prompt` yet — that comes in later tasks)

- [ ] **Step 4: Commit**

```bash
git add internal/ghorg/clippy.go
git commit -m "refactor: move clippy.go into internal/ghorg, export Prompt"
```

---

## Task 3: Move `graphlike.go`, add `ReportUserTeams`

**Files:**
- Create: `internal/ghorg/graphlike.go` (moved from `graphlike.go`)
- Modify: `internal/ghorg/graphlike.go` — add `ReportUserTeams`
- Delete: `graphlike.go`

**Interfaces:**
- Consumes: `teamInfo` (defined in this file, unexported, package-internal)
- Produces: `ghorg.ReportUserTeams(ctx context.Context, tc *http.Client, org, userLogin string) (string, error)`

- [ ] **Step 1: Move and rename package**

```bash
git mv graphlike.go internal/ghorg/graphlike.go
```

Change `package main` to `package ghorg` at the top of `internal/ghorg/graphlike.go`. No other change to `getUserTeams`, `findUserByEmail`, `userIsSSO`, or the `teamInfo`/`teamNode` types — they stay unexported and are only called from within the package.

- [ ] **Step 2: Write the failing test for the new report function**

Create `internal/ghorg/graphlike_test.go`:

```go
package ghorg

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReportUserTeams_ListsTeams(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]interface{}{
			"data": map[string]interface{}{
				"organization": map[string]interface{}{
					"id":    "org1",
					"login": "example-org",
					"teams": map[string]interface{}{
						"totalCount": 1,
						"nodes": []map[string]interface{}{
							{"id": "t1", "name": "Team Alpha", "slug": "team-alpha", "description": "", "url": "https://github.com/orgs/example-org/teams/team-alpha"},
						},
					},
				},
			},
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	tc := &http.Client{Transport: rewriteTransport{target: server.URL}}

	out, err := ReportUserTeams(context.Background(), tc, "example-org", "someuser")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "someuser is a member of the following teams") {
		t.Errorf("expected header in output, got: %s", out)
	}
	if !strings.Contains(out, "Team Alpha") {
		t.Errorf("expected team name in output, got: %s", out)
	}
}
```

This test needs a `rewriteTransport` helper that redirects githubv4's fixed `https://api.github.com/graphql` target to the local test server — check `main_test.go` (moving to `cmd/ghOrgTool/main_test.go` in Task 8) for whether this helper already exists there; if it does, move it into `internal/ghorg/testhelper_test.go` instead of duplicating it, since GraphQL tests in this package need it too. If it doesn't exist yet, add it to `internal/ghorg/testhelper_test.go`:

```go
// rewriteTransport redirects all requests to target, preserving path/query.
type rewriteTransport struct {
	target string
}

func (t rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	targetURL, err := url.Parse(t.target)
	if err != nil {
		return nil, err
	}
	req.URL.Scheme = targetURL.Scheme
	req.URL.Host = targetURL.Host
	return http.DefaultTransport.RoundTrip(req)
}
```

(Add `"net/url"` to `testhelper_test.go`'s imports if not already present.)

- [ ] **Step 2b: Run it to confirm it fails**

Run: `go test ./internal/ghorg/... -run TestReportUserTeams -v`
Expected: FAIL with "undefined: ReportUserTeams"

- [ ] **Step 3: Implement `ReportUserTeams`**

Add to `internal/ghorg/graphlike.go`:

```go
// formatUserTeamsReport renders a user's team memberships as CLI-style text.
func formatUserTeamsReport(userLogin string, teams []teamInfo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "User %s is a member of the following teams:\n", userLogin)
	for _, team := range teams {
		fmt.Fprintf(&b, "* %s (%s)\n", team.name, team.url)
	}
	return b.String()
}

// ReportUserTeams fetches and formats the teams a user belongs to in org.
func ReportUserTeams(ctx context.Context, tc *http.Client, org, userLogin string) (string, error) {
	teams, err := getUserTeams(ctx, tc, org, userLogin)
	if err != nil {
		return "", err
	}
	return formatUserTeamsReport(userLogin, teams), nil
}
```

Add `"fmt"` and `"strings"` to the file's import block if not already present.

- [ ] **Step 4: Run the test again**

Run: `go test ./internal/ghorg/... -run TestReportUserTeams -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ghorg/graphlike.go internal/ghorg/graphlike_test.go internal/ghorg/testhelper_test.go
git commit -m "refactor: move graphlike.go into internal/ghorg, add ReportUserTeams"
```

---

## Task 4: Move `users.go`, remove fatal error handling, relocate `UserIsValid`

**Files:**
- Create: `internal/ghorg/users.go` (moved from `users.go`)
- Create: `internal/ghorg/users_test.go` (moved from `users_test.go`)
- Delete: `users.go`, `users_test.go`

**Interfaces:**
- Consumes: `ghorg.ORG` (Task 1), `ghorg.Prompt` is NOT called from this file anymore (moved to caller) — `contains` (helper, moved here from `main.go`, stays unexported)
- Produces: `ghorg.ResolveLogin(ctx, tc, entitySlug *string) (string, error)`, `ghorg.IsUser(ctx, client, entitySlug *string) bool`, `ghorg.UserIsValid(ctx, client, tc, userLogin string) (bool, *github.User, []string)`

- [ ] **Step 1: Move the files**

```bash
git mv users.go internal/ghorg/users.go
git mv users_test.go internal/ghorg/users_test.go
```

Change `package main` to `package ghorg` in both.

- [ ] **Step 2: Export `resolveLogin` and `isUser`**

Rename `resolveLogin` → `ResolveLogin` and `isUser` → `IsUser` (definitions + the internal call to `resolveLogin` inside itself, and the call to `findUserByEmail`/`ORG` stay as-is — `ORG` now resolves to the package var from Task 1 automatically since it's the same package).

- [ ] **Step 3: Write the failing test for non-fatal `userPrerequisites`**

`userPrerequisites` currently calls `log.Fatal` on a missing email/name/bad domain and `prompt()` as a side effect. Replace it with an error-returning version with no `prompt()` call. Add to `internal/ghorg/users_test.go`:

```go
func TestUserPrerequisites_NoPublicEmail(t *testing.T) {
	ctx := context.Background()
	mux := http.NewServeMux()
	mux.HandleFunc("/users/someuser", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]interface{}{"login": "someuser", "name": "Some User"})
	})
	client, teardown := newTestClient(mux)
	defer teardown()

	userID := "someuser"
	_, err := userPrerequisites(ctx, client, &userID)
	if err == nil {
		t.Fatal("expected an error for a user with no public email, got nil")
	}
	if !strings.Contains(err.Error(), "no public email") {
		t.Errorf("expected 'no public email' in error, got: %v", err)
	}
}
```

(Add `"strings"` to the test file's imports if not already present. Check the existing test file for a helper resembling `newTestClient` — it's defined in `testhelper_test.go`, which moves alongside these files in Task 6/8; if `testhelper_test.go` hasn't moved into `internal/ghorg` yet by the time you do this task, move it now instead — `internal/ghorg` needs it before any of its own `_test.go` files using `newTestClient` will compile.)

- [ ] **Step 3b: Move `testhelper_test.go` now if not already done**

```bash
git mv testhelper_test.go internal/ghorg/testhelper_test.go
```

Change `package main` to `package ghorg`.

- [ ] **Step 4: Run the test to confirm it fails**

Run: `go test ./internal/ghorg/... -run TestUserPrerequisites_NoPublicEmail -v`
Expected: FAIL with "undefined: userPrerequisites" (signature doesn't match yet) or a compile error — either way, confirms the new behavior doesn't exist yet.

- [ ] **Step 5: Rewrite `userPrerequisites` to return an error instead of calling `log.Fatal`/`prompt`**

Replace the existing `userPrerequisites` function body with:

```go
// userPrerequisites checks the prerequisites for a user and returns an error
// describing the first failing check, if any.
func userPrerequisites(ctx context.Context, client *github.Client, userId *string) (*github.User, error) {
	ghUser, resp, err := client.Users.Get(ctx, *userId)
	if err != nil {
		return nil, fmt.Errorf("error while getting user %s: %w", *userId, err)
	}
	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("user %s not found", *userId)
	}
	if ghUser.Email == nil {
		return ghUser, fmt.Errorf("the account %s is non-conformant (no public email), please "+
			"check the instructions in the room topic ( fix on https://github.com/settings/profile )", *userId)
	}
	if ghUser.Name == nil {
		return ghUser, fmt.Errorf("the account %s is non-conformant (no name), please "+
			"check the instructions in the room topic ( fix on https://github.com/settings/profile )", *userId)
	}

	parts := strings.Split(*ghUser.Email, "@")
	acceptableDomains := GetAcceptableDomains()
	if !contains(acceptableDomains, parts[1]) {
		return ghUser, fmt.Errorf("the account %s (email %s) is non-conformant (incorrect mail domain), "+
			"please check the instructions in the room topic", *userId, *ghUser.Email)
	}

	log.Println("Validated Pre-requisites for", *userId, "GitHub Email:", *ghUser.Email)
	return ghUser, nil
}
```

Add the `contains` helper (moved from `main.go`, package-private, only used here) right below it:

```go
func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}
```

- [ ] **Step 6: Run the test again**

Run: `go test ./internal/ghorg/... -run TestUserPrerequisites_NoPublicEmail -v`
Expected: PASS

- [ ] **Step 7: Write the failing test for `UserIsValid`**

`UserIsValid` is relocated from `main.go` (currently `userIsValid`) and restructured to return failure reasons instead of calling `prompt()`/`log` itself. Add:

```go
func TestUserIsValid_NotOrgMember(t *testing.T) {
	ctx := context.Background()
	mux := http.NewServeMux()
	mux.HandleFunc("/users/someuser", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]interface{}{"login": "someuser", "name": "Some User", "email": "someuser@example.com"})
	})
	mux.HandleFunc("/orgs/example-org/members/someuser", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	})
	client, teardown := newTestClient(mux)
	defer teardown()

	ORG = "example-org"
	valid, ghUser, reasons := UserIsValid(ctx, client, nil, "someuser")
	if valid {
		t.Fatal("expected invalid result for a non-member")
	}
	if ghUser == nil || *ghUser.Login != "someuser" {
		t.Fatalf("expected resolved user someuser, got: %v", ghUser)
	}
	if len(reasons) != 1 || !strings.Contains(reasons[0], "not a member of organisation") {
		t.Errorf("expected one 'not a member' reason, got: %v", reasons)
	}
}
```

- [ ] **Step 8: Run it to confirm it fails**

Run: `go test ./internal/ghorg/... -run TestUserIsValid_NotOrgMember -v`
Expected: FAIL with "undefined: UserIsValid"

- [ ] **Step 9: Implement `UserIsValid`**

Add to `internal/ghorg/users.go` (this is the function relocated from `main.go`'s `userIsValid`, restructured to collect reasons instead of prompting/logging and exiting on the first failure):

```go
// UserIsValid checks a user against org, SSO, and 2FA prerequisites. It never
// prints or copies to the clipboard — callers render the returned reasons
// however is appropriate for their UI (CLI: Prompt + log; GUI: an output pane).
func UserIsValid(ctx context.Context, client *github.Client, tc *http.Client, userLogin string) (bool, *github.User, []string) {
	ghUser, err := userPrerequisites(ctx, client, &userLogin)
	if err != nil {
		return false, ghUser, []string{err.Error()}
	}

	var reasons []string

	if ok, code := meetsOrgPrequisites(ctx, client, ghUser); !ok && code == 1 {
		reasons = append(reasons, fmt.Sprintf("User %s is not a member of organisation %s", *ghUser.Login, ORG))
	}

	if len(reasons) == 0 {
		if ok, _ := meetsSSOPrequisites(ctx, tc, ghUser); !ok {
			reasons = append(reasons, fmt.Sprintf("User %s is not SSO Enabled", *ghUser.Login))
		}
	}

	if len(reasons) == 0 {
		if ok, _ := meets2FAPrerequisites(ctx, client, ghUser); !ok {
			reasons = append(reasons, fmt.Sprintf("User %s does not have 2FA enabled", *ghUser.Login))
		}
	}

	return len(reasons) == 0, ghUser, reasons
}
```

`meetsOrgPrequisites`, `meetsSSOPrequisites`, and `meets2FAPrerequisites` stay exactly as they are today (unexported, still read the package-level `ORG` var) — no changes needed to them.

- [ ] **Step 10: Run the test again**

Run: `go test ./internal/ghorg/... -run TestUserIsValid_NotOrgMember -v`
Expected: PASS

- [ ] **Step 11: Run the full package test suite**

Run: `go test ./internal/ghorg/... -v`
Expected: PASS (existing `users_test.go` tests that called `userPrerequisites`/`userIsValid` directly will fail to compile until updated — update their call sites to match the new signatures: `ghUser, err := userPrerequisites(...)` instead of `ghUser := userPrerequisites(...)`, and update any assertions that relied on `log.Fatal` exiting the test process, which shouldn't exist since tests can't easily test `log.Fatal` anyway).

- [ ] **Step 12: Commit**

```bash
git add internal/ghorg/users.go internal/ghorg/users_test.go internal/ghorg/testhelper_test.go
git commit -m "refactor: move users.go into internal/ghorg, remove log.Fatal/prompt from prerequisite checks"
```

---

## Task 5: Move `teams.go`, remove fatal error handling

**Files:**
- Create: `internal/ghorg/teams.go` (moved from `teams.go`)
- Create: `internal/ghorg/teams_test.go` (moved from `teams_test.go`)
- Delete: `teams.go`, `teams_test.go`

**Interfaces:**
- Produces: `ghorg.GetTeamByName(ctx, client, org, teamName string) (*github.Team, error)`, `ghorg.CheckAndAddMember(ctx, client, team *github.Team, ghUser *github.User) (bool, error)`, `ghorg.SummarizeTeam(ctx, client, team *github.Team) string`
- Note: `isTeam` stays unexported and unchanged (including its `log.Fatal`) — it has no callers outside `teams_test.go` today (verified: `grep -rn "isTeam(" --include="*.go" .` only shows the definition and two direct test calls). It is intentionally not wired into the CLI or GUI dispatch, so its `log.Fatal` can never fire from either front end. If a future task wires it up, fix this then.

- [ ] **Step 1: Move the files**

```bash
git mv teams.go internal/ghorg/teams.go
git mv teams_test.go internal/ghorg/teams_test.go
```

Change `package main` to `package ghorg` in both.

- [ ] **Step 2: Write the failing test for non-fatal `GetTeamByName`**

Add to `internal/ghorg/teams_test.go`:

```go
func TestGetTeamByName_NotFound(t *testing.T) {
	ctx := context.Background()
	mux := http.NewServeMux()
	mux.HandleFunc("/orgs/example-org/teams/no-such-team", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	})
	client, teardown := newTestClient(mux)
	defer teardown()

	_, err := GetTeamByName(ctx, client, "example-org", "No Such Team")
	if err == nil {
		t.Fatal("expected an error for a missing team, got nil")
	}
}
```

- [ ] **Step 3: Run it to confirm it fails**

Run: `go test ./internal/ghorg/... -run TestGetTeamByName_NotFound -v`
Expected: FAIL (compile error: `getTeamByName` doesn't return an error yet, or `GetTeamByName` undefined)

- [ ] **Step 4: Rewrite `getTeamByName` → `GetTeamByName`**

```go
// GetTeamByName gets a team by name (using the generated slug).
func GetTeamByName(ctx context.Context, client *github.Client, org, teamName string) (*github.Team, error) {
	team, _, err := client.Teams.GetTeamBySlug(ctx, org, slugify(teamName))
	if err != nil {
		return nil, fmt.Errorf("unable to find team %s: %w", teamName, err)
	}
	return team, nil
}
```

- [ ] **Step 5: Run the test again**

Run: `go test ./internal/ghorg/... -run TestGetTeamByName_NotFound -v`
Expected: PASS

- [ ] **Step 6: Write the failing test for non-fatal, non-prompting `CheckAndAddMember`**

```go
func TestCheckAndAddMember_APIError(t *testing.T) {
	ctx := context.Background()
	mux := http.NewServeMux()
	mux.HandleFunc("/organizations/1/team/2/memberships/someuser", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			http.Error(w, `{"message":"Internal Server Error"}`, http.StatusInternalServerError)
			return
		}
	})
	client, teardown := newTestClient(mux)
	defer teardown()

	team := newGithubTeam(1, 2, "Team Alpha", "team-alpha", "https://github.com/orgs/example-org/teams/team-alpha")
	ghUser := &github.User{Login: github.String("someuser")}

	_, err := CheckAndAddMember(ctx, client, team, ghUser)
	if err == nil {
		t.Fatal("expected an error when the membership check fails, got nil")
	}
}
```

- [ ] **Step 7: Run it to confirm it fails**

Run: `go test ./internal/ghorg/... -run TestCheckAndAddMember_APIError -v`
Expected: FAIL (compile error: `checkAndAddMember` returns nothing today)

- [ ] **Step 8: Rewrite `checkAndAddMember` → `CheckAndAddMember`**

```go
// CheckAndAddMember checks the prerequisites and, if satisfied, adds the user
// to the team. It returns whether a new membership was created; it never
// prints or copies to the clipboard — the caller decides how to report success.
func CheckAndAddMember(ctx context.Context, client *github.Client, team *github.Team, ghUser *github.User) (bool, error) {
	teamMembership, response, err := client.Teams.GetTeamMembershipByID(ctx,
		*team.Organization.ID, *team.ID, *ghUser.Login)
	if err != nil && (response == nil || response.StatusCode != 404) {
		return false, fmt.Errorf("unable to check team membership: %w", err)
	}
	if teamMembership != nil {
		return false, nil
	}

	opts := github.TeamAddTeamMembershipOptions{Role: "member"}
	_, _, err = client.Teams.AddTeamMembershipByID(ctx, *team.Organization.ID, *team.ID, *ghUser.Login, &opts)
	if err != nil {
		return false, fmt.Errorf("error adding user %s to team %s: %w", *ghUser.Login, *team.Name, err)
	}
	return true, nil
}
```

- [ ] **Step 9: Run the test again**

Run: `go test ./internal/ghorg/... -run TestCheckAndAddMember_APIError -v`
Expected: PASS

- [ ] **Step 10: Export `summarizeTeam`**

Rename `summarizeTeam` → `SummarizeTeam`. No logic changes (it has no `log.Fatal`, only `log.Printf` on pagination errors, which is fine to keep — it's non-fatal already).

- [ ] **Step 11: Update existing tests for the new signatures**

In `internal/ghorg/teams_test.go`, update every existing call site of `getTeamByName`/`checkAndAddMember` to the new names and to handle the returned `error` (e.g. `team, err := GetTeamByName(...)` and check `err` instead of relying on `log.Fatal` to end the test process on failure).

- [ ] **Step 12: Run the full package test suite**

Run: `go test ./internal/ghorg/... -v`
Expected: PASS

- [ ] **Step 13: Commit**

```bash
git add internal/ghorg/teams.go internal/ghorg/teams_test.go
git commit -m "refactor: move teams.go into internal/ghorg, remove log.Fatal/prompt from team operations"
```

---

## Task 6: Move `repos.go`, export pure renames, fix `CreateRepository`

**Files:**
- Create: `internal/ghorg/repos.go` (moved from `repos.go`)
- Create: `internal/ghorg/repos_test.go` (moved from `repos_test.go`)
- Delete: `repos.go`, `repos_test.go`

**Interfaces:**
- Produces (pure renames, no signature change beyond capitalization): `ghorg.IsRepository`, `ghorg.CheckRepository`, `ghorg.AddUserAsRepoCollaborator`, `ghorg.EnableDependabot`, `ghorg.HasDependabotGroupedPRs`, `ghorg.DefaultDependabotGroupedPRTemplate`, `ghorg.DependabotConfigPath`, `ghorg.StreamOrgAdminRepoAccessReport`, `ghorg.CreateRepository`

- [ ] **Step 1: Move the files**

```bash
git mv repos.go internal/ghorg/repos.go
git mv repos_test.go internal/ghorg/repos_test.go
```

Change `package main` to `package ghorg` in both.

- [ ] **Step 2: Write the failing test for non-fatal `CreateRepository`**

`createRepository` currently calls `log.Fatal("Creating repo failed:", err)` when the create API call errors. Add to `internal/ghorg/repos_test.go`:

```go
func TestCreateRepository_CreateAPIError(t *testing.T) {
	ctx := context.Background()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/example-org/new-repo", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	})
	mux.HandleFunc("/orgs/example-org/repos", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Unprocessable Entity"}`, http.StatusUnprocessableEntity)
	})
	client, teardown := newTestClient(mux)
	defer teardown()

	info := repositoryInfo{owner: "example-org", name: "new-repo", description: "test"}
	_, err := CreateRepository(ctx, client, info)
	if err == nil {
		t.Fatal("expected an error when repo creation fails, got nil")
	}
}
```

- [ ] **Step 3: Run it to confirm it fails**

Run: `go test ./internal/ghorg/... -run TestCreateRepository_CreateAPIError -v`
Expected: FAIL — either a compile error (`createRepository` vs `CreateRepository`) or the test process aborts because `log.Fatal` calls `os.Exit` (Go's test runner reports this as the test binary exiting unexpectedly)

- [ ] **Step 4: Fix `createRepository` → `CreateRepository`**

Rename the function and change its last block from:

```go
	repo, _, err := client.Repositories.Create(ctx, info.owner, repository)
	if err != nil {
		log.Fatal("Creating repo failed:", err)
	}
	return repo, nil
```

to:

```go
	repo, _, err := client.Repositories.Create(ctx, info.owner, repository)
	if err != nil {
		return nil, fmt.Errorf("creating repo failed: %w", err)
	}
	return repo, nil
```

- [ ] **Step 5: Run the test again**

Run: `go test ./internal/ghorg/... -run TestCreateRepository_CreateAPIError -v`
Expected: PASS

- [ ] **Step 6: Apply the remaining pure renames (no logic change)**

In `internal/ghorg/repos.go`, rename each of the following (definition + every internal call site within the package):

| Old | New |
|---|---|
| `isRepository` | `IsRepository` |
| `checkRepository` | `CheckRepository` |
| `addUserAsRepoCollaborator` | `AddUserAsRepoCollaborator` |
| `enableDependabot` | `EnableDependabot` |
| `hasDependabotGroupedPRs` | `HasDependabotGroupedPRs` |
| `defaultDependabotGroupedPRTemplate` | `DefaultDependabotGroupedPRTemplate` |
| `dependabotConfigPath` (const) | `DependabotConfigPath` |
| `streamOrgAdminRepoAccessReport` | `StreamOrgAdminRepoAccessReport` |

Also capitalize the two fields on the unexported `dependabotEnableResult` struct (returned by `EnableDependabot`) that `cmd/ghOrgTool` will need to read once it's in a different package: `vulnerabilityAlertsEnabled` → `VulnerabilityAlertsEnabled`, `automatedFixesEnabled` → `AutomatedFixesEnabled`. The struct type itself stays unexported — only the two fields need it, since callers outside the package only ever do `result.VulnerabilityAlertsEnabled`, never name the type directly.

None of these need logic changes — they already return errors rather than calling `log.Fatal`, and `StreamOrgAdminRepoAccessReport` already writes incrementally to its `io.Writer` parameter, which is what makes it reusable as-is for GUI streaming later.

- [ ] **Step 7: Update `internal/ghorg/repos_test.go` call sites**

Update every call site in the test file for the eight function renames and the two field renames above to use the capitalized names.

- [ ] **Step 8: Run the full package test suite**

Run: `go test ./internal/ghorg/... -v`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add internal/ghorg/repos.go internal/ghorg/repos_test.go
git commit -m "refactor: move repos.go into internal/ghorg, fix CreateRepository log.Fatal, export pure renames"
```

---

## Task 7: Convert `repos.go` report/list wrappers to return `(string, error)`

**Files:**
- Modify: `internal/ghorg/repos.go`
- Modify: `internal/ghorg/repos_test.go`

**Interfaces:**
- Produces: `ghorg.ListRepositoryCollaborators(ctx, client, owner, repo string) (string, error)`, `ghorg.ListTopActionsCacheUsageByRepo(ctx, client, org string, limit int) (string, error)`, `ghorg.ListRecentAdminGrants(ctx, client, org string) (string, error)`, `ghorg.ListRecentAdminRemovals(ctx, client, org string) (string, error)`, `ghorg.ReportUserRepoAccess(ctx, client, tc, org, userLogin, repoName string) (string, error)`, `ghorg.ReportUserAdminRepoAccess(ctx, client, tc, org, userLogin, resumeAfterRepo string) (string, error)`, `ghorg.FindAndReportTeamsWithAccessToAllRepos(ctx, client, owner string, repoNames []string) (string, error)`, `ghorg.ReportRepositoryTeams(ctx, client, owner, repositoryName string) (string, error)`
- Removes: `reportOrgAdminRepoAccess` (dead after this task — callers use `StreamOrgAdminRepoAccessReport` directly with their own `io.Writer`)

This is the biggest mechanical change in the plan: every function in this list currently builds its output with `fmt.Printf`/`fmt.Print` and returns only `error` (or, in one case, nothing at all). The GUI needs the formatted text back as a `string` instead of it going straight to stdout, so the CLI front end will now do the printing itself. Do these one at a time, each as its own commit, since they're independent of each other.

### 7a: `ListRecentAdminGrants` (smallest — the format function already returns a string)

- [ ] **Step 1: Write the failing test**

```go
func TestListRecentAdminGrants_ReturnsFormattedString(t *testing.T) {
	ctx := context.Background()
	mux := http.NewServeMux()
	mux.HandleFunc("/orgs/example-org/audit-log", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []interface{}{})
	})
	client, teardown := newTestClient(mux)
	defer teardown()

	out, err := ListRecentAdminGrants(ctx, client, "example-org")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "No recent admin access grants detected") {
		t.Errorf("expected no-results message in output, got: %s", out)
	}
}
```

- [ ] **Step 2: Confirm it fails**

Run: `go test ./internal/ghorg/... -run TestListRecentAdminGrants_ReturnsFormattedString -v`
Expected: FAIL (`listRecentAdminGrants` returns only `error` today)

- [ ] **Step 3: Change the signature**

```go
// ListRecentAdminGrants is the top-level command handler for --recent-admin-grants.
func ListRecentAdminGrants(ctx context.Context, client *github.Client, org string) (string, error) {
	now := time.Now()
	since := adminGrantLookbackSince(now)
	log.Printf("Scanning all repositories in %s for admin grants since %s...", org, since.UTC().Format("2006-01-02 15:04:05 UTC"))
	results, err := findRecentAdminGrants(ctx, client, org)
	if err != nil {
		return "", err
	}
	return reportRecentAdminGrants(org, results), nil
}
```

- [ ] **Step 4: Run the test again**

Run: `go test ./internal/ghorg/... -run TestListRecentAdminGrants_ReturnsFormattedString -v`
Expected: PASS

- [ ] **Step 5: Fix existing call sites in the test file**

Update every existing `err := listRecentAdminGrants(...)` call in `repos_test.go` to `_, err := ListRecentAdminGrants(...)`.

- [ ] **Step 6: Commit**

```bash
git add internal/ghorg/repos.go internal/ghorg/repos_test.go
git commit -m "refactor: ListRecentAdminGrants returns formatted string instead of printing"
```

### 7b: `ListRecentAdminRemovals` (identical pattern to 7a)

- [ ] **Step 1: Write the failing test**

```go
func TestListRecentAdminRemovals_ReturnsFormattedString(t *testing.T) {
	ctx := context.Background()
	mux := http.NewServeMux()
	mux.HandleFunc("/orgs/example-org/audit-log", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []interface{}{})
	})
	client, teardown := newTestClient(mux)
	defer teardown()

	out, err := ListRecentAdminRemovals(ctx, client, "example-org")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "No recent admin access removals detected") {
		t.Errorf("expected no-results message in output, got: %s", out)
	}
}
```

- [ ] **Step 2: Confirm it fails**

Run: `go test ./internal/ghorg/... -run TestListRecentAdminRemovals_ReturnsFormattedString -v`
Expected: FAIL

- [ ] **Step 3: Change the signature**

```go
// ListRecentAdminRemovals is the top-level command handler for --recent-admin-removals.
func ListRecentAdminRemovals(ctx context.Context, client *github.Client, org string) (string, error) {
	now := time.Now()
	since := adminGrantLookbackSince(now)
	log.Printf("Scanning all repositories in %s for admin removals since %s...", org, since.UTC().Format("2006-01-02 15:04:05 UTC"))
	results, err := findRecentAdminRemovals(ctx, client, org)
	if err != nil {
		return "", err
	}
	return reportRecentAdminRemovals(org, results), nil
}
```

- [ ] **Step 4: Run the test again**

Run: `go test ./internal/ghorg/... -run TestListRecentAdminRemovals_ReturnsFormattedString -v`
Expected: PASS

- [ ] **Step 5: Fix existing call sites, then commit**

```bash
git add internal/ghorg/repos.go internal/ghorg/repos_test.go
git commit -m "refactor: ListRecentAdminRemovals returns formatted string instead of printing"
```

### 7c: `ReportUserAdminRepoAccess` (also trivial — format function already returns a string)

- [ ] **Step 1: Write the failing test**

```go
func TestReportUserAdminRepoAccess_ReturnsFormattedString(t *testing.T) {
	ctx := context.Background()
	mux := http.NewServeMux()
	mux.HandleFunc("/orgs/example-org/repos", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []interface{}{})
	})
	client, teardown := newTestClient(mux)
	defer teardown()

	out, err := ReportUserAdminRepoAccess(ctx, client, nil, "example-org", "someuser", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "repo,user,access_type,team_name,access_url") {
		t.Errorf("expected CSV header in output, got: %s", out)
	}
}
```

- [ ] **Step 2: Confirm it fails**

Run: `go test ./internal/ghorg/... -run TestReportUserAdminRepoAccess_ReturnsFormattedString -v`
Expected: FAIL

- [ ] **Step 3: Change the signature**

```go
func ReportUserAdminRepoAccess(ctx context.Context, client *github.Client, tc *http.Client, org, userLogin, resumeAfterRepo string) (string, error) {
	report, err := findUserAdminRepoAccess(ctx, client, tc, org, userLogin, resumeAfterRepo)
	if err != nil {
		return "", err
	}
	return formatUserAdminRepoAccessReport(report), nil
}
```

- [ ] **Step 4: Run the test again, fix existing call sites, then commit**

```bash
go test ./internal/ghorg/... -run TestReportUserAdminRepoAccess_ReturnsFormattedString -v
git add internal/ghorg/repos.go internal/ghorg/repos_test.go
git commit -m "refactor: ReportUserAdminRepoAccess returns formatted string instead of printing"
```

### 7d: `ListTopActionsCacheUsageByRepo`

- [ ] **Step 1: Write the failing test**

```go
func TestListTopActionsCacheUsageByRepo_ReturnsFormattedString(t *testing.T) {
	ctx := context.Background()
	mux := http.NewServeMux()
	mux.HandleFunc("/orgs/example-org/actions/cache/usage-by-repository", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]interface{}{"repository_cache_usages": []interface{}{}})
	})
	mux.HandleFunc("/orgs/example-org/settings/billing/shared-storage", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]interface{}{"days_left_in_billing_cycle": 12, "estimated_paid_storage_for_month": 0, "estimated_storage_for_month": 0})
	})
	client, teardown := newTestClient(mux)
	defer teardown()

	out, err := ListTopActionsCacheUsageByRepo(ctx, client, "example-org", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "example-org") {
		t.Errorf("expected org name in output, got: %s", out)
	}
}
```

- [ ] **Step 2: Confirm it fails**

Run: `go test ./internal/ghorg/... -run TestListTopActionsCacheUsageByRepo_ReturnsFormattedString -v`
Expected: FAIL

- [ ] **Step 3: Change the signature**

```go
func ListTopActionsCacheUsageByRepo(ctx context.Context, client *github.Client, org string, limit int) (string, error) {
	usages, err := getActionsCacheUsageByRepoForOrg(ctx, client, org)
	if err != nil {
		return "", err
	}

	billing, err := getActionsStorageBillingForOrg(ctx, client, org)
	if err != nil {
		log.Printf("Warning: unable to retrieve org Actions storage billing for %s: %v", org, err)
	}

	return formatActionsCacheUsageReport(org, usages, billing, limit), nil
}
```

- [ ] **Step 4: Run the test again, fix existing call sites, then commit**

```bash
go test ./internal/ghorg/... -run TestListTopActionsCacheUsageByRepo_ReturnsFormattedString -v
git add internal/ghorg/repos.go internal/ghorg/repos_test.go
git commit -m "refactor: ListTopActionsCacheUsageByRepo returns formatted string instead of printing"
```

### 7e: `ListRepositoryCollaborators` (the whole body switches from `fmt.Printf` to a `strings.Builder`)

- [ ] **Step 1: Write the failing test**

```go
func TestListRepositoryCollaborators_ReturnsFormattedString(t *testing.T) {
	ctx := context.Background()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/example-org/my-repo/collaborators", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []interface{}{})
	})
	client, teardown := newTestClient(mux)
	defer teardown()

	out, err := ListRepositoryCollaborators(ctx, client, "example-org", "my-repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "No direct collaborators found") {
		t.Errorf("expected empty-state message in output, got: %s", out)
	}
}
```

- [ ] **Step 2: Confirm it fails**

Run: `go test ./internal/ghorg/... -run TestListRepositoryCollaborators_ReturnsFormattedString -v`
Expected: FAIL

- [ ] **Step 3: Rewrite the function**

Replace every `fmt.Printf(...)` call in the function body with `fmt.Fprintf(&b, ...)`, declare `var b strings.Builder` at the top, and change the two `return nil` statements to `return b.String(), nil`:

```go
func ListRepositoryCollaborators(ctx context.Context, client *github.Client, owner, repo string) (string, error) {
	log.Printf("Fetching collaborators for repository %s/%s", owner, repo)

	var b strings.Builder

	opts := &github.ListCollaboratorsOptions{
		ListOptions: github.ListOptions{PerPage: 100},
		Affiliation: "direct",
	}

	collaborators, _, err := client.Repositories.ListCollaborators(ctx, owner, repo, opts)
	if err != nil {
		return "", fmt.Errorf("failed to list collaborators: %w", err)
	}

	if len(collaborators) == 0 {
		fmt.Fprintf(&b, "📋 No direct collaborators found for repository %s/%s\n", owner, repo)
		fmt.Fprintf(&b, "   (Note: Team members are not included in this list)\n")
		return b.String(), nil
	}

	fmt.Fprintf(&b, "📋 Collaborators for repository %s/%s:\n\n", owner, repo)

	invitations, _, _ := client.Repositories.ListInvitations(ctx, owner, repo, nil)
	invitationMap := make(map[string]*github.RepositoryInvitation)
	for _, inv := range invitations {
		if inv.Invitee != nil {
			invitationMap[*inv.Invitee.Login] = inv
		}
	}

	events, _, _ := client.Activity.ListRepositoryEvents(ctx, owner, repo, &github.ListOptions{PerPage: 100})
	eventMap := make(map[string]time.Time)
	for _, event := range events {
		if event.GetType() == "MemberEvent" {
			payload, err := event.ParsePayload()
			if err != nil {
				continue
			}
			memberEvent, ok := payload.(*github.MemberEvent)
			if ok && memberEvent.Member != nil && memberEvent.GetAction() == "added" {
				login := *memberEvent.Member.Login
				if _, exists := eventMap[login]; !exists {
					eventMap[login] = event.CreatedAt.Time
				}
			}
		}
	}

	now := time.Now()

	for i, collab := range collaborators {
		fmt.Fprintf(&b, "%d. 👤 User: %s\n", i+1, *collab.Login)

		var permissions []string
		if collab.Permissions != nil {
			if collab.Permissions.GetAdmin() {
				permissions = append(permissions, "admin")
			}
			if collab.Permissions.GetMaintain() {
				permissions = append(permissions, "maintain")
			}
			if collab.Permissions.GetPush() {
				permissions = append(permissions, "push")
			}
			if collab.Permissions.GetTriage() {
				permissions = append(permissions, "triage")
			}
			if collab.Permissions.GetPull() {
				permissions = append(permissions, "pull")
			}
		}

		if len(permissions) > 0 {
			fmt.Fprintf(&b, "   🔐 Permissions: %s\n", strings.Join(permissions, ", "))
		}

		permission, _, permErr := client.Repositories.GetPermissionLevel(ctx, owner, repo, *collab.Login)
		if permErr == nil && permission.Permission != nil {
			fmt.Fprintf(&b, "   📊 Access Level: %s\n", *permission.Permission)
		}

		var addedTime time.Time
		var addedSource string

		if inv, exists := invitationMap[*collab.Login]; exists {
			addedTime = inv.CreatedAt.Time
			addedSource = "invitation"
		}

		if addedTime.IsZero() {
			if eventTime, exists := eventMap[*collab.Login]; exists {
				addedTime = eventTime
				addedSource = "event"
			}
		}

		if !addedTime.IsZero() {
			duration := now.Sub(addedTime)
			fmt.Fprintf(&b, "   📅 Added: %s (%.1f hours ago)\n", addedTime.Format("2006-01-02 15:04:05"), duration.Hours())
			if addedSource == "invitation" {
				fmt.Fprintf(&b, "   ℹ️  Status: Invitation pending\n")
			}

			if collab.Permissions.GetAdmin() && duration.Hours() > 24 {
				fmt.Fprintf(&b, "   ⚠️  WARNING: Admin access granted >24 hours ago - consider reviewing\n")
			}
		} else {
			fmt.Fprintf(&b, "   📅 Added: Unknown (not found in recent events)\n")
		}

		if collab.HTMLURL != nil {
			fmt.Fprintf(&b, "   🔗 Profile: %s\n", *collab.HTMLURL)
		}

		fmt.Fprintf(&b, "\n")
	}

	fmt.Fprintf(&b, "📊 Total: %d direct collaborator(s)\n", len(collaborators))

	return b.String(), nil
}
```

- [ ] **Step 4: Run the test again**

Run: `go test ./internal/ghorg/... -run TestListRepositoryCollaborators_ReturnsFormattedString -v`
Expected: PASS

- [ ] **Step 5: Fix existing call sites in `repos_test.go`**

Every existing `TestListRepositoryCollaborators_*` test (there are six: `_Empty`, `_WithCollaborator`, `_APIError`, `_WithInvitation`, `_AdminOldInvitation`, `_WithMemberEvent`) currently does `err := listRepositoryCollaborators(...)`. Update each to `_, err := ListRepositoryCollaborators(...)`.

- [ ] **Step 6: Run the full package test suite**

Run: `go test ./internal/ghorg/... -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/ghorg/repos.go internal/ghorg/repos_test.go
git commit -m "refactor: ListRepositoryCollaborators returns formatted string instead of printing"
```

### 7f: `FindAndReportTeamsWithAccessToAllRepos` (currently has no return value at all)

- [ ] **Step 1: Write the failing test**

```go
func TestFindAndReportTeamsWithAccessToAllRepos_ReturnsFormattedString(t *testing.T) {
	ctx := context.Background()
	mux := http.NewServeMux()
	for _, repo := range []string{"r1", "r2"} {
		repo := repo
		mux.HandleFunc("/repos/example-org/"+repo, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, map[string]interface{}{"name": repo, "owner": map[string]string{"login": "example-org"}})
		})
		mux.HandleFunc("/repos/example-org/"+repo+"/teams", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, []map[string]interface{}{
				{"name": "All Access Team", "slug": "all-access-team", "html_url": "https://github.com", "permission": "push"},
			})
		})
	}
	client, teardown := newTestClient(mux)
	defer teardown()

	out, err := FindAndReportTeamsWithAccessToAllRepos(ctx, client, "example-org", []string{"r1", "r2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "EXACT MATCHES") || !strings.Contains(out, "All Access Team") {
		t.Errorf("expected exact-match report with team name, got: %s", out)
	}
}
```

- [ ] **Step 2: Confirm it fails**

Run: `go test ./internal/ghorg/... -run TestFindAndReportTeamsWithAccessToAllRepos_ReturnsFormattedString -v`
Expected: FAIL

- [ ] **Step 3: Rewrite the function**

Replace every `fmt.Printf(...)` with `fmt.Fprintf(&b, ...)`, add `var b strings.Builder` at the top, change the signature to return `(string, error)`, and return `b.String(), nil` at the end (and `"", err` on the early error return):

```go
func FindAndReportTeamsWithAccessToAllRepos(ctx context.Context, client *github.Client, owner string, repoNames []string) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "Analyzing team access patterns for %d repositories...\n", len(repoNames))
	fmt.Fprintf(&b, "Repositories: %v\n\n", repoNames)

	result, err := findTeamsWithAccessAnalysis(ctx, client, owner, repoNames)
	if err != nil {
		return "", fmt.Errorf("error finding teams: %w", err)
	}

	if len(result.exactMatches) > 0 {
		fmt.Fprintf(&b, "🎯 EXACT MATCHES - Teams with access to ALL %d repositories:\n\n", len(repoNames))
		for i, team := range result.exactMatches {
			fmt.Fprintf(&b, "%d. Team: %s\n", i+1, team.name)
			fmt.Fprintf(&b, "   Slug: %s\n", team.slug)
			if team.description != "" {
				fmt.Fprintf(&b, "   Description: %s\n", team.description)
			}
			fmt.Fprintf(&b, "   Access Level: %s\n", team.access)
			fmt.Fprintf(&b, "   URL: %s\n", team.url)
			fmt.Fprintf(&b, "   Coverage: 100%% (%d/%d repositories)\n", len(repoNames), len(repoNames))
			fmt.Fprintf(&b, "\n")
		}
	} else {
		fmt.Fprintf(&b, "🎯 EXACT MATCHES: No teams found with access to ALL repositories.\n\n")
	}

	if len(result.closeMatches) > 0 {
		fmt.Fprintf(&b, "🔍 CLOSE MATCHES - Teams with access to more than half of the repositories:\n\n")
		for i, match := range result.closeMatches {
			fmt.Fprintf(&b, "%d. Team: %s\n", i+1, match.team.name)
			fmt.Fprintf(&b, "   Slug: %s\n", match.team.slug)
			if match.team.description != "" {
				fmt.Fprintf(&b, "   Description: %s\n", match.team.description)
			}
			fmt.Fprintf(&b, "   Access Level: %s\n", match.team.access)
			fmt.Fprintf(&b, "   URL: %s\n", match.team.url)
			fmt.Fprintf(&b, "   Coverage: %.1f%% (%d/%d repositories)\n", match.accessPercent, match.accessCount, len(repoNames))
			if len(match.missingRepos) > 0 {
				fmt.Fprintf(&b, "   Missing access to: %v\n", match.missingRepos)
			}
			fmt.Fprintf(&b, "\n")
		}
	} else {
		fmt.Fprintf(&b, "🔍 CLOSE MATCHES: No teams found with access to more than half of the repositories.\n\n")
	}

	totalMatches := len(result.exactMatches) + len(result.closeMatches)
	if totalMatches == 0 {
		fmt.Fprintf(&b, "📊 SUMMARY: No teams found with significant access coverage.\n")
		fmt.Fprintf(&b, "To find teams with access to individual repositories, use the --teams flag with each repository name.\n")
	} else {
		fmt.Fprintf(&b, "📊 SUMMARY: Found %d exact matches and %d close matches.\n", len(result.exactMatches), len(result.closeMatches))
	}

	return b.String(), nil
}
```

- [ ] **Step 4: Run the test again**

Run: `go test ./internal/ghorg/... -run TestFindAndReportTeamsWithAccessToAllRepos_ReturnsFormattedString -v`
Expected: PASS

- [ ] **Step 5: Fix existing call sites**

The four existing `TestFindAndReportTeamsWithAccessToAllRepos_*` tests (`_ExactMatch`, `_NoMatch`, `_WithCloseMatch`, `_AnalysisError`) call the function without capturing a return value. Update them to `_, err := FindAndReportTeamsWithAccessToAllRepos(...)`, and for `_AnalysisError` assert `err != nil` instead of whatever it currently checks (re-read that test first — it likely just calls the function and relies on it not panicking, since there was no error return before).

- [ ] **Step 6: Run the full package test suite, then commit**

```bash
go test ./internal/ghorg/... -v
git add internal/ghorg/repos.go internal/ghorg/repos_test.go
git commit -m "refactor: FindAndReportTeamsWithAccessToAllRepos returns formatted string instead of printing"
```

### 7g: `ReportUserRepoAccess`

- [ ] **Step 1: Write the failing test**

```go
func TestReportUserRepoAccess_ReturnsFormattedString(t *testing.T) {
	ctx := context.Background()
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]interface{}{
			"data": map[string]interface{}{
				"organization": map[string]interface{}{
					"id": "org1", "login": "example-org",
					"teams": map[string]interface{}{
						"totalCount": 1,
						"nodes": []map[string]interface{}{
							{"id": "t1", "name": "Team Alpha", "slug": "team-alpha", "description": "", "url": "https://github.com/orgs/example-org/teams/team-alpha"},
						},
					},
				},
			},
		})
	})
	mux.HandleFunc("/repos/example-org/my-repo/teams", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []map[string]interface{}{
			{"name": "Team Alpha", "slug": "team-alpha", "html_url": "https://github.com/orgs/example-org/teams/team-alpha", "permission": "push"},
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	client, teardown := newTestClient(mux)
	defer teardown()
	tc := &http.Client{Transport: rewriteTransport{target: server.URL}}

	out, err := ReportUserRepoAccess(ctx, client, tc, "example-org", "someuser", "my-repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Effective permission: write") {
		t.Errorf("expected effective permission in output, got: %s", out)
	}
}
```

- [ ] **Step 2: Confirm it fails**

Run: `go test ./internal/ghorg/... -run TestReportUserRepoAccess_ReturnsFormattedString -v`
Expected: FAIL

- [ ] **Step 3: Rewrite the function**

```go
func ReportUserRepoAccess(ctx context.Context, client *github.Client, tc *http.Client, org, userLogin, repoName string) (string, error) {
	userTeams, err := getUserTeams(ctx, tc, org, userLogin)
	if err != nil {
		return "", fmt.Errorf("unable to get teams for user %s: %w", userLogin, err)
	}

	repoTeams, err := getRepositoryTeams(ctx, client, org, repoName)
	if err != nil {
		return "", fmt.Errorf("unable to get teams for repository %s: %w", repoName, err)
	}

	repoTeamPermissions := make(map[string]string)
	for _, t := range repoTeams {
		repoTeamPermissions[t.slug] = normalizePermission(t.access)
	}

	var matches []userRepoTeamAccess
	for _, ut := range userTeams {
		if perm, ok := repoTeamPermissions[ut.slug]; ok {
			matches = append(matches, userRepoTeamAccess{team: ut, permission: perm})
		}
	}

	var b strings.Builder

	if len(matches) == 0 {
		fmt.Fprintf(&b, "User %s has no team-based access to repository %s/%s\n", userLogin, org, repoName)
		return b.String(), nil
	}

	effectivePerm := ""
	for _, m := range matches {
		if permissionLevel(m.permission) > permissionLevel(effectivePerm) {
			effectivePerm = m.permission
		}
	}

	fmt.Fprintf(&b, "Access report: %s → %s/%s\n\n", userLogin, org, repoName)
	fmt.Fprintf(&b, "Effective permission: %s\n\n", effectivePerm)
	fmt.Fprintf(&b, "Via teams:\n")
	for _, m := range matches {
		fmt.Fprintf(&b, "  - %s (%s): %s\n", m.team.name, m.team.url, m.permission)
	}

	return b.String(), nil
}
```

- [ ] **Step 4: Run the test again**

Run: `go test ./internal/ghorg/... -run TestReportUserRepoAccess_ReturnsFormattedString -v`
Expected: PASS

- [ ] **Step 5: Fix existing call sites, run the full suite, then commit**

```bash
go test ./internal/ghorg/... -v
git add internal/ghorg/repos.go internal/ghorg/repos_test.go
git commit -m "refactor: ReportUserRepoAccess returns formatted string instead of printing"
```

### 7h: Add `ReportRepositoryTeams`, drop `reportOrgAdminRepoAccess`

- [ ] **Step 1: Write the failing test**

```go
func TestReportRepositoryTeams_ReturnsFormattedString(t *testing.T) {
	ctx := context.Background()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/example-org/my-repo/teams", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []map[string]interface{}{
			{"name": "Team Alpha", "slug": "team-alpha", "html_url": "https://github.com/orgs/example-org/teams/team-alpha", "permission": "push"},
		})
	})
	client, teardown := newTestClient(mux)
	defer teardown()

	out, err := ReportRepositoryTeams(ctx, client, "example-org", "my-repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "my-repo has the following teams with access") || !strings.Contains(out, "Team Alpha") {
		t.Errorf("expected repo-teams report, got: %s", out)
	}
}
```

- [ ] **Step 2: Confirm it fails**

Run: `go test ./internal/ghorg/... -run TestReportRepositoryTeams_ReturnsFormattedString -v`
Expected: FAIL with "undefined: ReportRepositoryTeams"

- [ ] **Step 3: Add `formatRepositoryTeamsReport` and `ReportRepositoryTeams`**

```go
// formatRepositoryTeamsReport renders the teams with access to a repository as CLI-style text.
func formatRepositoryTeamsReport(repoName string, teams []teamInfo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Repository %s has the following teams with access:\n", repoName)
	for _, team := range teams {
		fmt.Fprintf(&b, "* %s (%s) %s\n", team.name, team.url, team.access)
	}
	return b.String()
}

// ReportRepositoryTeams fetches and formats the teams that have access to a repository.
func ReportRepositoryTeams(ctx context.Context, client *github.Client, owner, repositoryName string) (string, error) {
	teams, err := getRepositoryTeams(ctx, client, owner, repositoryName)
	if err != nil {
		return "", err
	}
	return formatRepositoryTeamsReport(repositoryName, teams), nil
}
```

- [ ] **Step 4: Run the test again**

Run: `go test ./internal/ghorg/... -run TestReportRepositoryTeams_ReturnsFormattedString -v`
Expected: PASS

- [ ] **Step 5: Delete `reportOrgAdminRepoAccess`**

It's now dead code (`func reportOrgAdminRepoAccess(ctx context.Context, client *github.Client, org, resumeAfterRepo, checkpointFile string) error { return streamOrgAdminRepoAccessReport(ctx, client, os.Stdout, org, resumeAfterRepo, checkpointFile) }`) — delete it entirely. `cmd/ghOrgTool` will call `ghorg.StreamOrgAdminRepoAccessReport(ctx, client, os.Stdout, ...)` directly in Task 8.

- [ ] **Step 6: Run the full package test suite**

Run: `go test ./internal/ghorg/... -v`
Expected: PASS (no existing test referenced `reportOrgAdminRepoAccess` directly — confirm with `grep -rn "reportOrgAdminRepoAccess" internal/ghorg/repos_test.go` before deleting, and if a test does reference it, delete that test too since the behavior moves to the CLI layer, which is covered by `cmd/ghOrgTool`'s own tests in Task 8)

- [ ] **Step 7: Commit**

```bash
git add internal/ghorg/repos.go internal/ghorg/repos_test.go
git commit -m "refactor: add ReportRepositoryTeams, drop dead reportOrgAdminRepoAccess wrapper"
```

---

## Task 8: Add `Connect()` to `internal/ghorg`

**Files:**
- Create: `internal/ghorg/client.go`
- Create: `internal/ghorg/client_test.go`

**Interfaces:**
- Produces: `ghorg.Connect() (context.Context, *http.Client, *github.Client, error)`

- [ ] **Step 1: Write the failing test**

```go
package ghorg

import "testing"

func TestConnect_UsesEnvToken(t *testing.T) {
	t.Setenv(TokenEnvVar, "test-token-123")

	_, _, client, err := Connect()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected a non-nil GitHub client")
	}
}
```

- [ ] **Step 2: Confirm it fails**

Run: `go test ./internal/ghorg/... -run TestConnect_UsesEnvToken -v`
Expected: FAIL with "undefined: Connect"

- [ ] **Step 3: Implement `Connect`**

```go
package ghorg

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/user"
	"path/filepath"

	"github.com/google/go-github/v84/github"
	"github.com/jdxcode/netrc"
	"golang.org/x/oauth2"
)

// Connect resolves a GitHub token — checking the GITHUB_AUTH_TOKEN env var,
// then the config file, then .netrc, in that order — and returns a ready
// context, GraphQL-capable HTTP client, and REST client.
func Connect() (context.Context, *http.Client, *github.Client, error) {
	usr, err := user.Current()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("unable to get current user: %w", err)
	}

	token := os.Getenv(TokenEnvVar)

	if token == "" {
		token = GetGithubToken()
	}

	if token == "" {
		n, err := netrc.Parse(filepath.Join(usr.HomeDir, ".netrc"))
		if err != nil {
			return nil, nil, nil, fmt.Errorf("unable to load .netrc: %w", err)
		}
		token = n.Machine("api.github.com").Get("password")
	}

	if token == "" {
		return nil, nil, nil, fmt.Errorf("unable to find a GitHub token (checked %s, config file, and .netrc)", TokenEnvVar)
	}

	ctx := context.Background()
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)
	return ctx, tc, client, nil
}
```

- [ ] **Step 4: Run the test again**

Run: `go test ./internal/ghorg/... -run TestConnect_UsesEnvToken -v`
Expected: PASS

- [ ] **Step 5: Run the entire `internal/ghorg` suite with race detection, matching CI**

Run: `go vet ./internal/ghorg/... && go test -race -count=1 ./internal/ghorg/...`
Expected: all PASS, no vet issues

- [ ] **Step 6: Commit**

```bash
git add internal/ghorg/client.go internal/ghorg/client_test.go
git commit -m "feat: add ghorg.Connect, replacing main.go's fatal-on-missing-token connect()"
```

---

## Task 9: Create `cmd/ghOrgTool`, wire the CLI to `internal/ghorg`

**Files:**
- Create: `cmd/ghOrgTool/main.go` (moved + rewired from root `main.go`)
- Create: `cmd/ghOrgTool/main_test.go` (moved + rewired from root `main_test.go`)
- Delete: `main.go`, `main_test.go`

**Interfaces:**
- Consumes: every exported symbol from Tasks 1–8 (`ghorg.Connect`, `ghorg.ORG`, `ghorg.GetOrgLogin`, `ghorg.GetDefaultTeam`, `ghorg.InitConfig`, `ghorg.RotateToken`, `ghorg.Prompt`, `ghorg.IsRepository`, `ghorg.IsUser`, `ghorg.ResolveLogin`, `ghorg.UserIsValid`, `ghorg.GetTeamByName`, `ghorg.CheckAndAddMember`, `ghorg.SummarizeTeam`, `ghorg.ReportRepositoryTeams`, `ghorg.ReportUserTeams`, `ghorg.CheckRepository`, `ghorg.ListRepositoryCollaborators`, `ghorg.ListTopActionsCacheUsageByRepo`, `ghorg.ListRecentAdminGrants`, `ghorg.ListRecentAdminRemovals`, `ghorg.AddUserAsRepoCollaborator`, `ghorg.EnableDependabot`, `ghorg.HasDependabotGroupedPRs`, `ghorg.DefaultDependabotGroupedPRTemplate`, `ghorg.DependabotConfigPath`, `ghorg.ReportUserRepoAccess`, `ghorg.StreamOrgAdminRepoAccessReport`, `ghorg.ReportUserAdminRepoAccess`, `ghorg.FindAndReportTeamsWithAccessToAllRepos`)

This task has no new behavior — it's a mechanical rewire of the existing dispatch logic to call through `ghorg.*` instead of same-package functions, and to handle the errors those functions now return instead of relying on them to `log.Fatal`. `detectEntityType`, `decomposeGithubURL`, `normalizeEntityArgument`, `githubURLComponents`, and `entityType` are CLI-only (the GUI uses explicit form fields instead of free-text entity detection) and stay in this file unchanged except for the `ghorg.`-prefixed calls they make.

- [ ] **Step 1: Move the files**

```bash
mkdir -p cmd/ghOrgTool
git mv main.go cmd/ghOrgTool/main.go
git mv main_test.go cmd/ghOrgTool/main_test.go
```

- [ ] **Step 2: Replace the whole of `cmd/ghOrgTool/main.go`**

Replace the file's contents with the following. This preserves every existing dispatch branch's user-visible behavior (same log lines, same fatal-vs-continue semantics), it just routes through `ghorg.*` and handles the newly-returned errors:

```go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/glow-mdsol/ghOrgTool/internal/ghorg"
	"github.com/google/go-github/v84/github"
	"rsc.io/getopt"
)

// entityType represents the type of GitHub entity
type entityType int

const (
	entityUnknown entityType = iota
	entityUser
	entityRepository
)

// detectEntityType determines whether an entity slug is a repository or user.
// Returns the entity type and resolved login (for users) or repo name (for repos).
func detectEntityType(ctx context.Context, client *github.Client, tc *http.Client, entitySlug string) (entityType, string) {
	if strings.Contains(entitySlug, "@") {
		login, err := ghorg.ResolveLogin(ctx, tc, &entitySlug)
		if err != nil || login == "" {
			return entityUnknown, ""
		}
		return entityUser, login
	}

	if ghorg.IsRepository(ctx, client, ghorg.ORG, entitySlug) {
		return entityRepository, entitySlug
	}

	if ghorg.IsUser(ctx, client, &entitySlug) {
		_, resp, err := client.Organizations.GetOrgMembership(ctx, entitySlug, ghorg.ORG)
		if err == nil && resp.StatusCode == 200 {
			return entityUser, entitySlug
		}
		log.Printf("User %s exists but is not a member of organization %s", entitySlug, ghorg.ORG)
	}

	return entityUnknown, ""
}

type githubURLComponents struct {
	orgName  *string
	repoName *string
	teamName *string
	userName *string
}

// decomposeGithubURL splits a GitHub URL into its components.
func decomposeGithubURL(rawURL string) (githubURLComponents, error) {
	cleaned := strings.TrimSpace(rawURL)
	if cleaned == "" {
		return githubURLComponents{}, fmt.Errorf("invalid GitHub URL: %s", rawURL)
	}

	if strings.HasPrefix(cleaned, "<") && strings.HasSuffix(cleaned, ">") {
		cleaned = strings.TrimPrefix(strings.TrimSuffix(cleaned, ">"), "<")
	}
	if pipeIndex := strings.Index(cleaned, "|"); pipeIndex != -1 {
		cleaned = cleaned[:pipeIndex]
	}

	if !strings.Contains(cleaned, "://") {
		cleaned = "https://" + cleaned
	}

	parsed, err := url.Parse(cleaned)
	if err != nil {
		return githubURLComponents{}, fmt.Errorf("invalid GitHub URL: %s", rawURL)
	}

	if !strings.EqualFold(parsed.Hostname(), "github.com") {
		return githubURLComponents{}, fmt.Errorf("unsupported host for GitHub URL: %s", rawURL)
	}

	path := strings.Trim(parsed.Path, "/")
	if path == "" {
		return githubURLComponents{}, fmt.Errorf("invalid GitHub URL: %s", rawURL)
	}

	segments := strings.Split(path, "/")
	for i, segment := range segments {
		decoded, decodeErr := url.PathUnescape(segment)
		if decodeErr == nil {
			segments[i] = decoded
		}
	}

	switch {
	case len(segments) >= 4 && segments[0] == "orgs" && segments[2] == "teams":
		org := segments[1]
		team := segments[3]
		return githubURLComponents{orgName: &org, teamName: &team}, nil
	case len(segments) >= 2 && segments[0] == "orgs":
		org := segments[1]
		return githubURLComponents{orgName: &org}, nil
	case len(segments) >= 2:
		org := segments[0]
		repo := segments[1]
		return githubURLComponents{orgName: &org, repoName: &repo}, nil
	case len(segments) == 1:
		user := segments[0]
		return githubURLComponents{userName: &user}, nil
	default:
		return githubURLComponents{}, fmt.Errorf("invalid GitHub URL: %s", rawURL)
	}
}

func normalizeEntityArgument(arg string) string {
	trimmed := strings.TrimSpace(arg)
	if trimmed == "" {
		return trimmed
	}

	components, err := decomposeGithubURL(trimmed)
	if err != nil {
		return trimmed
	}

	if components.repoName != nil && *components.repoName != "" {
		return *components.repoName
	}
	if components.userName != nil && *components.userName != "" {
		return *components.userName
	}
	if components.teamName != nil && *components.teamName != "" {
		return *components.teamName
	}
	if components.orgName != nil && *components.orgName != "" {
		return *components.orgName
	}

	return trimmed
}

func main() {
	ghorg.ORG = ghorg.GetOrgLogin()
	defaultTeam := ghorg.GetDefaultTeam()
	var teamName = flag.String("team", defaultTeam, "Specified Team")
	var repoName = flag.String("repo", "", "Repository name for repo operations")
	var resetFlag = flag.Bool("reset", false, "Generate the Reset link")
	var findCommonTeams = flag.Bool("find-common-teams", false, "Find teams that have access to ALL specified repositories")
	var addToTM = flag.Bool("add", false, "Add User to Team")
	var addRepoAdmin = flag.Bool("add-repo-admin", false, "Add user as admin collaborator to repository")
	var enableDependabotFlag = flag.Bool("enable-dependabot", false, "Enable Dependabot alerts and security updates on one or more repositories")
	var dependabotGroupedPRsFlag = flag.Bool("dependabot-grouped-prs", false, "Optional with --enable-dependabot: check grouped PR config and print PR-ready guidance (no direct commits)")
	var listRepoCollaborators = flag.Bool("list-repo-collaborators", false, "List collaborators on repository with permissions and added dates")
	var listActionsStorage = flag.Bool("list-actions-storage", false, "List top 10 repositories by Actions cache usage and org billable constrained storage")
	var recentAdminGrants = flag.Bool("recent-admin-grants", false, "List users recently granted admin access to any org repo (Monday runs include grants since previous Friday) that still have that access")
	var recentAdminRemovals = flag.Bool("recent-admin-removals", false, "List users recently removed from admin access on org repos (same lookback window as recent-admin-grants)")
	var describeTeam = flag.Bool("describe-team", false, "Show detailed summary of a team")
	var userRepoAccess = flag.Bool("user-repo-access", false, "Report a user's effective access to a repository via team membership (requires --repo)")
	var adminUserReport = flag.Bool("admin-user-report", false, "Report repositories where users have effective admin access via teams or direct collaborator links (optionally filtered to one user)")
	var resumeAfterRepo = flag.String("resume-after-repo", "", "Resume all-repos access reporting after the named repository")
	var checkpointFile = flag.String("checkpoint-file", "", "Checkpoint file for deterministic admin-user-report restarts")
	var initFlag = flag.Bool("init", false, "Initialize configuration file")
	var rotateTokenFlag = flag.Bool("rotate-token", false, "Rotate/update GitHub token in configuration")
	var help = flag.Bool("help", false, "Print help")
	getopt.Alias("s", "team")
	getopt.Alias("R", "repo")
	getopt.Alias("a", "add")
	getopt.Alias("A", "add-repo-admin")
	getopt.Alias("B", "enable-dependabot")
	getopt.Alias("P", "dependabot-grouped-prs")
	getopt.Alias("L", "list-repo-collaborators")
	getopt.Alias("S", "list-actions-storage")
	getopt.Alias("G", "recent-admin-grants")
	getopt.Alias("M", "recent-admin-removals")
	getopt.Alias("c", "find-common-teams")
	getopt.Alias("r", "reset")
	getopt.Alias("d", "describe-team")
	getopt.Alias("u", "user-repo-access")
	getopt.Alias("U", "admin-user-report")
	getopt.Alias("i", "init")
	getopt.Alias("t", "rotate-token")
	getopt.Alias("h", "help")
	getopt.Parse()

	if *dependabotGroupedPRsFlag && !*enableDependabotFlag {
		log.Fatal("--dependabot-grouped-prs must be used with --enable-dependabot")
	}

	if *initFlag {
		if err := ghorg.InitConfig(); err != nil {
			log.Fatalf("Configuration initialization failed: %v", err)
		}
		os.Exit(0)
	}

	if *rotateTokenFlag {
		if err := ghorg.RotateToken(); err != nil {
			log.Fatalf("Token rotation failed: %v", err)
		}
		os.Exit(0)
	}

	if *help {
		printHelp(defaultTeam)
		os.Exit(0)
	}

	var userOrRepoList = flag.Args()
	for i, arg := range userOrRepoList {
		userOrRepoList[i] = normalizeEntityArgument(arg)
	}
	*repoName = normalizeEntityArgument(*repoName)

	ctx, tc, client, err := ghorg.Connect()
	if err != nil {
		log.Fatal(err)
	}

	if *describeTeam {
		team, err := ghorg.GetTeamByName(ctx, client, ghorg.ORG, *teamName)
		if err != nil {
			log.Fatalf("Unable to find team '%s': %v", *teamName, err)
		}
		log.Printf("Got team '%s' for '%s'", *team.Name, *teamName)
		fmt.Println(ghorg.SummarizeTeam(ctx, client, team))
		return
	}

	if *listRepoCollaborators {
		if *repoName == "" {
			log.Fatal("--repo flag is required when using --list-repo-collaborators")
		}
		if !ghorg.IsRepository(ctx, client, ghorg.ORG, *repoName) {
			log.Fatalf("Repository '%s' not found in organization '%s'", *repoName, ghorg.ORG)
		}
		out, err := ghorg.ListRepositoryCollaborators(ctx, client, ghorg.ORG, *repoName)
		if err != nil {
			log.Printf("Error listing collaborators for repository %s: %s", *repoName, err)
			return
		}
		fmt.Print(out)
		return
	}

	if *listActionsStorage {
		out, err := ghorg.ListTopActionsCacheUsageByRepo(ctx, client, ghorg.ORG, 10)
		if err != nil {
			log.Printf("Error listing GitHub Actions storage report: %s", err)
			return
		}
		fmt.Print(out)
		return
	}

	if *recentAdminGrants {
		out, err := ghorg.ListRecentAdminGrants(ctx, client, ghorg.ORG)
		if err != nil {
			log.Printf("Error listing recent admin grants: %s", err)
			return
		}
		fmt.Print(out)
		return
	}

	if *recentAdminRemovals {
		out, err := ghorg.ListRecentAdminRemovals(ctx, client, ghorg.ORG)
		if err != nil {
			log.Printf("Error listing recent admin removals: %s", err)
			return
		}
		fmt.Print(out)
		return
	}

	if *addRepoAdmin {
		if *repoName == "" {
			log.Fatal("--repo flag is required when using --add-repo-admin")
		}
		if !ghorg.IsRepository(ctx, client, ghorg.ORG, *repoName) {
			log.Fatalf("Repository '%s' not found in organization '%s'", *repoName, ghorg.ORG)
		}
		if len(userOrRepoList) == 0 {
			log.Fatal("At least one username or email is required")
		}

		for _, entitySlug := range userOrRepoList {
			if entitySlug == "" {
				continue
			}
			login, err := ghorg.ResolveLogin(ctx, tc, &entitySlug)
			if err != nil {
				log.Printf("Unable to resolve %s: %s", entitySlug, err)
				continue
			}
			if login == "" {
				continue
			}
			if !ghorg.IsUser(ctx, client, &login) {
				log.Printf("User %s not found", login)
				continue
			}
			if err := ghorg.AddUserAsRepoCollaborator(ctx, client, ghorg.ORG, *repoName, login); err != nil {
				log.Printf("Error adding user %s as admin to repository %s: %s", login, *repoName, err)
			}
		}
		return
	}

	if *enableDependabotFlag {
		var repoTargets []string
		if *repoName != "" {
			repoTargets = append(repoTargets, *repoName)
		}
		repoTargets = append(repoTargets, userOrRepoList...)

		if len(repoTargets) == 0 {
			log.Fatal("At least one repository is required when using --enable-dependabot")
		}

		seen := make(map[string]struct{})
		validTargets := 0

		for _, repo := range repoTargets {
			repo = strings.TrimSpace(repo)
			if repo == "" {
				continue
			}
			if _, exists := seen[repo]; exists {
				continue
			}
			seen[repo] = struct{}{}

			if !ghorg.IsRepository(ctx, client, ghorg.ORG, repo) {
				log.Printf("Warning: repository '%s' not found in organization '%s', skipping", repo, ghorg.ORG)
				continue
			}

			result, err := ghorg.EnableDependabot(ctx, client, ghorg.ORG, repo)
			if err != nil {
				log.Printf("Error enabling Dependabot for repository %s: %s", repo, err)
				continue
			}

			validTargets++
			switch {
			case result.VulnerabilityAlertsEnabled && result.AutomatedFixesEnabled:
				log.Printf("Enabled Dependabot alerts and security updates for repository %s", repo)
			case result.VulnerabilityAlertsEnabled:
				log.Printf("Enabled Dependabot alerts for repository %s (security updates were already enabled)", repo)
			case result.AutomatedFixesEnabled:
				log.Printf("Enabled Dependabot security updates for repository %s (alerts were already enabled)", repo)
			default:
				log.Printf("Dependabot is already fully enabled for repository %s", repo)
			}

			if *dependabotGroupedPRsFlag {
				groupedConfigured, groupedErr := ghorg.HasDependabotGroupedPRs(ctx, client, ghorg.ORG, repo)
				if groupedErr != nil {
					log.Printf("Unable to check grouped Dependabot PR config for repository %s: %s", repo, groupedErr)
					continue
				}
				if groupedConfigured {
					log.Printf("Grouped Dependabot PRs are already configured for repository %s", repo)
				} else {
					log.Printf("Grouped Dependabot PRs requested for repository %s, but %s is missing or ungrouped", repo, ghorg.DependabotConfigPath)
					log.Printf("Direct commits are disabled. Open a pull request in %s/%s with this content in %s:", ghorg.ORG, repo, ghorg.DependabotConfigPath)
					fmt.Println(ghorg.DefaultDependabotGroupedPRTemplate())
				}
			}
		}

		if validTargets == 0 {
			log.Fatal("No valid repositories found for --enable-dependabot")
		}
		return
	}

	if *userRepoAccess {
		if *repoName == "" {
			log.Fatal("--repo flag is required when using --user-repo-access")
		}
		if !ghorg.IsRepository(ctx, client, ghorg.ORG, *repoName) {
			log.Fatalf("Repository '%s' not found in organization '%s'", *repoName, ghorg.ORG)
		}
		if len(userOrRepoList) == 0 {
			log.Fatal("A username or email is required when using --user-repo-access")
		}
		userSlug := userOrRepoList[0]
		login, err := ghorg.ResolveLogin(ctx, tc, &userSlug)
		if err != nil || login == "" {
			log.Fatalf("Unable to resolve user '%s'", userSlug)
		}
		out, err := ghorg.ReportUserRepoAccess(ctx, client, tc, ghorg.ORG, login, *repoName)
		if err != nil {
			log.Printf("Error generating access report: %s", err)
			return
		}
		fmt.Print(out)
		return
	}

	if *adminUserReport {
		if len(userOrRepoList) == 0 {
			if err := ghorg.StreamOrgAdminRepoAccessReport(ctx, client, os.Stdout, ghorg.ORG, *resumeAfterRepo, *checkpointFile); err != nil {
				log.Printf("Error generating org-wide admin access report: %s", err)
			}
			return
		}
		userSlug := userOrRepoList[0]
		login, err := ghorg.ResolveLogin(ctx, tc, &userSlug)
		if err != nil || login == "" {
			log.Fatalf("Unable to resolve user '%s'", userSlug)
		}
		out, err := ghorg.ReportUserAdminRepoAccess(ctx, client, tc, ghorg.ORG, login, *resumeAfterRepo)
		if err != nil {
			log.Printf("Error generating admin access report: %s", err)
			return
		}
		fmt.Print(out)
		return
	}

	if *findCommonTeams {
		var repoNames []string
		for _, entitySlug := range userOrRepoList {
			if entitySlug == "" {
				continue
			}
			if !ghorg.IsRepository(ctx, client, ghorg.ORG, entitySlug) {
				log.Printf("Warning: '%s' is not a valid repository in organization '%s', skipping", entitySlug, ghorg.ORG)
				continue
			}
			repoNames = append(repoNames, entitySlug)
		}

		if len(repoNames) == 0 {
			log.Fatal("No valid repositories found in the provided arguments")
		}

		out, err := ghorg.FindAndReportTeamsWithAccessToAllRepos(ctx, client, ghorg.ORG, repoNames)
		if err != nil {
			log.Printf("Error finding common teams: %s", err)
			return
		}
		fmt.Print(out)
		return
	}

	if len(userOrRepoList) == 0 {
		log.Fatal("Usage is: ghOrgTool <options> <logins or repository names>")
	}

	for i := 0; i < len(userOrRepoList); i++ {
		entitySlug := userOrRepoList[i]
		if entitySlug == "" {
			continue
		}

		entType, resolvedName := detectEntityType(ctx, client, tc, entitySlug)

		switch entType {
		case entityRepository:
			if _, err := ghorg.CheckRepository(ctx, client, ghorg.ORG, resolvedName); err != nil {
				log.Printf("Can't resolve Repository %s: %s", resolvedName, err)
				continue
			}
			out, err := ghorg.ReportRepositoryTeams(ctx, client, ghorg.ORG, resolvedName)
			if err != nil {
				log.Printf("Unable to resolve teams for Repository %s: %s", resolvedName, err)
				continue
			}
			fmt.Print(out)

		case entityUser:
			log.Printf("Processing user %s", resolvedName)

			if *resetFlag {
				ghorg.Prompt(fmt.Sprintf("https://github.com/orgs/%s/people/%s/sso", ghorg.ORG, resolvedName))
				log.Printf("Reset Link: https://github.com/orgs/%s/people/%s/sso", ghorg.ORG, resolvedName)
				continue
			}

			valid, ghUser, reasons := ghorg.UserIsValid(ctx, client, tc, resolvedName)
			if !valid {
				for _, reason := range reasons {
					ghorg.Prompt(reason)
					log.Println(reason)
				}
				continue
			}

			if *addToTM {
				team, err := ghorg.GetTeamByName(ctx, client, ghorg.ORG, *teamName)
				if err != nil {
					log.Printf("Unable to find team %s: %s", *teamName, err)
					continue
				}
				added, err := ghorg.CheckAndAddMember(ctx, client, team, ghUser)
				if err != nil {
					log.Printf("Error adding user %s to team %s: %s", *ghUser.Login, *team.Name, err)
					continue
				}
				if added {
					ghorg.Prompt(fmt.Sprintf("User %s added to %s", *ghUser.Login, *team.Name))
					log.Println("User", *ghUser.Login, "added to", *team.Name)
				} else {
					log.Println("User", *ghUser.Login, "is already a member of", *team.Name)
				}
				continue
			}

			out, err := ghorg.ReportUserTeams(ctx, tc, ghorg.ORG, resolvedName)
			if err == nil {
				fmt.Print(out)
			} else {
				log.Println("Unable to get teams: ", err)
			}

		default:
			ghorg.Prompt(fmt.Sprintf("Unable to identify '%s' as a user or repository.", entitySlug))
			log.Printf("Unable to identify '%s' as a user or repository.", entitySlug)
		}
	}
}

func printHelp(defaultTeam string) {
	fmt.Println("ghOrgTool - GitHub Organization Management Tool")
	fmt.Println("\nUSAGE:")
	fmt.Println("  ghOrgTool [options] <usernames/emails or repository names>")
	fmt.Println("\nUSER OPERATIONS:")
	fmt.Println("  -a, --add                    Add users to a team (use with --team)")
	fmt.Println("  -r, --reset                  Generate SSO reset link for users")
	fmt.Println("\nTEAM OPERATIONS:")
	fmt.Println("  -d, --describe-team          Show detailed summary of a team (use with --team)")
	fmt.Println("\nREPOSITORY OPERATIONS:")
	fmt.Println("  -A, --add-repo-admin         Add users as admin collaborators to a repository (requires --repo)")
	fmt.Println("  -B, --enable-dependabot      Enable Dependabot alerts and security updates")
	fmt.Println("  -P, --dependabot-grouped-prs Optional with --enable-dependabot: verify grouped PR config and print guidance")
	fmt.Println("  -L, --list-repo-collaborators")
	fmt.Println("                               List all collaborators on a repository (requires --repo)")
	fmt.Println("  -S, --list-actions-storage   List top 10 repositories by Actions cache usage and org billable constrained storage")
	fmt.Println("  -G, --recent-admin-grants    List users recently granted admin access to any org repo (Monday includes since Friday, still active)")
	fmt.Println("  -M, --recent-admin-removals  List users recently removed from admin access on any org repo (same lookback window)")
	fmt.Println("  -c, --find-common-teams      Find teams with access to ALL specified repositories")
	fmt.Println("  -u, --user-repo-access       Report a user's effective access to a repository via team membership (requires --repo)")
	fmt.Println("  -U, --admin-user-report      Report repositories where users have effective admin access via teams or direct collaborator links")
	fmt.Println("\nOPTIONS:")
	fmt.Printf("  -s, --team <name>            Specify team name (default: '%s')\n", defaultTeam)
	fmt.Println("  -R, --repo <name>            Specify repository name for repo operations")
	fmt.Println("      --resume-after-repo      Resume all-repos access reporting after the named repository")
	fmt.Println("      --checkpoint-file        Persist the last completed repo for deterministic admin-user-report restarts")
	fmt.Println("  -i, --init                   Initialize configuration file interactively")
	fmt.Println("  -t, --rotate-token           Rotate/update GitHub token in configuration")
	fmt.Println("  -h, --help                   Show this help message")
	fmt.Println("\nEXAMPLES:")
	fmt.Println("  # Initialize configuration (first time setup)")
	fmt.Println("  ghOrgTool --init")
	fmt.Println("\n  # Update/rotate GitHub token")
	fmt.Println("  ghOrgTool --rotate-token")
	fmt.Println("\n  # List teams for a user (default behavior)")
	fmt.Println("  ghOrgTool user1")
	fmt.Println("\n  # List teams for a repository (default behavior)")
	fmt.Println("  ghOrgTool my-repo")
	fmt.Println("\n  # Add users to the default team")
	fmt.Println("  ghOrgTool --add user1 user2@example.com")
	fmt.Println("\n  # Add users to a specific team")
	fmt.Println("  ghOrgTool --add --team 'Engineering Team' user1 user2")
	fmt.Println("\n  # Generate SSO reset link")
	fmt.Println("  ghOrgTool --reset username")
	fmt.Println("\n  # Add user as admin to a repository")
	fmt.Println("  ghOrgTool --add-repo-admin --repo my-repo user1 user2")
	fmt.Println("\n  # Enable Dependabot on one repository")
	fmt.Println("  ghOrgTool --enable-dependabot --repo my-repo")
	fmt.Println("\n  # Enable Dependabot and check grouped PR config (no direct commits)")
	fmt.Println("  ghOrgTool --enable-dependabot --dependabot-grouped-prs --repo my-repo")
	fmt.Println("\n  # Enable Dependabot on multiple repositories")
	fmt.Println("  ghOrgTool --enable-dependabot repo1 repo2 repo3")
	fmt.Println("\n  # List all collaborators on a repository")
	fmt.Println("  ghOrgTool --list-repo-collaborators --repo my-repo")
	fmt.Println("\n  # List top repositories by Actions cache usage and show billable constrained storage")
	fmt.Println("  ghOrgTool --list-actions-storage")
	fmt.Println("\n  # List users recently granted admin access to any org repo")
	fmt.Println("  ghOrgTool --recent-admin-grants")
	fmt.Println("\n  # List users recently removed from admin access on any org repo")
	fmt.Println("  ghOrgTool --recent-admin-removals")
	fmt.Println("\n  # Find teams with access to multiple repositories")
	fmt.Println("  ghOrgTool --find-common-teams repo1 repo2 repo3")
	fmt.Println("\n  # Show detailed summary of a team")
	fmt.Println("  ghOrgTool --describe-team --team 'Engineering Team'")
	fmt.Println("\n  # Report repos where any user has admin access via teams")
	fmt.Println("  ghOrgTool --admin-user-report")
	fmt.Println("\n  # Report repos where a specific user has admin access via teams")
	fmt.Println("  ghOrgTool --admin-user-report someuser")
	fmt.Println("\n  # Resume an interrupted admin access scan")
	fmt.Println("  ghOrgTool --admin-user-report --resume-after-repo some-repo")
	fmt.Println("\n  # Run with a checkpoint file for deterministic restarts")
	fmt.Println("  ghOrgTool --admin-user-report --checkpoint-file admin-user-report.checkpoint > admin-users.csv")
}
```

`EnableDependabot`'s result fields are referenced above as `result.VulnerabilityAlertsEnabled` / `result.AutomatedFixesEnabled` (capitalized) — these are exported as part of Task 6 Step 6's rename table, on the unexported `dependabotEnableResult` struct returned by `EnableDependabot` (the struct type itself stays unexported; only its fields need exporting, since `cmd/ghOrgTool` accesses them via `result.` without ever needing to name the type).

- [ ] **Step 3: Update `cmd/ghOrgTool/main_test.go`**

Move the file (already done via `git mv` in Step 1), keep `package main`, and update any call sites that referenced now-relocated functions (`detectEntityType`, `decomposeGithubURL`, `normalizeEntityArgument` stay local and need no changes; anything testing `connect()` directly needs updating to either delete that test — since `Connect` is now tested in `internal/ghorg/client_test.go` — or leave `cmd/ghOrgTool`-level tests focused on flag dispatch and entity detection only).

- [ ] **Step 4: Build and vet**

Run: `go build ./... && go vet ./...`
Expected: both succeed with zero errors

- [ ] **Step 5: Run the full test suite matching CI**

Run: `xvfb-run -a go test -race -count=1 -coverprofile=coverage.out ./...` (on macOS, drop `xvfb-run -a` — that's only needed on Linux CI for the clipboard package; just run `go test -race -count=1 -coverprofile=coverage.out ./...`)
Expected: all PASS

- [ ] **Step 6: Manual smoke test against a real org (optional but recommended before committing)**

Run: `go run ./cmd/ghOrgTool --help` and `go run ./cmd/ghOrgTool someuser` (with `GITHUB_AUTH_TOKEN` set) and confirm the output matches what `git stash` + the old root `main.go` produced for the same inputs.

- [ ] **Step 7: Update the README's `go install` line**

In `README.md`, change:

```
go install github.com/glow-example-org/ghOrgTool@latest
```

to:

```
go install github.com/glow-mdsol/ghOrgTool/cmd/ghOrgTool@latest
```

(Note this also fixes a pre-existing mismatch — the README currently says `glow-example-org` but `go.mod`'s module path is `glow-mdsol`.)

- [ ] **Step 8: Commit**

```bash
git add cmd/ghOrgTool/main.go cmd/ghOrgTool/main_test.go README.md
git commit -m "refactor: move CLI into cmd/ghOrgTool, wire dispatch through internal/ghorg"
```

---

## Task 10: Add Fyne dependency, scaffold `cmd/ghOrgToolGUI` with a minimal tray icon

**Files:**
- Modify: `go.mod`, `go.sum`
- Create: `cmd/ghOrgToolGUI/main.go`

**Interfaces:**
- Produces: a runnable binary at `cmd/ghOrgToolGUI` with a tray icon and a "Quit" menu item — no commands wired yet (that's Tasks 11–14)

- [ ] **Step 1: Add the dependency**

```bash
go get fyne.io/fyne/v2@latest
go get fyne.io/systray@latest
go mod tidy
```

- [ ] **Step 2: Scaffold the entry point**

Create `cmd/ghOrgToolGUI/main.go`, using the `fyne.io/fyne/v2/driver/desktop` package's `desktop.App` interface (obtained via a type assertion on the running `fyne.App`) to install a system tray menu:

```go
package main

import (
	"log"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/driver/desktop"

	"github.com/glow-mdsol/ghOrgTool/internal/ghorg"
)

func main() {
	ghorg.ORG = ghorg.GetOrgLogin()

	a := app.NewWithID("com.glow-mdsol.ghOrgToolGUI")

	if desk, ok := a.(desktop.App); ok {
		menu := fyne.NewMenu("ghOrgTool",
			fyne.NewMenuItem("Quit", func() { a.Quit() }),
		)
		desk.SetSystemTrayMenu(menu)
	} else {
		log.Fatal("system tray is not supported on this platform build")
	}

	a.Run()
}
```

- [ ] **Step 3: Build it**

Run: `go build ./cmd/ghOrgToolGUI/...`
Expected: succeeds. (On Linux this needs the same `libx11-dev libxfixes-dev` system packages the clipboard dependency already requires per `.github/workflows/test.yml` — Fyne needs a few more: `libgl1-mesa-dev xorg-dev`. Add these to the CI workflow's "Install Linux clipboard dependencies" step, renaming it to "Install Linux GUI dependencies", in Task 17.)

- [ ] **Step 4: Manual verification**

Run: `go run ./cmd/ghOrgToolGUI` — a tray icon should appear (default Fyne icon, since we haven't set a custom one yet); clicking it shows a "Quit" menu item that exits the app.

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum cmd/ghOrgToolGUI/main.go
git commit -m "feat: scaffold cmd/ghOrgToolGUI with a minimal tray icon"
```

---

## Task 11: Command registry

**Files:**
- Create: `cmd/ghOrgToolGUI/commands.go`
- Create: `cmd/ghOrgToolGUI/commands_test.go`

**Interfaces:**
- Produces: `Field{Key, Label string; Required bool}`, `Command{Name, Group string; Fields []Field; Mutating bool; ConfirmText func(values map[string]string) string; Run func(ctx context.Context, client *github.Client, tc *http.Client, values map[string]string) (string, error)}`, `allCommands []Command`

- [ ] **Step 1: Write the failing test**

```go
package main

import "testing"

func TestAllCommands_EveryCommandHasNameAndRun(t *testing.T) {
	if len(allCommands) == 0 {
		t.Fatal("expected at least one registered command")
	}
	seen := make(map[string]bool)
	for _, cmd := range allCommands {
		if cmd.Name == "" {
			t.Errorf("command with empty Name: %+v", cmd)
		}
		if seen[cmd.Name] {
			t.Errorf("duplicate command name: %s", cmd.Name)
		}
		seen[cmd.Name] = true
		if cmd.Run == nil {
			t.Errorf("command %s has a nil Run handler", cmd.Name)
		}
		if cmd.Mutating && cmd.ConfirmText == nil {
			t.Errorf("mutating command %s has no ConfirmText", cmd.Name)
		}
	}
}

func TestAllCommands_ExpectedCommandsPresent(t *testing.T) {
	want := []string{
		"admin-user-report", "recent-admin-grants", "recent-admin-removals",
		"list-actions-storage", "list-repo-collaborators", "user-repo-access",
		"find-common-teams", "describe-team", "user-check",
		"add-to-team", "add-repo-admin", "enable-dependabot", "reset-sso-link",
	}
	byName := make(map[string]bool)
	for _, cmd := range allCommands {
		byName[cmd.Name] = true
	}
	for _, name := range want {
		if !byName[name] {
			t.Errorf("expected command %q to be registered", name)
		}
	}
}
```

- [ ] **Step 2: Confirm it fails**

Run: `go test ./cmd/ghOrgToolGUI/... -run TestAllCommands -v`
Expected: FAIL (compile error — `allCommands` doesn't exist)

- [ ] **Step 3: Implement the registry**

Create `cmd/ghOrgToolGUI/commands.go`:

```go
package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/glow-mdsol/ghOrgTool/internal/ghorg"
	"github.com/google/go-github/v84/github"
)

// Field describes one input the user must supply for a Command.
type Field struct {
	Key      string // map key used to look up the value in Command.Run
	Label    string // shown to the user
	Required bool
}

// Command describes one GUI-triggerable action, backed by internal/ghorg.
type Command struct {
	Name        string // stable id, used in tests and the tray menu
	Label       string // human-readable tray menu text
	Group       string // "Reports", "Actions", or "Settings"
	Fields      []Field
	Mutating    bool
	ConfirmText func(values map[string]string) string
	Run         func(ctx context.Context, client *github.Client, tc *http.Client, values map[string]string) (string, error)
}

var allCommands = []Command{
	{
		Name:  "admin-user-report",
		Label: "Admin User Report",
		Group: "Reports",
		Fields: []Field{
			{Key: "user", Label: "Filter to user (optional)"},
		},
		Run: func(ctx context.Context, client *github.Client, tc *http.Client, values map[string]string) (string, error) {
			if values["user"] == "" {
				var b strings.Builder
				if err := ghorg.StreamOrgAdminRepoAccessReport(ctx, client, &b, ghorg.ORG, "", ""); err != nil {
					return b.String(), err
				}
				return b.String(), nil
			}
			login, err := ghorg.ResolveLogin(ctx, tc, &[]string{values["user"]}[0])
			if err != nil || login == "" {
				return "", fmt.Errorf("unable to resolve user %q", values["user"])
			}
			return ghorg.ReportUserAdminRepoAccess(ctx, client, tc, ghorg.ORG, login, "")
		},
	},
	{
		Name:  "recent-admin-grants",
		Label: "Recent Admin Grants",
		Group: "Reports",
		Run: func(ctx context.Context, client *github.Client, tc *http.Client, values map[string]string) (string, error) {
			return ghorg.ListRecentAdminGrants(ctx, client, ghorg.ORG)
		},
	},
	{
		Name:  "recent-admin-removals",
		Label: "Recent Admin Removals",
		Group: "Reports",
		Run: func(ctx context.Context, client *github.Client, tc *http.Client, values map[string]string) (string, error) {
			return ghorg.ListRecentAdminRemovals(ctx, client, ghorg.ORG)
		},
	},
	{
		Name:  "list-actions-storage",
		Label: "Actions Storage Report",
		Group: "Reports",
		Run: func(ctx context.Context, client *github.Client, tc *http.Client, values map[string]string) (string, error) {
			return ghorg.ListTopActionsCacheUsageByRepo(ctx, client, ghorg.ORG, 10)
		},
	},
	{
		Name:  "list-repo-collaborators",
		Label: "Repository Collaborators",
		Group: "Reports",
		Fields: []Field{
			{Key: "repo", Label: "Repository", Required: true},
		},
		Run: func(ctx context.Context, client *github.Client, tc *http.Client, values map[string]string) (string, error) {
			return ghorg.ListRepositoryCollaborators(ctx, client, ghorg.ORG, values["repo"])
		},
	},
	{
		Name:  "user-repo-access",
		Label: "User Repository Access",
		Group: "Reports",
		Fields: []Field{
			{Key: "user", Label: "User", Required: true},
			{Key: "repo", Label: "Repository", Required: true},
		},
		Run: func(ctx context.Context, client *github.Client, tc *http.Client, values map[string]string) (string, error) {
			userSlug := values["user"]
			login, err := ghorg.ResolveLogin(ctx, tc, &userSlug)
			if err != nil || login == "" {
				return "", fmt.Errorf("unable to resolve user %q", values["user"])
			}
			return ghorg.ReportUserRepoAccess(ctx, client, tc, ghorg.ORG, login, values["repo"])
		},
	},
	{
		Name:  "find-common-teams",
		Label: "Find Common Teams",
		Group: "Reports",
		Fields: []Field{
			{Key: "repos", Label: "Repositories (comma-separated)", Required: true},
		},
		Run: func(ctx context.Context, client *github.Client, tc *http.Client, values map[string]string) (string, error) {
			return ghorg.FindAndReportTeamsWithAccessToAllRepos(ctx, client, ghorg.ORG, splitAndTrim(values["repos"]))
		},
	},
	{
		Name:  "describe-team",
		Label: "Describe Team",
		Group: "Reports",
		Fields: []Field{
			{Key: "team", Label: "Team name", Required: true},
		},
		Run: func(ctx context.Context, client *github.Client, tc *http.Client, values map[string]string) (string, error) {
			team, err := ghorg.GetTeamByName(ctx, client, ghorg.ORG, values["team"])
			if err != nil {
				return "", err
			}
			return ghorg.SummarizeTeam(ctx, client, team), nil
		},
	},
	{
		Name:  "user-check",
		Label: "User Check",
		Group: "Reports",
		Fields: []Field{
			{Key: "user", Label: "User", Required: true},
		},
		Run: func(ctx context.Context, client *github.Client, tc *http.Client, values map[string]string) (string, error) {
			valid, ghUser, reasons := ghorg.UserIsValid(ctx, client, tc, values["user"])
			if !valid {
				return strings.Join(reasons, "\n"), nil
			}
			return ghorg.ReportUserTeams(ctx, tc, ghorg.ORG, *ghUser.Login)
		},
	},
	{
		Name:     "add-to-team",
		Label:    "Add User to Team",
		Group:    "Actions",
		Mutating: true,
		Fields: []Field{
			{Key: "user", Label: "User", Required: true},
			{Key: "team", Label: "Team", Required: true},
		},
		ConfirmText: func(values map[string]string) string {
			return fmt.Sprintf("Add %s to team %s?", values["user"], values["team"])
		},
		Run: func(ctx context.Context, client *github.Client, tc *http.Client, values map[string]string) (string, error) {
			valid, ghUser, reasons := ghorg.UserIsValid(ctx, client, tc, values["user"])
			if !valid {
				return strings.Join(reasons, "\n"), nil
			}
			team, err := ghorg.GetTeamByName(ctx, client, ghorg.ORG, values["team"])
			if err != nil {
				return "", err
			}
			added, err := ghorg.CheckAndAddMember(ctx, client, team, ghUser)
			if err != nil {
				return "", err
			}
			if added {
				return fmt.Sprintf("User %s added to %s", *ghUser.Login, *team.Name), nil
			}
			return fmt.Sprintf("User %s is already a member of %s", *ghUser.Login, *team.Name), nil
		},
	},
	{
		Name:     "add-repo-admin",
		Label:    "Add Repo Admin",
		Group:    "Actions",
		Mutating: true,
		Fields: []Field{
			{Key: "user", Label: "User", Required: true},
			{Key: "repo", Label: "Repository", Required: true},
		},
		ConfirmText: func(values map[string]string) string {
			return fmt.Sprintf("Add %s as admin collaborator on %s/%s?", values["user"], ghorg.ORG, values["repo"])
		},
		Run: func(ctx context.Context, client *github.Client, tc *http.Client, values map[string]string) (string, error) {
			userSlug := values["user"]
			login, err := ghorg.ResolveLogin(ctx, tc, &userSlug)
			if err != nil || login == "" {
				return "", fmt.Errorf("unable to resolve user %q", values["user"])
			}
			if err := ghorg.AddUserAsRepoCollaborator(ctx, client, ghorg.ORG, values["repo"], login); err != nil {
				return "", err
			}
			return fmt.Sprintf("%s added as admin collaborator on %s/%s", login, ghorg.ORG, values["repo"]), nil
		},
	},
	{
		Name:     "enable-dependabot",
		Label:    "Enable Dependabot",
		Group:    "Actions",
		Mutating: true,
		Fields: []Field{
			{Key: "repo", Label: "Repository", Required: true},
		},
		ConfirmText: func(values map[string]string) string {
			return fmt.Sprintf("Enable Dependabot alerts and security updates on %s/%s?", ghorg.ORG, values["repo"])
		},
		Run: func(ctx context.Context, client *github.Client, tc *http.Client, values map[string]string) (string, error) {
			result, err := ghorg.EnableDependabot(ctx, client, ghorg.ORG, values["repo"])
			if err != nil {
				return "", err
			}
			switch {
			case result.VulnerabilityAlertsEnabled && result.AutomatedFixesEnabled:
				return fmt.Sprintf("Enabled Dependabot alerts and security updates for %s", values["repo"]), nil
			case result.VulnerabilityAlertsEnabled:
				return fmt.Sprintf("Enabled Dependabot alerts for %s (security updates were already enabled)", values["repo"]), nil
			case result.AutomatedFixesEnabled:
				return fmt.Sprintf("Enabled Dependabot security updates for %s (alerts were already enabled)", values["repo"]), nil
			default:
				return fmt.Sprintf("Dependabot is already fully enabled for %s", values["repo"]), nil
			}
		},
	},
	{
		Name:     "reset-sso-link",
		Label:    "Reset SSO Link",
		Group:    "Actions",
		Mutating: true,
		Fields: []Field{
			{Key: "user", Label: "User", Required: true},
		},
		ConfirmText: func(values map[string]string) string {
			return fmt.Sprintf("Generate an SSO reset link for %s?", values["user"])
		},
		Run: func(ctx context.Context, client *github.Client, tc *http.Client, values map[string]string) (string, error) {
			return fmt.Sprintf("https://github.com/orgs/%s/people/%s/sso", ghorg.ORG, values["user"]), nil
		},
	},
}

func splitAndTrim(csv string) []string {
	var out []string
	for _, part := range strings.Split(csv, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
```

(`"strings"` in the import block above is used by `splitAndTrim`, `strings.Join` in the `user-check` handler, and `strings.Builder` in the `admin-user-report` handler.)

- [ ] **Step 4: Run the test again**

Run: `go test ./cmd/ghOrgToolGUI/... -run TestAllCommands -v`
Expected: PASS

- [ ] **Step 5: Build and test**

Run: `go build ./cmd/ghOrgToolGUI/... && go test ./cmd/ghOrgToolGUI/... -v`
Expected: both succeed

- [ ] **Step 6: Commit**

```bash
git add cmd/ghOrgToolGUI/commands.go cmd/ghOrgToolGUI/commands_test.go
git commit -m "feat: add GUI command registry covering full CLI parity"
```

---

## Task 12: Pure helpers — form validation + confirmation gating

**Files:**
- Create: `cmd/ghOrgToolGUI/formvalues.go`
- Create: `cmd/ghOrgToolGUI/formvalues_test.go`
- Create: `cmd/ghOrgToolGUI/confirm.go`
- Create: `cmd/ghOrgToolGUI/confirm_test.go`

**Interfaces:**
- Consumes: `Command`, `Field` (Task 11)
- Produces: `missingRequiredFields(cmd Command, values map[string]string) []string`, `needsConfirmation(cmd Command) bool`, `showConfirm(win fyne.Window, message string, onConfirm func())`

These two helpers are built together because `window.go` (Task 13) depends on both from the start — building them first means Task 13's window compiles cleanly the first time instead of needing a follow-up wiring step.

- [ ] **Step 1: Write the failing test for the pure validation helper**

```go
package main

import (
	"reflect"
	"testing"
)

func TestMissingRequiredFields(t *testing.T) {
	cmd := Command{
		Fields: []Field{
			{Key: "user", Label: "User", Required: true},
			{Key: "repo", Label: "Repository", Required: true},
			{Key: "note", Label: "Note", Required: false},
		},
	}

	got := missingRequiredFields(cmd, map[string]string{"user": "alice", "repo": ""})
	want := []string{"Repository"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	got = missingRequiredFields(cmd, map[string]string{"user": "alice", "repo": "my-repo"})
	if len(got) != 0 {
		t.Errorf("expected no missing fields, got %v", got)
	}
}
```

- [ ] **Step 2: Confirm it fails**

Run: `go test ./cmd/ghOrgToolGUI/... -run TestMissingRequiredFields -v`
Expected: FAIL with "undefined: missingRequiredFields"

- [ ] **Step 3: Implement it**

Create `cmd/ghOrgToolGUI/formvalues.go`:

```go
package main

import "strings"

// missingRequiredFields returns the labels of every required field in cmd
// whose value in values is empty or whitespace-only.
func missingRequiredFields(cmd Command, values map[string]string) []string {
	var missing []string
	for _, f := range cmd.Fields {
		if f.Required && strings.TrimSpace(values[f.Key]) == "" {
			missing = append(missing, f.Label)
		}
	}
	return missing
}
```

- [ ] **Step 4: Run the test again**

Run: `go test ./cmd/ghOrgToolGUI/... -run TestMissingRequiredFields -v`
Expected: PASS

- [ ] **Step 5: Write the failing test for the pure confirmation-gating function**

```go
func TestNeedsConfirmation(t *testing.T) {
	if needsConfirmation(Command{Mutating: false}) {
		t.Error("expected non-mutating command to not need confirmation")
	}
	if !needsConfirmation(Command{Mutating: true}) {
		t.Error("expected mutating command to need confirmation")
	}
}
```

Add this to a new `cmd/ghOrgToolGUI/confirm_test.go`.

- [ ] **Step 6: Confirm it fails**

Run: `go test ./cmd/ghOrgToolGUI/... -run TestNeedsConfirmation -v`
Expected: FAIL with "undefined: needsConfirmation"

- [ ] **Step 7: Implement `confirm.go`**

Create `cmd/ghOrgToolGUI/confirm.go`:

```go
package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
)

// needsConfirmation reports whether cmd must be gated behind a confirm dialog.
func needsConfirmation(cmd Command) bool {
	return cmd.Mutating
}

// showConfirm shows a native confirmation dialog with message; onConfirm runs
// only if the user accepts.
func showConfirm(win fyne.Window, message string, onConfirm func()) {
	dialog.ShowConfirm("Confirm", message, func(confirmed bool) {
		if confirmed {
			onConfirm()
		}
	}, win)
}
```

- [ ] **Step 8: Run both tests again**

Run: `go test ./cmd/ghOrgToolGUI/... -run 'TestMissingRequiredFields|TestNeedsConfirmation' -v`
Expected: both PASS

- [ ] **Step 9: Commit**

```bash
git add cmd/ghOrgToolGUI/formvalues.go cmd/ghOrgToolGUI/formvalues_test.go cmd/ghOrgToolGUI/confirm.go cmd/ghOrgToolGUI/confirm_test.go
git commit -m "feat: add pure form-validation and confirmation-gating helpers for the GUI"
```

---

## Task 13: Shared output window

**Files:**
- Create: `cmd/ghOrgToolGUI/window.go`

**Interfaces:**
- Consumes: `missingRequiredFields`, `needsConfirmation`, `showConfirm` (Task 12); `Command` (Task 11)
- Produces: `showOutputWindow(a fyne.App, cmd Command, client *github.Client, tc *http.Client)`

Rendering actual Fyne widgets isn't practically unit-testable without a display, so there's no red/green cycle for this task — write it directly and verify manually in Step 2.

- [ ] **Step 1: Implement the shared output window**

Create `cmd/ghOrgToolGUI/window.go`:

```go
package main

import (
	"context"
	"net/http"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/google/go-github/v84/github"
)

var (
	sharedWindow     fyne.Window
	sharedWindowOnce sync.Once
)

func getSharedWindow(a fyne.App) fyne.Window {
	sharedWindowOnce.Do(func() {
		sharedWindow = a.NewWindow("ghOrgTool")
		sharedWindow.Resize(fyne.NewSize(640, 480))
		sharedWindow.SetCloseIntercept(func() { sharedWindow.Hide() })
	})
	return sharedWindow
}

// showOutputWindow opens (or focuses) the shared window with cmd's input
// form. Submitting the form runs cmd.Run in the background and sets the
// result into a scrollable output area, gated behind a confirm dialog first
// if cmd is mutating.
func showOutputWindow(a fyne.App, cmd Command, client *github.Client, tc *http.Client) {
	win := getSharedWindow(a)

	values := make(map[string]string)
	entries := make(map[string]*widget.Entry)
	formItems := make([]*widget.FormItem, 0, len(cmd.Fields))
	for _, f := range cmd.Fields {
		entry := widget.NewEntry()
		entries[f.Key] = entry
		formItems = append(formItems, widget.NewFormItem(f.Label, entry))
	}

	output := widget.NewLabel("")
	output.Wrapping = fyne.TextWrapWord
	errorBanner := widget.NewLabel("")
	errorBanner.Importance = widget.DangerImportance
	errorBanner.Hide()

	scroll := container.NewVScroll(output)
	scroll.SetMinSize(fyne.NewSize(600, 320))

	form := &widget.Form{
		Items: formItems,
		OnSubmit: func() {
			for k, e := range entries {
				values[k] = e.Text
			}
			if missing := missingRequiredFields(cmd, values); len(missing) > 0 {
				errorBanner.SetText("Missing required fields: " + strings.Join(missing, ", "))
				errorBanner.Show()
				return
			}
			errorBanner.Hide()
			runCommand(win, cmd, client, tc, values, output, errorBanner)
		},
	}
	form.SubmitText = "Run"
	if cmd.Mutating {
		form.SubmitText = "Run (will prompt to confirm)"
	}

	win.SetContent(container.NewBorder(form, nil, nil, nil, container.NewVBox(errorBanner, scroll)))
	win.SetTitle("ghOrgTool — " + cmd.Label)
	win.Show()
	win.RequestFocus()
}

func runCommand(win fyne.Window, cmd Command, client *github.Client, tc *http.Client, values map[string]string, output *widget.Label, errorBanner *widget.Label) {
	start := func() {
		go func() {
			out, err := cmd.Run(context.Background(), client, tc, values)
			fyne.Do(func() {
				if err != nil {
					errorBanner.SetText(err.Error())
					errorBanner.Show()
				} else {
					errorBanner.Hide()
				}
				output.SetText(out)
			})
		}()
	}

	if !needsConfirmation(cmd) {
		start()
		return
	}

	showConfirm(win, cmd.ConfirmText(values), start)
}
```

- [ ] **Step 2: Build**

Run: `go build ./cmd/ghOrgToolGUI/...`
Expected: succeeds

- [ ] **Step 3: Manual verification**

Temporarily add `showOutputWindow(a, allCommands[0], nil, nil)` right before `a.Run()` in `main.go` (there's no tray-menu wiring to reach it yet — that's Task 16), then run `go run ./cmd/ghOrgToolGUI`. Confirm: the form renders with the right fields for `allCommands[0]`, and — temporarily pick a mutating command's index instead, e.g. the `add-to-team` entry, to check this part too — submitting a mutating command shows a confirm dialog first, and cancelling it does not run the command. Revert the temporary line afterward.

- [ ] **Step 4: Commit**

```bash
git add cmd/ghOrgToolGUI/window.go
git commit -m "feat: add shared GUI output window with form rendering and confirm gating"
```

---

## Task 14: Settings window

**Files:**
- Create: `cmd/ghOrgToolGUI/settings.go`

**Interfaces:**
- Consumes: `ghorg.LoadConfig`, `ghorg.SaveConfig`, `ghorg.Config`
- Produces: `showSettingsWindow(a fyne.App)`

- [ ] **Step 1: Implement (no pure logic to isolate here beyond what `LoadConfig`/`SaveConfig` already test in `internal/ghorg`, so this is a direct build-and-manually-verify step)**

Create `cmd/ghOrgToolGUI/settings.go`:

```go
package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/glow-mdsol/ghOrgTool/internal/ghorg"
)

// showSettingsWindow opens a window for editing org login, default team, and
// GitHub token, backed directly by ghorg.LoadConfig/SaveConfig.
func showSettingsWindow(a fyne.App) {
	win := a.NewWindow("ghOrgTool — Settings")
	win.Resize(fyne.NewSize(420, 240))

	cfg := ghorg.LoadConfig()

	orgEntry := widget.NewEntry()
	orgEntry.SetText(cfg.OrgLogin)
	teamEntry := widget.NewEntry()
	teamEntry.SetText(cfg.DefaultTeam)
	tokenEntry := widget.NewPasswordEntry()
	tokenEntry.SetText(cfg.GithubToken)

	status := widget.NewLabel("")

	form := &widget.Form{
		Items: []*widget.FormItem{
			widget.NewFormItem("Organization login", orgEntry),
			widget.NewFormItem("Default team", teamEntry),
			widget.NewFormItem("GitHub token", tokenEntry),
		},
		OnSubmit: func() {
			cfg.OrgLogin = orgEntry.Text
			cfg.DefaultTeam = teamEntry.Text
			cfg.GithubToken = tokenEntry.Text
			if err := ghorg.SaveConfig(cfg); err != nil {
				status.SetText("Error saving: " + err.Error())
				return
			}
			ghorg.ORG = ghorg.GetOrgLogin()
			status.SetText("Saved.")
		},
	}
	form.SubmitText = "Save"

	win.SetContent(container.NewVBox(form, status))
	win.Show()
}
```

- [ ] **Step 2: Build**

Run: `go build ./cmd/ghOrgToolGUI/...`
Expected: succeeds

- [ ] **Step 3: Manual verification**

Temporarily call `showSettingsWindow(a)` before `a.Run()` in `main.go`, run `go run ./cmd/ghOrgToolGUI`, confirm the form pre-fills from the existing config file (create one first with `go run ./cmd/ghOrgTool --init` if none exists), edit a value, save, and confirm `cat ~/.config/ghOrgTool/config.json` (or the OS-appropriate path) reflects the change. Revert the temporary call.

- [ ] **Step 4: Commit**

```bash
git add cmd/ghOrgToolGUI/settings.go
git commit -m "feat: add GUI settings window backed by ghorg config load/save"
```

---

## Task 15: Autostart at login

**Files:**
- Create: `cmd/ghOrgToolGUI/autostart.go`
- Create: `cmd/ghOrgToolGUI/autostart_test.go`

**Interfaces:**
- Produces: `Enable(execPath string) error`, `Disable() error`, `IsEnabled() (bool, error)`

- [ ] **Step 1: Write the failing test (Linux/macOS `.desktop`/plist path, sandboxed to a temp `HOME`)**

```go
package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAutostart_EnableDisableRoundTrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("registry-based autostart is exercised manually on Windows, not in this suite")
	}

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	enabled, err := IsEnabled()
	if err != nil {
		t.Fatalf("unexpected error checking initial state: %v", err)
	}
	if enabled {
		t.Fatal("expected autostart to be disabled initially in a fresh temp HOME")
	}

	if err := Enable("/usr/local/bin/ghOrgToolGUI"); err != nil {
		t.Fatalf("unexpected error enabling autostart: %v", err)
	}

	enabled, err = IsEnabled()
	if err != nil {
		t.Fatalf("unexpected error checking state after enable: %v", err)
	}
	if !enabled {
		t.Fatal("expected autostart to be enabled after Enable()")
	}

	if err := Disable(); err != nil {
		t.Fatalf("unexpected error disabling autostart: %v", err)
	}

	enabled, err = IsEnabled()
	if err != nil {
		t.Fatalf("unexpected error checking state after disable: %v", err)
	}
	if enabled {
		t.Fatal("expected autostart to be disabled after Disable()")
	}

	// Sanity: the autostart file/plist should actually be gone.
	if runtime.GOOS == "darwin" {
		if _, err := os.Stat(filepath.Join(tmpHome, "Library", "LaunchAgents", "com.glow-mdsol.ghOrgToolGUI.plist")); !os.IsNotExist(err) {
			t.Error("expected LaunchAgent plist to be removed after Disable()")
		}
	} else {
		if _, err := os.Stat(filepath.Join(tmpHome, ".config", "autostart", "ghOrgToolGUI.desktop")); !os.IsNotExist(err) {
			t.Error("expected autostart .desktop file to be removed after Disable()")
		}
	}
}
```

- [ ] **Step 2: Confirm it fails**

Run: `go test ./cmd/ghOrgToolGUI/... -run TestAutostart_EnableDisableRoundTrip -v`
Expected: FAIL with "undefined: IsEnabled"

- [ ] **Step 3: Implement `autostart.go`**

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const autostartAppID = "com.glow-mdsol.ghOrgToolGUI"

// Enable registers execPath to launch automatically at login.
func Enable(execPath string) error {
	switch runtime.GOOS {
	case "darwin":
		return enableDarwin(execPath)
	case "linux":
		return enableLinux(execPath)
	case "windows":
		return enableWindows(execPath)
	default:
		return fmt.Errorf("autostart is not supported on %s", runtime.GOOS)
	}
}

// Disable removes the login-item registration, if any.
func Disable() error {
	switch runtime.GOOS {
	case "darwin":
		return disableDarwin()
	case "linux":
		return disableLinux()
	case "windows":
		return disableWindows()
	default:
		return fmt.Errorf("autostart is not supported on %s", runtime.GOOS)
	}
}

// IsEnabled reports whether autostart is currently registered.
func IsEnabled() (bool, error) {
	switch runtime.GOOS {
	case "darwin":
		return isEnabledDarwin()
	case "linux":
		return isEnabledLinux()
	case "windows":
		return isEnabledWindows()
	default:
		return false, fmt.Errorf("autostart is not supported on %s", runtime.GOOS)
	}
}

func darwinPlistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", autostartAppID+".plist"), nil
}

func enableDarwin(execPath string) error {
	path, err := darwinPlistPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
</dict>
</plist>
`, autostartAppID, execPath)
	return os.WriteFile(path, []byte(plist), 0644)
}

func disableDarwin() error {
	path, err := darwinPlistPath()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func isEnabledDarwin() (bool, error) {
	path, err := darwinPlistPath()
	if err != nil {
		return false, err
	}
	_, err = os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

func linuxDesktopFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "autostart", "ghOrgToolGUI.desktop"), nil
}

func enableLinux(execPath string) error {
	path, err := linuxDesktopFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	desktopEntry := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=ghOrgTool
Exec=%s
X-GNOME-Autostart-enabled=true
`, execPath)
	return os.WriteFile(path, []byte(desktopEntry), 0644)
}

func disableLinux() error {
	path, err := linuxDesktopFilePath()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func isEnabledLinux() (bool, error) {
	path, err := linuxDesktopFilePath()
	if err != nil {
		return false, err
	}
	_, err = os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}
```

Windows uses the registry rather than a file, so it can't be covered by the same temp-`HOME` test — implement it in a separate file so a build-tag-free `go build`/`go vet` on any OS still compiles all three (Go compiles all `.go` files in a package regardless of target OS unless they use `_windows.go`-style suffixes or build tags; since `enableWindows`/`disableWindows`/`isEnabledWindows` reference `golang.org/x/sys/windows/registry`, which only builds on Windows, put them in a file named `autostart_windows.go` with the implementation, and a stub in a same-named-pattern file for other platforms — OR, simpler and consistent with this file's `runtime.GOOS switch` style, use `golang.org/x/sys/windows/registry` only inside functions gated by a `//go:build windows` tag at the top of a separate `autostart_windows.go` file, with a non-Windows stub in `autostart_other.go`):

Create `cmd/ghOrgToolGUI/autostart_windows.go`:

```go
//go:build windows

package main

import "golang.org/x/sys/windows/registry"

const autostartRegistryValueName = "ghOrgToolGUI"

func enableWindows(execPath string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	return key.SetStringValue(autostartRegistryValueName, execPath)
}

func disableWindows() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	err = key.DeleteValue(autostartRegistryValueName)
	if err == registry.ErrNotExist {
		return nil
	}
	return err
}

func isEnabledWindows() (bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.QUERY_VALUE)
	if err != nil {
		return false, err
	}
	defer key.Close()
	_, _, err = key.GetStringValue(autostartRegistryValueName)
	if err == registry.ErrNotExist {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
```

Create `cmd/ghOrgToolGUI/autostart_other.go`:

```go
//go:build !windows

package main

import "fmt"

func enableWindows(execPath string) error { return fmt.Errorf("not on windows") }
func disableWindows() error               { return fmt.Errorf("not on windows") }
func isEnabledWindows() (bool, error)      { return false, fmt.Errorf("not on windows") }
```

Add the dependency: `go get golang.org/x/sys/windows/registry@latest` (the module already depends on `golang.org/x/sys` transitively; this just needs it promoted to a direct, or already-covered, requirement — run `go mod tidy` after adding the import and check `go.mod`).

- [ ] **Step 4: Run the test again**

Run: `go test ./cmd/ghOrgToolGUI/... -run TestAutostart_EnableDisableRoundTrip -v`
Expected: PASS

- [ ] **Step 5: Build for the current OS and run the full GUI package suite**

Run: `go build ./cmd/ghOrgToolGUI/... && go vet ./cmd/ghOrgToolGUI/... && go test ./cmd/ghOrgToolGUI/... -v`
Expected: all succeed

- [ ] **Step 6: Commit**

```bash
git add cmd/ghOrgToolGUI/autostart.go cmd/ghOrgToolGUI/autostart_windows.go cmd/ghOrgToolGUI/autostart_other.go cmd/ghOrgToolGUI/autostart_test.go go.mod go.sum
git commit -m "feat: add cross-platform login autostart for the tray app"
```

---

## Task 16: Wire the tray menu — commands, settings, autostart toggle, first-run bootstrap

**Files:**
- Modify: `cmd/ghOrgToolGUI/main.go`

**Interfaces:**
- Consumes: `allCommands`, `showOutputWindow`, `showSettingsWindow`, `Enable`/`Disable`/`IsEnabled`, `ghorg.LoadConfig`/`ghorg.SaveConfig`

- [ ] **Step 1: Rewrite `main.go`**

```go
package main

import (
	"log"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/driver/desktop"
	"github.com/google/go-github/v84/github"
	"net/http"

	"github.com/glow-mdsol/ghOrgTool/internal/ghorg"
)

func main() {
	ghorg.ORG = ghorg.GetOrgLogin()

	a := app.NewWithID("com.glow-mdsol.ghOrgToolGUI")

	_, tc, client, err := ghorg.Connect()
	if err != nil {
		log.Fatal(err)
	}

	desk, ok := a.(desktop.App)
	if !ok {
		log.Fatal("system tray is not supported on this platform build")
	}

	bootstrapAutostart(a)

	desk.SetSystemTrayMenu(buildTrayMenu(a, client, tc))

	a.Run()
}

func buildTrayMenu(a fyne.App, client *github.Client, tc *http.Client) *fyne.Menu {
	groups := map[string][]*fyne.MenuItem{}
	var groupOrder []string
	for _, cmd := range allCommands {
		cmd := cmd
		if _, exists := groups[cmd.Group]; !exists {
			groupOrder = append(groupOrder, cmd.Group)
		}
		groups[cmd.Group] = append(groups[cmd.Group], fyne.NewMenuItem(cmd.Label, func() {
			showOutputWindow(a, cmd, client, tc)
		}))
	}

	var items []*fyne.MenuItem
	for _, group := range groupOrder {
		for _, item := range groups[group] {
			items = append(items, item)
		}
		items = append(items, fyne.NewMenuItemSeparator())
	}

	autostartItem := fyne.NewMenuItem("Start at Login", func() {})
	autostartItem.Checked, _ = IsEnabled()
	autostartItem.Action = func() {
		enabled, _ := IsEnabled()
		if enabled {
			if err := Disable(); err != nil {
				log.Printf("Unable to disable autostart: %v", err)
			}
		} else {
			execPath, err := os.Executable()
			if err != nil {
				log.Printf("Unable to determine executable path: %v", err)
				return
			}
			if err := Enable(execPath); err != nil {
				log.Printf("Unable to enable autostart: %v", err)
			}
		}
		autostartItem.Checked, _ = IsEnabled()
	}
	items = append(items, autostartItem)

	items = append(items, fyne.NewMenuItem("Settings", func() { showSettingsWindow(a) }))
	items = append(items, fyne.NewMenuItemSeparator())
	items = append(items, fyne.NewMenuItem("Quit", func() { a.Quit() }))

	return fyne.NewMenu("ghOrgTool", items...)
}

// bootstrapAutostart enables autostart on first launch only, tracked via a
// dedicated config flag so the user's later choice to disable it sticks.
func bootstrapAutostart(a fyne.App) {
	cfg := ghorg.LoadConfig()
	if cfg.AutostartBootstrapped {
		return
	}
	execPath, err := os.Executable()
	if err != nil {
		log.Printf("Unable to determine executable path for autostart bootstrap: %v", err)
		return
	}
	if err := Enable(execPath); err != nil {
		log.Printf("Unable to enable autostart on first launch: %v", err)
	}
	cfg.AutostartBootstrapped = true
	if err := ghorg.SaveConfig(cfg); err != nil {
		log.Printf("Unable to persist autostart bootstrap flag: %v", err)
	}
}
```

Add `"os"` to the import block (used by `os.Executable`).

- [ ] **Step 2: Add the `AutostartBootstrapped` field to `ghorg.Config`**

In `internal/ghorg/config.go`, add a field to the `Config` struct:

```go
type Config struct {
	DefaultTeam            string   `json:"default_team"`
	OrgLogin               string   `json:"org_login,omitempty"`
	GithubToken            string   `json:"github_token,omitempty"`
	AcceptableDomains      []string `json:"acceptable_domains,omitempty"`
	AutostartBootstrapped  bool     `json:"autostart_bootstrapped,omitempty"`
}
```

This is additive and backward-compatible — existing config files without this key parse fine, with the field defaulting to `false`.

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: succeeds

- [ ] **Step 4: Manual verification**

Run: `go run ./cmd/ghOrgToolGUI` (with `GITHUB_AUTH_TOKEN` set and a config file already present from `go run ./cmd/ghOrgTool --init`). Confirm:
- A tray icon appears with grouped menu items for every report and action.
- Clicking a report command opens the shared window with its form.
- Clicking a mutating action shows the confirm dialog before running.
- "Start at Login" is checked after first launch; toggling it off and re-checking confirms the autostart file/registry entry is removed (macOS: `test -f ~/Library/LaunchAgents/com.glow-mdsol.ghOrgToolGUI.plist`; Linux: `test -f ~/.config/autostart/ghOrgToolGUI.desktop`).
- "Settings" opens the settings window from Task 14.
- "Quit" exits cleanly.

- [ ] **Step 5: Run the full test suite matching CI**

Run: `go vet ./... && go test -race -count=1 -coverprofile=coverage.out ./...`
Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/ghOrgToolGUI/main.go internal/ghorg/config.go
git commit -m "feat: wire tray menu to command registry, settings, and login autostart"
```

---

## Task 17: Packaging

**Files:**
- Create: `Makefile`
- Modify: `.github/workflows/test.yml` (Linux GUI build dependencies)

**Interfaces:** none (build tooling only)

- [ ] **Step 1: Add packaging targets**

Create `Makefile`. No custom icon asset exists yet and designing one is out of scope for this plan, so every target omits `-icon`/`-icon` equivalents and lets Fyne fall back to its default icon; swap in a real `-icon path/to/Icon.png` flag on each target once a real icon asset is added in a future task:

```makefile
.PHONY: package-darwin package-windows package-linux package-all

FYNE := go run fyne.io/fyne/v2/cmd/fyne@latest
FYNE_CROSS := go run github.com/fyne-io/fyne-cross@latest

package-darwin:
	cd cmd/ghOrgToolGUI && $(FYNE) package -os darwin

package-windows:
	$(FYNE_CROSS) windows -arch=amd64 -app-id com.glow-mdsol.ghOrgToolGUI ./cmd/ghOrgToolGUI

package-linux:
	$(FYNE_CROSS) linux -arch=amd64 -app-id com.glow-mdsol.ghOrgToolGUI ./cmd/ghOrgToolGUI

package-all: package-darwin package-windows package-linux
```

- [ ] **Step 2: Verify the darwin target runs (on macOS)**

Run: `make package-darwin`
Expected: produces `cmd/ghOrgToolGUI/ghOrgToolGUI.app`

- [ ] **Step 3: Update CI's Linux system dependencies for the GUI build**

In `.github/workflows/test.yml`, the step currently named "Install Linux clipboard dependencies" installs `libx11-dev libxfixes-dev xvfb`. Rename it and add the packages Fyne needs to compile on Linux:

```yaml
      - name: Install Linux GUI/clipboard dependencies
        run: |
          sudo apt-get update
          sudo apt-get install -y libx11-dev libxfixes-dev xvfb libgl1-mesa-dev xorg-dev
```

- [ ] **Step 4: Verify CI still builds green**

Run: `go build ./... && go vet ./... && xvfb-run -a go test -race -count=1 ./...` locally to approximate the updated CI step (full CI verification happens once this is pushed and the workflow runs).

- [ ] **Step 5: Commit**

```bash
git add Makefile .github/workflows/test.yml
git commit -m "build: add fyne packaging targets, extend CI Linux deps for GUI build"
```

---

## Task 18: Documentation

**Files:**
- Modify: `README.md`
- Modify: `CONTRIBUTING.md`

**Interfaces:** none (docs only)

- [ ] **Step 1: Add a GUI section to `README.md`**

After the existing "Usage" section, add:

```markdown
## Desktop GUI

`ghOrgTool` also ships as a tray/menu-bar application with full parity with the CLI commands above.

### Installation

Build the packaged artifact for your platform (see `CONTRIBUTING.md` for the `make package-*` targets), or run it directly from source:

```bash
go run github.com/glow-mdsol/ghOrgTool/cmd/ghOrgToolGUI@latest
```

The app is unsigned. On first launch, macOS Gatekeeper or Windows SmartScreen will warn that the app is from an unidentified developer — this is expected for an internal tool; allow it to run via "Open Anyway" (macOS: System Settings → Privacy & Security) or "More info → Run anyway" (Windows).

### Usage

- The tray icon's menu lists every report and action command, grouped into Reports, Actions, and Settings.
- Clicking a command opens a window with a form for any required input (repo/user/team) and an output pane showing the same report text the CLI prints.
- Mutating actions (add to team, add repo admin, enable Dependabot, reset SSO link) show a confirmation dialog before running.
- "Settings" edits the same config file (`org_login`, `default_team`, `github_token`) the CLI uses.
- The app launches at login by default; toggle "Start at Login" in the tray menu to change that.
```

Then update the CLI installation section to fix the `go install` path (this may already be done from Task 9 — check before duplicating):

```markdown
* Install the tool
  ```
  go install github.com/glow-mdsol/ghOrgTool/cmd/ghOrgTool@latest
  ```
```

- [ ] **Step 2: Update `CONTRIBUTING.md`'s project structure section**

Replace the existing "Project Structure" tree with:

```markdown
## Project Structure

```
ghOrgTool/
├── internal/ghorg/    # business logic: GitHub API calls, config, error-returning (no log.Fatal/os.Exit)
│   ├── repos.go, teams.go, users.go, graphlike.go, config.go, clippy.go, client.go
│   └── *_test.go      # unit tests – mirror the file they test
├── cmd/
│   ├── ghOrgTool/      # CLI: flag parsing, dispatch, text/log output
│   │   ├── main.go
│   │   └── main_test.go
│   └── ghOrgToolGUI/   # Fyne tray app: full CLI parity via a command registry
│       ├── main.go, commands.go, window.go, confirm.go, settings.go, autostart.go
│       └── *_test.go
├── Makefile            # fyne packaging targets (make package-darwin/windows/linux)
└── go.mod / go.sum
```
```

Also update the "Key packages used" table to add:

```markdown
| `fyne.io/fyne/v2` | Cross-platform GUI toolkit + system tray, used by `cmd/ghOrgToolGUI` |
```

And add a short subsection after "Writing tests":

```markdown
### Testing the GUI package

`cmd/ghOrgToolGUI` keeps Fyne widget-construction code (`window.go`, `settings.go`) unit-test-free — it can't run headless in CI without a display. Instead, pure logic used by the GUI (form validation, confirmation gating, the command registry, autostart) is extracted into small functions and tested directly, the same way the rest of the codebase is tested. When adding a new GUI command, add it to `allCommands` in `commands.go` and cover any new pure logic with a test; don't try to unit-test the widget tree itself.
```

- [ ] **Step 3: Commit**

```bash
git add README.md CONTRIBUTING.md
git commit -m "docs: document the desktop GUI, update project structure and install instructions"
```

---

## Plan Self-Review Notes

- **Spec coverage:** Every Goal in the spec has a task — tray menu + full command parity (Tasks 11, 16), shared output window with form + streaming (Task 12), confirmation for mutating commands (Task 13), settings window (Task 14), installable packaging (Task 17), login autostart defaulting on (Tasks 15–16). Non-goals (signing, CI-automated release, E2E UI tests) are respected — no task attempts them.
- **`log.Fatal`/`os.Exit` audit:** every call site found in the original `grep` (Tasks 4–8) is addressed: `users.go` (3 sites in `userPrerequisites`), `teams.go` (3 sites across `isTeam`/`getTeamByName`/`checkAndAddMember` — `isTeam`'s is intentionally left as documented dead code), `main.go`'s `connect()` (3 sites) and flag-validation fatals (those stay in `cmd/ghOrgTool`, which is allowed to be fatal — it's a CLI), and `repos.go`'s `createRepository` (1 site).
- **Known deferred item:** `isTeam` in `teams.go` keeps its `log.Fatal` — it has no live caller in either front end, called out explicitly in Task 5 rather than silently left. Wire it up in a future task only after fixing that.
