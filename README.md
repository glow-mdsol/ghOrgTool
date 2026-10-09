# ghOrgTool

A silly little app to streamline the process of checking a users account for the correct setup and adding if ok.  

It will run the following checks on the user account:
* requires a public email address
* requires 2-FA enabled
* requires Name set on account 

It will use the SSO connection to link a user email to an account, but the user will need to be in the SSO context


## Installation
* Install go
  ```
  brew install go
  ```
* Install the tool
  ```
  go install github.com/glow-mdsol/ghOrgTool@latest
  ```
* Add your GOBIN path to your path, by adding the following to your `~/.zshrc` or `~/.bashrc`
  ```
  export PATH="~/go/bin:$PATH"
  ```

## Configuration

### Authentication
The app requires a GitHub Token with User and Org permissions. The token is loaded in the following priority order:

1. **Environment Variable**: `GITHUB_AUTH_TOKEN`
2. **Configuration File**: `github_token` field in the config file (see below)
3. **`.netrc` File**: looks for a machine record for `api.github.com` in your [.netrc](https://www.gnu.org/software/inetutils/manual/html_node/The-_002enetrc-file.html) file

### User Configuration File
You can customize settings by creating a configuration file. The tool will automatically look for a config file in the following locations based on your operating system:

* **macOS/Linux**: `~/.config/ghOrgTool/config.json`
* **Windows**: `%APPDATA%\ghOrgTool\config.json`

#### Quick Setup with `--init`

The easiest way to create a configuration file is to use the interactive initialization command:

```bash
ghOrgTool --init
```

This will:
- Prompt you for the organization login
- Prompt you for your default team name
- Optionally prompt for your GitHub personal access token
- Create the config file in the correct location for your OS
- Set appropriate file permissions (Unix/macOS only)

**Example:**
```bash
$ ghOrgTool --init
Configuration Initialization
============================

Enter organization login [example-org]: sandbox-org

Enter default team name [Default Team]: Engineering Team

Enter GitHub personal access token (optional):
  Leave empty to use GITHUB_AUTH_TOKEN environment variable or .netrc
Token: ghp_abc123xyz456

✓ Configuration saved to: /Users/username/.config/ghOrgTool/config.json
✓ File permissions set to 600 (user read/write only)

Configuration summary:
  Organization Login: sandbox-org
  Default Team: Engineering Team
  GitHub Token: ***configured***
```

#### Rotating/Updating Your GitHub Token

If you need to update or rotate your GitHub token (e.g., for security reasons or token expiration), use the `--rotate-token` command:

```bash
ghOrgTool --rotate-token
```

This will:
- Preserve your existing configuration settings (like default team)
- Prompt you for a new GitHub token
- Update only the token in the configuration file
- Maintain secure file permissions

**Example:**
```bash
$ ghOrgTool --rotate-token
Rotate GitHub Token
===================

Current configuration has a token set.

Enter new GitHub personal access token:
  Leave empty to remove the token from config
New Token: ghp_new_token_xyz789

✓ Token updated in: /Users/username/.config/ghOrgTool/config.json
✓ File permissions verified (600)

✓ GitHub Token: ***configured***
```

**Note:** You can also remove the token from your configuration by leaving the input empty when prompted. This is useful if you want to switch to using an environment variable or .netrc file instead.

#### Manual Configuration

The configuration file should be in JSON format and supports the following options:

```json
{
  "org_login": "your-org-login",
  "default_team": "Your Team Name",
  "github_token": "ghp_your_github_token_here"
}
```

**Configuration Options:**
- `org_login`: The GitHub organization login used by org-level operations (defaults to "example-org" if not specified)
- `default_team`: The default team name to use when adding users (defaults to "Default Team" if not specified)
- `github_token`: Your GitHub personal access token (optional, only if not using environment variable or .netrc)

**Example Configuration:**
```json
{
  "org_login": "sandbox-org",
  "default_team": "Engineering Team",
  "github_token": "ghp_abc123xyz456"
}
```

**Minimal Configuration (org + team):**
```json
{
  "org_login": "sandbox-org",
  "default_team": "Engineering Team"
}
```

**Security Note:** If you store your GitHub token in the config file, make sure the file has appropriate permissions to prevent unauthorized access.

#### Manual File Creation

If you prefer to create the configuration file manually instead of using `--init`:

On macOS/Linux:
```bash
mkdir -p ~/.config/ghOrgTool
cat > ~/.config/ghOrgTool/config.json << 'EOF'
{
  "org_login": "your-org-login",
  "default_team": "Your Team Name",
  "github_token": "ghp_your_token_here"
}
EOF
chmod 600 ~/.config/ghOrgTool/config.json
```

On Windows (PowerShell):
```powershell
New-Item -ItemType Directory -Force -Path "$env:APPDATA\ghOrgTool"
Set-Content -Path "$env:APPDATA\ghOrgTool\config.json" -Value @'
{
  "org_login": "your-org-login",
  "default_team": "Your Team Name",
  "github_token": "ghp_your_token_here"
}
'@
```


## Usage
Usage of the tool is pretty simple
  ```shell
  Usage is: ghOrgTool <options> <logins or repository names>
  where options are:
  -a, --add
        Add users to a team (use with --team)
  -A, --add-repo-admin
        Add user as admin collaborator to repository (requires --repo)
    -B, --enable-dependabot
      Enable Dependabot alerts and security updates for one or more repositories
    -P, --dependabot-grouped-prs
      Optional with --enable-dependabot: verify grouped PR config and print PR-ready guidance (no direct commits)
    -U, --admin-user-report
      Report repositories where users have effective admin access via teams or direct collaborator links (optionally filtered to one user)
  -c, --find-common-teams
        Find teams that have access to ALL specified repositories
  -d, --describe-team
        Show detailed summary of a team (use with --team)
  -h, --help
        Print help
  -L, --list-repo-collaborators
        List collaborators on repository with permissions and added dates (requires --repo)
  -S, --list-actions-storage
      List top 10 repositories by Actions cache usage and org billable constrained storage
  -G, --recent-admin-grants
      List users granted admin access to any org repo in the last 24h that still have that access
    -M, --recent-admin-removals
      List users recently removed from admin access on any org repo (same lookback window as grants)
  -R, --repo string
        Repository name for repo operations
    --checkpoint-file string
      Persist the last completed repo for deterministic admin-user-report restarts
    --resume-after-repo string
      Resume all-repos access reporting after the named repository
  -r, --reset
        Generate the Reset link
  -s, --team string
        Specified Team (default "Default Team")
  -u, --user-repo-access
        Report a user's effective access to a repository via team membership (requires --repo)
  
  Note: Without any flags, the tool lists teams for the specified user or repository.
  ```

Enable Dependabot for one repository:
```shell
ghOrgTool --enable-dependabot --repo my-repo
```

Enable Dependabot for multiple repositories:
```shell
ghOrgTool --enable-dependabot repo1 repo2 repo3
```

Optionally check grouped PR configuration (off by default):
```shell
ghOrgTool --enable-dependabot --dependabot-grouped-prs --repo my-repo
```

### Tools

#### User account check
The tool can take a repository name, a user name or a user email (which can only be looked up via the SSO)

In the case of a User we run some tests:
    ```shell
    $ ghOrgTool someuser
    
    2026/02/02 16:20:21 Using provided login: someuser
    2026/02/02 16:20:21 Processing someuser
    2026/02/02 16:20:21 Validated Pre-requisites for someuser GitHub Email: someuser@email.com
    ```
It will run the following validation checks:
* User has a public email address
* User has a name
* User is a member of the Organisation
* User has 2FA enabled (we don't check for insecure 2fa at the moment)

Once the checks are complete it will list the teams a user has access to
  ```shell
  $ ghOrgTool someuser
  2022/05/16 11:55:52 Validated Pre-requisites for someuser GitHub Email: someuser@somedomain.com
  2022/05/16 11:55:52 User someuser is a admin of example-org
  2022/05/16 11:55:53 User someuser is a member of the following teams
  2022/05/16 11:55:53 * Team Alpha (https://github.com/orgs/ORG/teams/team-alpha)
  2022/05/16 11:55:53 * Team Bravo (https://github.com/orgs/ORG/teams/team-bravo)
  ...
  ```
If the argument is a repository, list the teams that have access to a repository (and what level of access they have)
  ```shell
  $ ghOrgTool somerepo
  2022/05/16 11:55:53 Repository somerepo has the following teams with access:
  2022/05/16 11:55:53 * Team Alpha (https://github.com/orgs/ORG/teams/team-alpha) pull
  2022/05/16 11:55:53 * Team Bravo (https://github.com/orgs/ORG/teams/team-bravo) push
  2022/05/16 11:55:53 * Team Yankee (https://github.com/orgs/ORG/teams/team-yankee) admin
  ...
  ```

#### User Repository Access Report
Report a user's effective (highest) permission level on a specific repository, broken down by which teams grant that access.

Accepts a username or email address. Use `--repo` to specify the target repository.
  ```shell
  $ ghOrgTool --user-repo-access --repo somerepo someuser
  Access report: someuser → example-org/somerepo

  Effective permission: write

#### Admin User Report Across All Repositories
Report every repository where users have effective `admin` access across the organization via teams or direct collaborator links.

The scan lists org repositories once, checks them in parallel with a bounded worker pool, and flushes CSV rows in repository order so restart boundaries stay deterministic.

```shell
$ ghOrgTool --admin-user-report
Admin access report (all users) across example-org repositories

1. alice -> example-org/repo-a
   Via admin teams:
     - Platform Admins (https://github.com/orgs/example-org/teams/team-admin): admin

2. bob -> example-org/repo-a
   Via admin teams:
     - Platform Admins (https://github.com/orgs/example-org/teams/team-admin): admin

Processed 214 repositories with no API skips.
```

You can still filter to a specific user:

```shell
$ ghOrgTool --admin-user-report someuser
```

If a long scan is interrupted or some repositories fail transiently, resume after the last completed repository name:

```shell
$ ghOrgTool --admin-user-report --resume-after-repo repo-a
```

For repeatable restarts, use a checkpoint file. The command updates it after each repo is completely flushed to CSV, so you can rerun with the same checkpoint file and append the remaining rows.

```shell
$ ghOrgTool --admin-user-report --checkpoint-file admin-user-report.checkpoint > admin-users.csv
$ ghOrgTool --admin-user-report --checkpoint-file admin-user-report.checkpoint >> admin-users.csv
```

#### Recent Admin Access Removals
List users who had admin access removed recently, using the same lookback window as `--recent-admin-grants` (48 hours by default, 72 hours on Mondays).

```shell
$ ghOrgTool --recent-admin-removals
Users recently removed from admin access in example-org:

1. alice ← sample-orchestrator
  Removed by: org-admin
  Removed: 2026-04-15 10:00:00 UTC
  Current access: none
```

  Via teams:
    - Team Alpha (https://github.com/orgs/ORG/teams/team-alpha): read
    - Team Bravo (https://github.com/orgs/ORG/teams/team-bravo): write
  ```

Also works with email:
  ```shell
  $ ghOrgTool --user-repo-access --repo somerepo someuser@somedomain.com
  ```

#### GitHub Actions Storage Report
List the top 10 repositories in the organization by GitHub Actions cache usage and include org-level billable constrained Actions storage.

```shell
$ ghOrgTool --list-actions-storage
Org Actions shared storage billing summary:
  Billable constrained storage: 42 GB-month
  Estimated total shared storage: 64 GB-month
  Days left in billing cycle: 12

Top 10 repositories by GitHub Actions cache storage in example-org:

 1. example-org/repo-a                               3.00 GiB  4 active caches
 2. example-org/repo-b                             850.0 MiB  2 active caches
 3. example-org/repo-c                              12.0 KiB  1 active caches
```

This report uses the organization-level Actions cache usage endpoint, so the GitHub token must have at least `read:org` scope.

#### Recent Admin Grants
Scan all repositories in the organization for users who were granted direct admin collaborator access in the last 24 hours and who still hold that access. For each match the settings/access URL is printed so you can review or revoke the grant immediately.

```shell
$ ghOrgTool --recent-admin-grants
Users granted admin access in the last 24 hours (still active) in example-org:

1. alice → sample-orchestrator
  Granted by: org-admin
   Granted: 2026-04-15 09:12:00 UTC
   Access settings: https://github.com/example-org/sample-orchestrator/settings/access

2. bob → sample-api
  Granted by: repo-owner
   Granted: 2026-04-15 14:05:00 UTC
   Access settings: https://github.com/example-org/sample-api/settings/access
```

This command queries the organization audit log and validates current permission on matched users/repositories. The GitHub token must have `repo` and `read:org` scopes, and must be able to read org audit logs.

#### Reset Invite 
This is a wrapper for removing the SSO connection for a user (for when SSO doesn't link correctly)

This can be done using username; 
  ```shell
  $ ghOrgTool --reset someuser 
  2022/05/16 12:02:04 Reset Link: https://github.com/orgs/ORG/people/someuser/sso
  ```
Or, via email
  ```shell
  $ ghOrgTool --reset someuser@somedomain.com 
  2022/05/16 12:02:04 Reset Link: https://github.com/orgs/ORG/people/someuser/sso
  ```

#### Team Matching
Find teams that match a set of requested repos (or teans that are a close match)

##### Case 1: Exact and Close Matches Found
```
Analyzing team access patterns for 5 repositories...
Repositories: [repo1 repo2 repo3 repo4 repo5]

Repository repo1 has 4 teams with access
Repository repo2 has 3 teams with access  
Repository repo3 has 5 teams with access
Repository repo4 has 2 teams with access
Repository repo5 has 4 teams with access

🎯 EXACT MATCHES - Teams with access to ALL 5 repositories:

1. Team: DevOps Team
   Slug: devops-team
   Description: Infrastructure and deployment team
   Access Level: admin
   URL: https://github.com/orgs/example-org/teams/devops-team
   Coverage: 100% (5/5 repositories)

🔍 CLOSE MATCHES - Teams with access to more than half of the repositories:

1. Team: Security Team
   Slug: security-team
   Description: Application security team
   Access Level: maintain
   URL: https://github.com/orgs/example-org/teams/security-team
   Coverage: 80.0% (4/5 repositories)
   Missing access to: [repo2]

2. Team: Frontend Team
   Slug: frontend-team
   Description: UI/UX development team
   Access Level: push
   URL: https://github.com/orgs/example-org/teams/frontend-team
   Coverage: 60.0% (3/5 repositories)
   Missing access to: [repo4, repo5]

📊 SUMMARY: Found 1 exact matches and 2 close matches.
```

##### Case 2: No Exact Matches, Some Close Matches
```
Analyzing team access patterns for 4 repositories...
Repositories: [repo1 repo2 repo3 repo4]

Repository repo1 has 3 teams with access
Repository repo2 has 2 teams with access
Repository repo3 has 4 teams with access
Repository repo4 has 3 teams with access

🎯 EXACT MATCHES: No teams found with access to ALL repositories.

🔍 CLOSE MATCHES - Teams with access to more than half of the repositories:

1. Team: Backend Team
   Slug: backend-team
   Description: Server-side development team
   Access Level: maintain
   URL: https://github.com/orgs/example-org/teams/backend-team
   Coverage: 75.0% (3/4 repositories)
   Missing access to: [repo2]

📊 SUMMARY: Found 0 exact matches and 1 close matches.
```

##### Case 3: No Matches Found
```
Analyzing team access patterns for 3 repositories...
Repositories: [repo1 repo2 repo3]

Repository repo1 has 2 teams with access
Repository repo2 has 2 teams with access
Repository repo3 has 3 teams with access

🎯 EXACT MATCHES: No teams found with access to ALL repositories.

🔍 CLOSE MATCHES: No teams found with access to more than 1 repositories.

📊 SUMMARY: No teams found with significant access coverage.
To find teams with access to individual repositories, use the --teams flag with each repository name.
```
