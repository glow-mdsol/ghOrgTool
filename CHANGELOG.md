# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Publish workflow (`.github/workflows/publish.yml`): pushing a `v*.*.*` tag verifies it matches `version` in `main.go`, builds Linux, macOS (arm64) and Windows binaries, and creates a GitHub release with the changelog notes.

### Changed

- `CLAUDE.md` is now `AGENTS.md`, a tool-neutral guide for coding agents and contributors, with release directives.
- `CONTRIBUTING.md` documents the release process.

## [1.1.0] - 2026-11-27

### Added

- `-C, --find-common-teams-clipboard`: finds teams with access to all repositories named in the clipboard text. Repository names are extracted from whitespace/list-separated text, GitHub URLs are unwrapped, and names are checked against the organization before the common-team report runs.

### Changed

- `-c, --find-common-teams` ignores duplicate repository names, so each repository is counted once.

## [1.0.0] - 2026-10-13

### Added

- Versioned CLI with `-v, --version`.
