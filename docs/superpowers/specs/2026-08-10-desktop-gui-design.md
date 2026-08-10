# Desktop GUI + Tray Widget for ghOrgTool

Date: 2026-08-10
Status: Approved for planning

## Summary

Add an installable desktop application for `ghOrgTool` with a taskbar/menu-bar (system tray) widget, covering macOS, Windows, and Linux. The GUI is a quick-launch panel: the tray menu triggers any of the existing CLI commands, each opening a shared output window with a form for required input and the same report output the CLI produces today. Scope is full parity with the current CLI, including the mutating commands (add to team, add-repo-admin, enable-dependabot, reset SSO link), gated behind confirmation dialogs. The app auto-launches at login.

This is additive: the existing CLI keeps working exactly as it does today. No existing CLI flag, output format, or config file format changes.

## Goals

- A tray icon on macOS/Windows/Linux with one menu entry per existing CLI command.
- Clicking a command opens/focuses a window: a form for required input (repo/user/team), a confirmation step for mutating commands, and a scrollable output pane showing the same report text the CLI prints today.
- Long-running org-wide scans (`--admin-user-report`) stream output live instead of showing nothing until completion.
- A Settings window for org login / default team / token, backed by the existing config file.
- The app is installable per platform (`.app`/`.dmg`, `.exe` installer, `.AppImage`/`.deb`) and auto-launches at login.

## Non-goals (V1 scope boundaries)

- Code signing / notarization. Unsigned builds will trigger Gatekeeper (macOS) and SmartScreen (Windows) warnings on first run — accepted as a known V1 tradeoff, not something this design solves.
- CI-automated release packaging. Packaging is a documented local command a maintainer runs; wiring it into CI/CD is a fast-follow, not blocking.
- Full end-to-end UI test automation.
- Any change to CLI behavior, flags, or output format.
- A "start at login" toggle default of *off* — V1 defaults it to *on*, per the earlier design decision; a user can disable it from the tray menu.

## Current-state findings that shape this design

- `repos.go` already separates **data-fetching** (`find*`/`get*`/`list*` functions returning structs + `error`), **formatting** (`format*Report` functions returning a `string`), and **printing** (`report*` wrapper functions that call `fmt.Println`/`log.Printf`). This split means the GUI can reuse the data-fetch + format functions directly and skip the print wrappers.
- `main.go`, `users.go`, and `teams.go` use `log.Fatal` for error handling in several places (token resolution in `connect()`, user-prerequisite checks, team lookups, flag validation). `log.Fatal` calls `os.Exit`, which is fatal to a long-running GUI/tray process — every one of these must become a returned `error` for code shared with the GUI.
- `--admin-user-report`'s all-org scan (`streamOrgAdminRepoAccessReport`) already writes to an `io.Writer` incrementally rather than building one giant string — this is reused as-is for live streaming in the GUI output pane.
- `initConfig()`/`rotateToken()` in `config.go` are interactive terminal prompts (`bufio.Scanner`) — these stay CLI-only; the GUI gets its own Settings form calling the same underlying `loadConfig`/`saveConfig`.
- `clippy.go`'s `prompt()` (print + clipboard copy) is a terminal-oriented helper and stays CLI-only; the GUI wires its own "Copy" button using the same `golang.design/x/clipboard` package directly.
- Go does not allow two `main` packages at a module root, so introducing a second binary requires moving to a `cmd/` layout. This changes the public `go install github.com/glow-mdsol/ghOrgTool@latest` command to `.../cmd/ghOrgTool@latest` — a user-facing change that needs a README update and is called out explicitly rather than treated as incidental.

## Architecture

```
ghOrgTool/
├── internal/ghorg/       # business logic, moved from repo root, log.Fatal removed
│   ├── repos.go, teams.go, users.go, graphlike.go, config.go, clippy.go
│   └── *_test.go         # existing tests, moved unchanged
├── cmd/
│   ├── ghOrgTool/         # existing CLI: flag parsing, dispatch, text/log output
│   │   ├── main.go
│   │   └── main_test.go
│   └── ghOrgToolGUI/      # new: Fyne tray app
│       ├── main.go        # entry point, tray menu setup, window lifecycle
│       ├── commands.go    # command registry: form fields, handler, mutating flag
│       ├── window.go      # shared output window + form rendering
│       ├── confirm.go     # confirmation dialog for mutating commands
│       ├── settings.go    # settings window (org/team/token), uses ghorg config funcs
│       └── autostart.go   # login-item registration, runtime.GOOS switch
├── go.mod / go.sum
```

### `internal/ghorg` (library layer)

Today's root-level `.go` files move here with minimal logic changes:

