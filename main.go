package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/google/go-github/v84/github"
	"github.com/jdxcode/netrc"
	"golang.org/x/oauth2"
	"rsc.io/getopt"
)

// Default values
const DefaultOrgLogin = "example-org"
const DefaultTeamName = "Default Team"
const TokenEnvVar = "GITHUB_AUTH_TOKEN"

var DefaultAcceptableDomains = []string{"example.com", "example.org", "example.net"}

// ORG is a runtime-resolved organization login loaded from config.
var ORG = DefaultOrgLogin

// Helper function
func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

// creates the initial contact with GitHub - uses the users netrc to get the
// token
func connect() (context.Context, *http.Client, *github.Client) {
	usr, err := user.Current()
	if err != nil {
		log.Fatal("Unable to get User")
	}
	var token string
	// Priority: 1. Environment variable, 2. Config file, 3. .netrc file

	// 1. Check the environment variable
	token = os.Getenv(TokenEnvVar)

	// 2. Check the config file
	if token == "" {
		token = getGithubToken()
	}

	// 3. Check .netrc file
	if token == "" {
		n, err := netrc.Parse(filepath.Join(usr.HomeDir, ".netrc"))
		if err != nil {
			log.Fatal("Unable to load token")
		}
		token = n.Machine("api.github.com").Get("password")
	}

	if token == "" {
		log.Fatal("Unable to find a token for access")
	}
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)

	client := github.NewClient(tc)
	return ctx, tc, client
}

// entityType represents the type of GitHub entity
type entityType int

const (
	entityUnknown entityType = iota
	entityUser
	entityRepository
)

// detectEntityType determines whether an entity slug is a repository or user
// Returns the entity type and resolved login (for users) or repo name (for repos)
func detectEntityType(ctx context.Context, client *github.Client, tc *http.Client, entitySlug string) (entityType, string) {
	// If it contains @, it's definitely a user email, not a repo
	if strings.Contains(entitySlug, "@") {
		login, err := resolveLogin(ctx, tc, &entitySlug)
		if err != nil || login == "" {
			return entityUnknown, ""
		}
		return entityUser, login
	}

	// First check if it's a valid repository in the org
	// Repositories in the org get priority over usernames
	if isRepository(ctx, client, ORG, entitySlug) {
		return entityRepository, entitySlug
	}

	// Then check if it's a valid user
	if isUser(ctx, client, &entitySlug) {
		// Additional check: verify user is a member of the org
		// This prevents treating random GitHub users as valid entities
		_, resp, err := client.Organizations.GetOrgMembership(ctx, entitySlug, ORG)
		if err == nil && resp.StatusCode == 200 {
			return entityUser, entitySlug
		}
		// User exists but is not a member of the org - treat as unknown
		log.Printf("User %s exists but is not a member of organization %s", entitySlug, ORG)
	}

	return entityUnknown, ""
}

func userIsValid(ctx context.Context, client *github.Client, tc *http.Client, userLogin string) (bool, *github.User) {
	ghUser := userPrerequisites(ctx, client, &userLogin)
	// check membership of org
	result, code := meetsOrgPrequisites(ctx, client, ghUser)
	if !result && code == 1 {
		if code == 1 {
			prompt(fmt.Sprintf("User %s is not a member of organisation %s", *ghUser.Login, ORG))
			log.Println("User ", *ghUser.Login, " is not a member of organization ", ORG)
		} else {
			log.Printf("Unable to determine organization membership")
		}
		return false, ghUser
	}
	// check SSO requirements
	result, _ = meetsSSOPrequisites(ctx, tc, ghUser)
	if !result {
		prompt(
			fmt.Sprintf("User %s is not SSO Enabled", *ghUser.Login),
		)
		log.Printf("User %s is not SSO enabled", *ghUser.Login)
		return false, ghUser
	}
	// check 2FA is enabled
	result, code = meets2FAPrerequisites(ctx, client, ghUser)
	if !result {
		prompt(fmt.Sprintf("User %s does not have 2FA enabled", *ghUser.Login))
		log.Printf("User %s does not have 2FA enabled", *ghUser.Login)
		return false, ghUser
	}
	return true, ghUser
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

	// Slack often wraps pasted links as <url|label> or <url>.
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

// Go time!
func main() {
	ORG = getOrgLogin()
	defaultTeam := getDefaultTeam()
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
		if err := initConfig(); err != nil {
			log.Fatalf("Configuration initialization failed: %v", err)
		}
		os.Exit(0)
	}

	if *rotateTokenFlag {
		if err := rotateToken(); err != nil {
			log.Fatalf("Token rotation failed: %v", err)
		}
		os.Exit(0)
	}

	if *help {
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
		os.Exit(0)
	}
	var userOrRepoList = flag.Args()
	for i, arg := range userOrRepoList {
		userOrRepoList[i] = normalizeEntityArgument(arg)
	}
	*repoName = normalizeEntityArgument(*repoName)

	// create a connection
	ctx, tc, client := connect()

	if *describeTeam {
		// Describe a team with detailed summary
		team := getTeamByName(ctx, client, ORG, *teamName)
		log.Printf("Got team '%s' for '%s'", *team.Name, *teamName)
		summary := summarizeTeam(ctx, client, team)
		fmt.Println(summary)
		return
	}

	if *listRepoCollaborators {
		// List collaborators on repository
		if *repoName == "" {
			log.Fatal("--repo flag is required when using --list-repo-collaborators")
		}
		if !isRepository(ctx, client, ORG, *repoName) {
			log.Fatalf("Repository '%s' not found in organization '%s'", *repoName, ORG)
		}

		err := listRepositoryCollaborators(ctx, client, ORG, *repoName)
		if err != nil {
			log.Printf("Error listing collaborators for repository %s: %s", *repoName, err)
		}
		return
	}

	if *listActionsStorage {
		if err := listTopActionsCacheUsageByRepo(ctx, client, ORG, 10); err != nil {
			log.Printf("Error listing GitHub Actions storage report: %s", err)
		}
		return
	}

	if *recentAdminGrants {
		if err := listRecentAdminGrants(ctx, client, ORG); err != nil {
			log.Printf("Error listing recent admin grants: %s", err)
		}
		return
	}

	if *recentAdminRemovals {
		if err := listRecentAdminRemovals(ctx, client, ORG); err != nil {
			log.Printf("Error listing recent admin removals: %s", err)
		}
		return
	}

	if *addRepoAdmin {
		// Add user as admin collaborator to repository
		if *repoName == "" {
			log.Fatal("--repo flag is required when using --add-repo-admin")
		}
		if !isRepository(ctx, client, ORG, *repoName) {
			log.Fatalf("Repository '%s' not found in organization '%s'", *repoName, ORG)
		}
		if len(userOrRepoList) == 0 {
			log.Fatal("At least one username or email is required")
		}

		for _, entitySlug := range userOrRepoList {
			if entitySlug == "" {
				continue
			}

			// Resolve email to login if needed
			login, err := resolveLogin(ctx, tc, &entitySlug)
			if err != nil {
				log.Printf("Unable to resolve %s: %s", entitySlug, err)
				continue
			}
			if login == "" {
				continue
			}

			// Check if user exists
			if !isUser(ctx, client, &login) {
				log.Printf("User %s not found", login)
				continue
			}

			// Add user as admin collaborator
			err = addUserAsRepoCollaborator(ctx, client, ORG, *repoName, login)
			if err != nil {
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

			if !isRepository(ctx, client, ORG, repo) {
				log.Printf("Warning: repository '%s' not found in organization '%s', skipping", repo, ORG)
				continue
			}

			result, err := enableDependabot(ctx, client, ORG, repo)
			if err != nil {
				log.Printf("Error enabling Dependabot for repository %s: %s", repo, err)
				continue
			}

			validTargets++
			switch {
			case result.vulnerabilityAlertsEnabled && result.automatedFixesEnabled:
				log.Printf("Enabled Dependabot alerts and security updates for repository %s", repo)
			case result.vulnerabilityAlertsEnabled:
				log.Printf("Enabled Dependabot alerts for repository %s (security updates were already enabled)", repo)
			case result.automatedFixesEnabled:
				log.Printf("Enabled Dependabot security updates for repository %s (alerts were already enabled)", repo)
			default:
				log.Printf("Dependabot is already fully enabled for repository %s", repo)
			}

			if *dependabotGroupedPRsFlag {
				groupedConfigured, groupedErr := hasDependabotGroupedPRs(ctx, client, ORG, repo)
				if groupedErr != nil {
					log.Printf("Unable to check grouped Dependabot PR config for repository %s: %s", repo, groupedErr)
					continue
				}
				if groupedConfigured {
					log.Printf("Grouped Dependabot PRs are already configured for repository %s", repo)
				} else {
					log.Printf("Grouped Dependabot PRs requested for repository %s, but %s is missing or ungrouped", repo, dependabotConfigPath)
					log.Printf("Direct commits are disabled. Open a pull request in %s/%s with this content in %s:", ORG, repo, dependabotConfigPath)
					fmt.Println(defaultDependabotGroupedPRTemplate())
				}
			}
		}

		if validTargets == 0 {
			log.Fatal("No valid repositories found for --enable-dependabot")
		}
		return
	}

	if *userRepoAccess {
		// Report a user's effective access to a repository via their team memberships
		if *repoName == "" {
			log.Fatal("--repo flag is required when using --user-repo-access")
		}
		if !isRepository(ctx, client, ORG, *repoName) {
			log.Fatalf("Repository '%s' not found in organization '%s'", *repoName, ORG)
		}
		if len(userOrRepoList) == 0 {
			log.Fatal("A username or email is required when using --user-repo-access")
		}
		userSlug := userOrRepoList[0]
		login, err := resolveLogin(ctx, tc, &userSlug)
		if err != nil || login == "" {
			log.Fatalf("Unable to resolve user '%s'", userSlug)
		}
		if err := reportUserRepoAccess(ctx, client, tc, ORG, login, *repoName); err != nil {
			log.Printf("Error generating access report: %s", err)
		}
		return
	}

	if *adminUserReport {
		if len(userOrRepoList) == 0 {
			if err := reportOrgAdminRepoAccess(ctx, client, ORG, *resumeAfterRepo, *checkpointFile); err != nil {
				log.Printf("Error generating org-wide admin access report: %s", err)
			}
			return
		}
		userSlug := userOrRepoList[0]
		login, err := resolveLogin(ctx, tc, &userSlug)
		if err != nil || login == "" {
			log.Fatalf("Unable to resolve user '%s'", userSlug)
		}
		if err := reportUserAdminRepoAccess(ctx, client, tc, ORG, login, *resumeAfterRepo); err != nil {
			log.Printf("Error generating admin access report: %s", err)
		}
		return
	}

	if *findCommonTeams {
		// All arguments should be repository names
		var repoNames []string
		for _, entitySlug := range userOrRepoList {
			if entitySlug == "" {
				continue
			}
			if !isRepository(ctx, client, ORG, entitySlug) {
				log.Printf("Warning: '%s' is not a valid repository in organization '%s', skipping", entitySlug, ORG)
				continue
			}
			repoNames = append(repoNames, entitySlug)
		}

		if len(repoNames) == 0 {
			log.Fatal("No valid repositories found in the provided arguments")
		}

		findAndReportTeamsWithAccessToAllRepos(ctx, client, ORG, repoNames)
		return
	}

	// For all other operations, we need at least one user or repository argument
	if len(userOrRepoList) == 0 {
		log.Fatal("Usage is: ghOrgTool <options> <logins or repository names>")
	}

	// Process each entity (user or repository)
	for i := 0; i < len(userOrRepoList); i++ {
		entitySlug := userOrRepoList[i]
		if entitySlug == "" {
			// skip empty
			continue
		}

		// Detect what type of entity this is
		entType, resolvedName := detectEntityType(ctx, client, tc, entitySlug)

		switch entType {
		case entityRepository:
			// Handle repository operations
			_, err := checkRepository(ctx, client, ORG, resolvedName)
			if err != nil {
				log.Printf("Can't resolve Repository %s: %s", resolvedName, err)
				continue
			}

			// Default behavior: list teams for repository
			teams, err := getRepositoryTeams(ctx, client, ORG, resolvedName)
			if err != nil {
				log.Printf("Unable to resolve teams for Repository %s: %s", resolvedName, err)
				continue
			}
			log.Printf("Repository %s has the following teams with access:", resolvedName)
			for _, team := range teams {
				log.Printf("* %s (%s) %s", team.name, team.url, team.access)
			}

		case entityUser:
			// Handle user operations
			log.Printf("Processing user %s", resolvedName)

			// Supply the reset URL
			if *resetFlag {
				prompt(fmt.Sprintf("https://github.com/orgs/%s/people/%s/sso", ORG, resolvedName))
				log.Printf("Reset Link: https://github.com/orgs/%s/people/%s/sso", ORG, resolvedName)
				continue
			}

			// Check the user is valid
			valid, ghUser := userIsValid(ctx, client, tc, resolvedName)
			if !valid {
				continue
			}

			// Add to team
			if *addToTM {
				team := getTeamByName(ctx, client, ORG, *teamName)
				checkAndAddMember(ctx, client, team, ghUser)
				continue
			}

			// Default behavior (or explicit -t flag): list user's teams
			teams, err := getUserTeams(ctx, tc, ORG, resolvedName)
			if err == nil {
				log.Printf("User %s is a member of the following teams:", resolvedName)
				for _, team := range teams {
					log.Printf("* %s (%s)", team.name, team.url)
				}
			} else {
				log.Println("Unable to get teams: ", err)
			}

		default:
			prompt(fmt.Sprintf("Unable to identify '%s' as a user or repository.", entitySlug))
			log.Printf("Unable to identify '%s' as a user or repository.", entitySlug)
		}
	}
}