- Every `log.Fatal(...)` becomes `return err` (or `return zeroValue, err` where the function didn't previously return an error). Call sites in `cmd/ghOrgTool/main.go` wrap these in `log.Fatal` themselves, preserving today's CLI behavior exactly.
- `connect()` (token resolution + client setup) becomes `Connect() (context.Context, *http.Client, *github.Client, error)` instead of calling `log.Fatal` on a missing token.
- Data-fetch/format functions in `repos.go` (`findOrgAdminRepoAccess`, `formatOrgAdminRepoAccessReport`, etc.) are unchanged in shape — just exported and moved.
- No behavior change to what gets printed by the CLI; this is a pure refactor of error-handling plumbing plus a package move.

### `cmd/ghOrgTool` (CLI layer)

Unchanged behavior. `main()` keeps its flag parsing, `getopt` aliases, and dispatch switch, but every call into former root-level functions now goes through `ghorg.XxxFunc(...)` and checks the returned `error`, calling `log.Fatal`/`log.Printf` itself exactly where the old code did.

### `cmd/ghOrgToolGUI` (GUI layer)

Built with **Fyne** (`fyne.io/fyne/v2`), using its system tray support for the icon/menu.

- **Tray menu**: one item per CLI command, grouped into Reports (admin-user-report, recent-admin-grants, recent-admin-removals, list-actions-storage, list-repo-collaborators, user-repo-access, find-common-teams, describe-team, user check) and Actions (add to team, add-repo-admin, enable-dependabot, reset SSO link), plus Settings and Quit.
- **Command registry** (`commands.go`): a table keyed by command, holding the input fields it needs (repo/user/team/flags), a reference to the `ghorg` function(s) to call, and an `isMutating bool` used to gate the confirm dialog.
- **Output window**: a single shared `fyne.Window`, reused across commands (opened/focused, not recreated). Contains the command's input form at the top and a scrollable monospace text widget below for output.
  - Non-streaming commands: call the `ghorg` function on a background goroutine, then set the text widget's content to the `format*Report` string (or an error banner) on the UI thread.
  - The one streaming command (`--admin-user-report`): pass a custom `io.Writer` whose `Write` appends to the text widget on the UI thread, giving live progress for long org-wide scans.
- **Confirm dialog** (`confirm.go`): for the four mutating commands, `dialog.ShowConfirm` states the concrete action (e.g. "Add alice, bob as admin collaborators to example-org/repo-x?") before the call fires.
- **Settings window** (`settings.go`): form for org login / default team / token, reading and writing through `ghorg.LoadConfig`/`ghorg.SaveConfig` directly — no terminal prompt reuse.
- **Autostart** (`autostart.go`): `Enable() error`, `Disable() error`, `IsEnabled() (bool, error)`, dispatched via `runtime.GOOS` (matching the existing style in `config.go`'s `getConfigDir`, rather than introducing build-tag files):
  - macOS: write/remove a LaunchAgent plist under `~/Library/LaunchAgents`, `launchctl load`/`unload` it.
  - Windows: write/remove a value under `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`.
  - Linux: write/remove a `.desktop` file under `~/.config/autostart/`.
  - Wired to a checked "Start at Login" tray menu item; `Enable()` is called once on first launch (detected via a flag in the config file) so it defaults on, and the user can toggle it off from the tray menu afterward.

## Data flow

```
Tray click on "Admin User Report"
  → commands.go looks up the registry entry
  → window.go opens/focuses the shared output window, renders the command's input form
  → user fills in optional user filter, submits
  → [if isMutating] confirm.go shows dialog.ShowConfirm; user confirms
  → goroutine calls ghorg.FindOrgAdminRepoAccess / ghorg.StreamOrgAdminRepoAccessReport(..., writer)
  → writer.Write appends chunks to the output text widget (fyne.Do to marshal onto UI thread)
  → on error, an inline red banner replaces/prepends the output pane instead of the app crashing
```

## Error handling

- `internal/ghorg` never calls `log.Fatal` or `os.Exit`; every failure path returns an `error`.
- `cmd/ghOrgTool` preserves today's exact CLI error behavior (fatal on required-input errors, printed-and-continue on per-item errors within a loop) by checking returned errors at the same call sites `log.Fatal`/`log.Printf` occur today.
- `cmd/ghOrgToolGUI` never calls `log.Fatal`/`os.Exit` either; all errors surface as an inline banner in the output window (or, for form-validation errors like a missing `--repo` equivalent, an inline field error before any call is made).

## Testing

- All existing `*_test.go` files move to `internal/ghorg` unchanged; same `httptest.Server`-based patterns via `newTestClient`, same `t.Setenv("XDG_CONFIG_HOME", ...)` sandboxing, same coverage gate in CI.
- `cmd/ghOrgTool` keeps its existing dispatch-level tests, now exercising the thin wrapper plus `ghorg` error propagation.
- `cmd/ghOrgToolGUI` gets unit tests, using `fyne.io/fyne/v2/test`'s headless test app, for the pure/testable parts only:
  - command registry → form field mapping
  - confirm-gating logic (which commands require confirmation, and that the confirm text matches the resolved inputs)
  - autostart `Enable`/`Disable`/`IsEnabled` round-trip, sandboxed the same way config tests are today (temp `HOME`/`XDG_CONFIG_HOME`, or an injected file path)
  - No full end-to-end UI automation (button clicks driving real window rendering) — out of scope per YAGNI.

## Packaging / installation

- `fyne package` (single-platform) and `fyne-cross` (cross-compilation) build installable artifacts from `cmd/ghOrgToolGUI`:
  - macOS: `.app` bundle, optionally wrapped in a `.dmg`.
  - Windows: `.exe`, packaged with an NSIS-based installer via `fyne-cross`.
  - Linux: `.AppImage` and/or `.deb`.
- This is a documented, manually-run maintainer command (e.g. a `Makefile` target or a script in `CONTRIBUTING.md`), not a CI release pipeline — CI automation is an explicit non-goal for V1.
- Builds are unsigned. macOS Gatekeeper and Windows SmartScreen will show an "unidentified developer" warning on first run; users bypass this the same way as any other unsigned internal tool. Signing/notarization is out of scope.

## Documentation impact

- `README.md`: update the `go install` instructions (`.../cmd/ghOrgTool@latest`), add a GUI installation section (download/build the packaged artifact, first-run Gatekeeper/SmartScreen note, tray menu overview, Settings window).
- `CONTRIBUTING.md`: update the project-structure table to reflect `internal/ghorg` + `cmd/ghOrgTool` + `cmd/ghOrgToolGUI`, add the packaging command, note the `fyne.io/fyne/v2/test` pattern for GUI-layer tests.
- `CLAUDE.md`: update after implementation to reflect the new package layout (out of scope for this spec — handled as a follow-up doc update once the refactor lands).
