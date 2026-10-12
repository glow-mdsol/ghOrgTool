package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/go-github/v84/github"
)

type repositoryInfo struct {
	owner        string
	name         string
	description  string
	teams        []string
	templateRepo string
}

func getActionsCacheUsageByRepoForOrg(ctx context.Context, client *github.Client, org string) ([]*github.ActionsCacheUsage, error) {
	var usages []*github.ActionsCacheUsage
	opts := &github.ListOptions{PerPage: 100}

	for {
		usageList, resp, err := client.Actions.ListCacheUsageByRepoForOrg(ctx, org, opts)
		if err != nil {
			return nil, err
		}
		if usageList != nil {
			usages = append(usages, usageList.RepoCacheUsage...)
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return usages, nil
}

func getActionsStorageBillingForOrg(ctx context.Context, client *github.Client, org string) (*github.StorageBilling, error) {
	billing, _, err := client.Billing.GetOrganizationStorageBilling(ctx, org)
	if err != nil {
		return nil, err
	}

	return billing, nil
}

func topActionsCacheUsage(usages []*github.ActionsCacheUsage, limit int) []*github.ActionsCacheUsage {
	if len(usages) == 0 {
		return nil
	}

	sorted := append([]*github.ActionsCacheUsage(nil), usages...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].ActiveCachesSizeInBytes == sorted[j].ActiveCachesSizeInBytes {
			return sorted[i].FullName < sorted[j].FullName
		}
		return sorted[i].ActiveCachesSizeInBytes > sorted[j].ActiveCachesSizeInBytes
	})

	if limit <= 0 || limit > len(sorted) {
		limit = len(sorted)
	}

	return sorted[:limit]
}

func formatByteSize(size int64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
	value := float64(size)
	unitIndex := 0
	for value >= 1024 && unitIndex < len(units)-1 {
		value /= 1024
		unitIndex++
	}

	if unitIndex == 0 {
		return fmt.Sprintf("%d %s", size, units[unitIndex])
	}
	if unitIndex == 1 {
		return fmt.Sprintf("%.1f %s", value, units[unitIndex])
	}

	return fmt.Sprintf("%.2f %s", value, units[unitIndex])
}

func formatActionsCacheUsageReport(org string, usages []*github.ActionsCacheUsage, billing *github.StorageBilling, limit int) string {
	topUsages := topActionsCacheUsage(usages, limit)
	if len(topUsages) == 0 {
		return fmt.Sprintf("No repositories are currently using GitHub Actions cache storage in %s.\n", org)
	}

	var builder strings.Builder
	if billing != nil {
		fmt.Fprintf(
			&builder,
			"Org Actions shared storage billing summary:\n  Billable constrained storage: %d GB-month\n  Estimated total shared storage: %d GB-month\n  Days left in billing cycle: %d\n\n",
			billing.EstimatedPaidStorageForMonth,
			billing.EstimatedStorageForMonth,
			billing.DaysLeftInBillingCycle,
		)
	}
	fmt.Fprintf(&builder, "Top %d repositories by GitHub Actions cache storage in %s:\n\n", len(topUsages), org)
	for index, usage := range topUsages {
		fmt.Fprintf(
			&builder,
			"%2d. %-40s %10s  %d active caches\n",
			index+1,
			usage.FullName,
			formatByteSize(usage.ActiveCachesSizeInBytes),
			usage.ActiveCachesCount,
		)
	}

	return builder.String()
}

func listTopActionsCacheUsageByRepo(ctx context.Context, client *github.Client, org string, limit int) error {
	usages, err := getActionsCacheUsageByRepoForOrg(ctx, client, org)
	if err != nil {
		return err
	}

	billing, err := getActionsStorageBillingForOrg(ctx, client, org)
	if err != nil {
		log.Printf("Warning: unable to retrieve org Actions storage billing for %s: %v", org, err)
	}

	fmt.Print(formatActionsCacheUsageReport(org, usages, billing, limit))
	return nil
}

// checkRepository - check if a repository exists
func checkRepository(ctx context.Context,
	client *github.Client, owner, repositoryName string) (*repositoryInfo, error) {
	repository, _, err := client.Repositories.Get(ctx, owner, repositoryName)
	if err != nil {
		log.Println("Unable to detect whether repository exists:", err)
		return nil, err
	}
	info := repositoryInfo{
		owner:        *repository.Owner.Login,
		name:         *repository.Name,
		teams:        nil,
		templateRepo: "",
	}
	if repository.Description != nil {
		info.description = *repository.Description
	}
	return &info, nil
}

// isRepository - confirms that the input is the name of a repository within the org
func isRepository(ctx context.Context, client *github.Client, org, entitySlug string) bool {
	// repo will never have a @ in it
	if strings.Contains(entitySlug, "@") {
		return false
	}
	_, resp, err := client.Organizations.Get(ctx, org)
	if err != nil || resp.StatusCode == 404 {
		return false
	}
	_, resp, err = client.Repositories.Get(ctx, org, entitySlug)
	if err == nil && resp.StatusCode == 200 {
		return true
	}
	return false
}

// createRepository - create a new repository within the org
func createRepository(ctx context.Context,
	client *github.Client,
	info repositoryInfo) (*github.Repository, error) {
	exists, _, err := client.Repositories.Get(ctx, info.owner, info.name)
	if err != nil {
		log.Println("Unable to detect whether repository exists:", err)
		return nil, err
	}
	if exists != nil {
		log.Println("Repository exists")
		return nil, nil
	}

	repository := &github.Repository{
		Name:        github.String(info.name),
		Private:     github.Bool(true),
		Description: github.String(info.description)}

	if info.templateRepo != "" {
		template, _, err := client.Repositories.Get(ctx, info.owner, info.templateRepo)
		if err != nil {
			log.Println("Unable to locate template dir ", info.templateRepo)
		} else {
			repository.TemplateRepository = template
		}
	}

	repo, _, err := client.Repositories.Create(ctx, info.owner, repository)
	if err != nil {
		log.Fatal("Creating repo failed:", err)
	}
	return repo, nil
}

// enableVulnerabilityAlerts - enable vulnerability alerts for a repository
func enableVulnerabilityAlerts(ctx context.Context, client *github.Client, owner, repository string) (bool, error) {
	enabled, _, err := client.Repositories.GetVulnerabilityAlerts(ctx, owner, repository)
	if err != nil {
		log.Println("Unable to find repository", err)
		return false, err
	}
	if enabled {
		log.Println("Repository ", repository, "already enabled")
		return false, nil
	}
	_, err = client.Repositories.EnableVulnerabilityAlerts(ctx, owner, repository)
	if err != nil {
		log.Println("Unable to enable vulnerability alerts for repository", err)
		return false, err
	}
	return true, nil
}

// enableAutomatedSecurityFixes - enable Dependabot security updates for a repository
func enableAutomatedSecurityFixes(ctx context.Context, client *github.Client, owner, repository string) (bool, error) {
	fixes, _, err := client.Repositories.GetAutomatedSecurityFixes(ctx, owner, repository)
	if err != nil {
		log.Println("Unable to check automated security fixes for repository", err)
		return false, err
	}
	if fixes != nil && fixes.Enabled != nil && *fixes.Enabled {
		log.Println("Automated security fixes for repository", repository, "already enabled")
		return false, nil
	}
	_, err = client.Repositories.EnableAutomatedSecurityFixes(ctx, owner, repository)
	if err != nil {
		log.Println("Unable to enable automated security fixes for repository", err)
		return false, err
	}
	return true, nil
}

// dependabotEnableResult records which Dependabot-related features were newly enabled.
type dependabotEnableResult struct {
	vulnerabilityAlertsEnabled bool
	automatedFixesEnabled      bool
}

// enableDependabot enables both vulnerability alerts and automated security fixes.
func enableDependabot(ctx context.Context, client *github.Client, owner, repository string) (*dependabotEnableResult, error) {
	alertsEnabled, err := enableVulnerabilityAlerts(ctx, client, owner, repository)
	if err != nil {
		return nil, err
	}

	automatedFixesEnabled, err := enableAutomatedSecurityFixes(ctx, client, owner, repository)
	if err != nil {
		return nil, err
	}

	return &dependabotEnableResult{
		vulnerabilityAlertsEnabled: alertsEnabled,
		automatedFixesEnabled:      automatedFixesEnabled,
	}, nil
}

const dependabotConfigPath = ".github/dependabot.yml"

// hasDependabotGroupedPRs checks whether repository config appears to include grouped update rules.
func hasDependabotGroupedPRs(ctx context.Context, client *github.Client, owner, repository string) (bool, error) {
	file, _, _, err := client.Repositories.GetContents(ctx, owner, repository, dependabotConfigPath, nil)
	if err != nil {
		if ghErr, ok := err.(*github.ErrorResponse); ok && ghErr.Response != nil && ghErr.Response.StatusCode == http.StatusNotFound {
			return false, nil
		}
		return false, err
	}

	if file == nil {
		return false, fmt.Errorf("%s exists but is not a file", dependabotConfigPath)
	}

	content, err := file.GetContent()
	if err != nil {
		return false, err
	}

	return strings.Contains(content, "groups:") && strings.Contains(content, "patterns:") && strings.Contains(content, "*"), nil
}

func defaultDependabotGroupedPRTemplate() string {
	return "version: 2\nupdates:\n  - package-ecosystem: gomod\n    directory: /\n    schedule:\n      interval: weekly\n    groups:\n      all-dependencies:\n        patterns:\n          - \"*\"\n  - package-ecosystem: github-actions\n    directory: /\n    schedule:\n      interval: weekly\n    groups:\n      all-dependencies:\n        patterns:\n          - \"*\"\n"
}

// getRepositoryTeams - get the teams associated with a repository
func getRepositoryTeams(ctx context.Context, client *github.Client, owner, repositoryName string) ([]teamInfo, error) {
	var listOptions = github.ListOptions{PerPage: 100}
	repoTeams, _, err := client.Repositories.ListTeams(ctx, owner, repositoryName, &listOptions)
	if err != nil {
		return nil, err
	}
	var teams []teamInfo
	for _, team := range repoTeams {
		info := teamInfo{
			name:   *team.Name,
			slug:   *team.Slug,
			url:    *team.HTMLURL,
			access: *team.Permission,
		}
		if team.Description != nil {
			info.description = *team.Description
		}
		teams = append(teams, info)
	}
	return teams, nil
}

// repoTeamsResult holds the result of getting teams for a repository
type repoTeamsResult struct {
	repoName string
	teams    []teamInfo
	err      error
}

// teamMatchResult represents the result of team matching analysis
type teamMatchResult struct {
	exactMatches []teamInfo      // teams with access to ALL repositories
	closeMatches []teamMatchInfo // teams with access to over half of repositories
}

// teamMatchInfo contains team information along with match statistics
type teamMatchInfo struct {
	team          teamInfo
	accessCount   int      // number of repositories this team has access to
	accessPercent float64  // percentage of repositories this team has access to
	missingRepos  []string // repositories this team doesn't have access to
}

// findTeamsWithAccessToAllRepos finds teams that have access to all specified repositories
// It processes repositories in parallel for efficiency by launching separate goroutines
// for each repository to fetch team information concurrently.
// Returns a slice of teamInfo structs representing teams that have access to ALL repositories,
// or an empty slice if no such teams exist.
func findTeamsWithAccessToAllRepos(ctx context.Context, client *github.Client, owner string, repoNames []string) ([]teamInfo, error) {
	result, err := findTeamsWithAccessAnalysis(ctx, client, owner, repoNames)
	if err != nil {
		return nil, err
	}
	return result.exactMatches, nil
}

// findTeamsWithAccessAnalysis finds teams that have access to repositories with detailed analysis
// It processes repositories in parallel and returns both exact matches (all repos) and close matches (>50% of repos)
func findTeamsWithAccessAnalysis(ctx context.Context, client *github.Client, owner string, repoNames []string) (*teamMatchResult, error) {
	if len(repoNames) == 0 {
		return nil, fmt.Errorf("no repository names provided")
	}

	// Channel to collect results from goroutines
	resultChan := make(chan repoTeamsResult, len(repoNames))
	var wg sync.WaitGroup

	// Launch goroutines to get teams for each repository in parallel
	for _, repoName := range repoNames {
		wg.Add(1)
		go func(repo string) {
			defer wg.Done()
			teams, err := getRepositoryTeams(ctx, client, owner, repo)
			resultChan <- repoTeamsResult{
				repoName: repo,
				teams:    teams,
				err:      err,
			}
		}(repoName)
	}

	// Wait for all goroutines to complete and close the channel
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
	repoTeamsMap := make(map[string][]teamInfo)
	var errors []string

	for result := range resultChan {
		if result.err != nil {
			errors = append(errors, fmt.Sprintf("Error getting teams for repo %s: %v", result.repoName, result.err))
			continue
		}
		repoTeamsMap[result.repoName] = result.teams
	}

	// If we had errors getting teams for some repos, report them
	if len(errors) > 0 {
		log.Printf("Errors occurred while getting teams for some repositories:")
		for _, err := range errors {
			log.Printf("  %s", err)
		}
	}

	// If we couldn't get teams for any repository, return error
	if len(repoTeamsMap) == 0 {
		return nil, fmt.Errorf("could not get teams for any of the specified repositories")
	}

	// Find teams that appear in ALL repositories
	teamAccessCount := make(map[string]int)
	teamDetails := make(map[string]teamInfo)

	// Count how many repositories each team has access to
	for repoName, teams := range repoTeamsMap {
		log.Printf("Repository %s has %d teams with access", repoName, len(teams))
		for _, team := range teams {
			teamAccessCount[team.slug]++
			// Store team details (all repos should have same team details for same slug)
			teamDetails[team.slug] = team
		}
	}

	// Analyze team access patterns
	totalRepos := len(repoTeamsMap)

	var exactMatches []teamInfo
	var closeMatches []teamMatchInfo

	for teamSlug, count := range teamAccessCount {
		team := teamDetails[teamSlug]
		accessPercent := float64(count) / float64(totalRepos) * 100

		if count == totalRepos {
			// Exact match - has access to ALL repositories
			exactMatches = append(exactMatches, team)
		} else if accessPercent > 50.0 {
			// Close match - has access to more than 50% of repositories
			missingRepos := findMissingRepos(teamSlug, repoTeamsMap)
			closeMatches = append(closeMatches, teamMatchInfo{
				team:          team,
				accessCount:   count,
				accessPercent: accessPercent,
				missingRepos:  missingRepos,
			})
		}
	}

	return &teamMatchResult{
		exactMatches: exactMatches,
		closeMatches: closeMatches,
	}, nil
}

// findMissingRepos identifies which repositories a team doesn't have access to
func findMissingRepos(teamSlug string, repoTeamsMap map[string][]teamInfo) []string {
	var missingRepos []string

	for repoName, teams := range repoTeamsMap {
		hasAccess := false
		for _, team := range teams {
			if team.slug == teamSlug {
				hasAccess = true
				break
			}
		}
		if !hasAccess {
			missingRepos = append(missingRepos, repoName)
		}
	}

	return missingRepos
}

// findAndReportTeamsWithAccessToAllRepos is a convenience function that finds teams
// with access to all repos and reports the results, including close matches
func findAndReportTeamsWithAccessToAllRepos(ctx context.Context, client *github.Client, owner string, repoNames []string) {
	fmt.Printf("Analyzing team access patterns for %d repositories...\n", len(repoNames))
	fmt.Printf("Repositories: %v\n\n", repoNames)

	result, err := findTeamsWithAccessAnalysis(ctx, client, owner, repoNames)
	if err != nil {
		log.Printf("Error finding teams: %v", err)
		return
	}

	// Report exact matches (100% access)
	if len(result.exactMatches) > 0 {
		fmt.Printf("🎯 EXACT MATCHES - Teams with access to ALL %d repositories:\n\n", len(repoNames))
		for i, team := range result.exactMatches {
			fmt.Printf("%d. Team: %s\n", i+1, team.name)
			fmt.Printf("   Slug: %s\n", team.slug)
			if team.description != "" {
				fmt.Printf("   Description: %s\n", team.description)
			}
			fmt.Printf("   Access Level: %s\n", team.access)
			fmt.Printf("   URL: %s\n", team.url)
			fmt.Printf("   Coverage: 100%% (%d/%d repositories)\n", len(repoNames), len(repoNames))
			fmt.Printf("\n")
		}
	} else {
		fmt.Printf("🎯 EXACT MATCHES: No teams found with access to ALL repositories.\n\n")
	}

	// Report close matches (>50% access)
	if len(result.closeMatches) > 0 {
		fmt.Printf("🔍 CLOSE MATCHES - Teams with access to more than half of the repositories:\n\n")
		for i, match := range result.closeMatches {
			fmt.Printf("%d. Team: %s\n", i+1, match.team.name)
			fmt.Printf("   Slug: %s\n", match.team.slug)
			if match.team.description != "" {
				fmt.Printf("   Description: %s\n", match.team.description)
			}
			fmt.Printf("   Access Level: %s\n", match.team.access)
			fmt.Printf("   URL: %s\n", match.team.url)
			fmt.Printf("   Coverage: %.1f%% (%d/%d repositories)\n", match.accessPercent, match.accessCount, len(repoNames))
			if len(match.missingRepos) > 0 {
				fmt.Printf("   Missing access to: %v\n", match.missingRepos)
			}
			fmt.Printf("\n")
		}
	} else {
		fmt.Printf("🔍 CLOSE MATCHES: No teams found with access to more than half of the repositories.\n\n")
	}

	// Summary
	totalMatches := len(result.exactMatches) + len(result.closeMatches)
	if totalMatches == 0 {
		fmt.Printf("📊 SUMMARY: No teams found with significant access coverage.\n")
		fmt.Printf("To find teams with access to individual repositories, use the --teams flag with each repository name.\n")
	} else {
		fmt.Printf("📊 SUMMARY: Found %d exact matches and %d close matches.\n", len(result.exactMatches), len(result.closeMatches))
	}
}

// addUserAsRepoCollaborator adds a user as a repository collaborator with admin permission
// It checks for existing admin collaborators and warns if they were added recently or should be removed
func addUserAsRepoCollaborator(ctx context.Context, client *github.Client, owner, repo, username string) error {
	log.Printf("Checking existing collaborators for repository %s/%s", owner, repo)

	// List all collaborators with admin permission
	opts := &github.ListCollaboratorsOptions{
		ListOptions: github.ListOptions{PerPage: 100},
		Affiliation: "direct", // Only direct collaborators, not team members
	}

	collaborators, _, err := client.Repositories.ListCollaborators(ctx, owner, repo, opts)
	if err != nil {
		return fmt.Errorf("failed to list collaborators: %w", err)
	}

	// Check for existing admin collaborators
	now := time.Now()
	hasOldAdmin := false

	for _, collab := range collaborators {
		// Check if this collaborator has admin permission
		if collab.Permissions.GetAdmin() {
			// Skip if it's the user we're trying to add
			if *collab.Login == username {
				// Get the invitation/permission details to check when it was added
				permission, _, err := client.Repositories.GetPermissionLevel(ctx, owner, repo, username)
				if err == nil && permission.Permission != nil {
					log.Printf("⚠️  User %s already has %s access to repository %s/%s", username, *permission.Permission, owner, repo)

					// Try to get invitation info to determine how long ago
					invitations, _, invErr := client.Repositories.ListInvitations(ctx, owner, repo, nil)
					if invErr == nil {
						for _, inv := range invitations {
							if *inv.Invitee.Login == username {
								duration := now.Sub(inv.CreatedAt.Time)
								if duration.Hours() < 24 {
									fmt.Printf("⚠️  WARNING: User %s already has admin access (added %.1f hours ago)\n", username, duration.Hours())
									return nil
								}
							}
						}
					}

					// If we couldn't determine the time, just warn they already have access
					fmt.Printf("ℹ️  User %s already has admin access to this repository\n", username)
					return nil
				}
				continue
			}

			// Another user has admin access
			log.Printf("Found existing admin collaborator: %s", *collab.Login)

			// Try to determine when they were added
			// Note: GitHub API doesn't directly provide "added date" for collaborators
			// We can check invitations for pending ones, but for accepted ones we need to check events
			invitations, _, invErr := client.Repositories.ListInvitations(ctx, owner, repo, nil)
			addedTime := time.Time{}

			if invErr == nil {
				for _, inv := range invitations {
					if inv.Invitee != nil && *inv.Invitee.Login == *collab.Login {
						addedTime = inv.CreatedAt.Time
						break
					}
				}
			}

			// If we couldn't find invitation, check recent events
			if addedTime.IsZero() {
				events, _, evErr := client.Activity.ListRepositoryEvents(ctx, owner, repo, &github.ListOptions{PerPage: 100})
				if evErr == nil {
					for _, event := range events {
						if event.GetType() == "MemberEvent" {
							payload, err := event.ParsePayload()
							if err != nil {
								continue
							}
							memberEvent, ok := payload.(*github.MemberEvent)
							if ok && memberEvent.Member != nil && *memberEvent.Member.Login == *collab.Login {
								addedTime = event.CreatedAt.Time
								break
							}
						}
					}
				}
			}

			if !addedTime.IsZero() {
				duration := now.Sub(addedTime)
				if duration.Hours() > 24 {
					fmt.Printf("⚠️  WARNING: User %s has admin access and was added %.1f hours ago (>24h) - should be removed\n",
						*collab.Login, duration.Hours())
					hasOldAdmin = true
				} else {
					fmt.Printf("⚠️  WARNING: User %s already has admin access (added %.1f hours ago)\n",
						*collab.Login, duration.Hours())
				}
			} else {
				// Can't determine when they were added, just warn
				fmt.Printf("⚠️  WARNING: User %s has admin access (added date unknown) - consider reviewing\n", *collab.Login)
			}
		}
	}

	// Add the user as collaborator with admin permission
	log.Printf("Adding user %s as admin collaborator to repository %s/%s", username, owner, repo)

	opts2 := &github.RepositoryAddCollaboratorOptions{
		Permission: "admin",
	}

	_, resp, err := client.Repositories.AddCollaborator(ctx, owner, repo, username, opts2)
	if err != nil {
		return fmt.Errorf("failed to add collaborator: %w", err)
	}

	if resp.StatusCode == 204 {
		fmt.Printf("✅ User %s already had admin access to repository %s/%s\n", username, owner, repo)
	} else if resp.StatusCode == 201 {
		fmt.Printf("✅ Successfully sent admin collaboration invitation to user %s for repository %s/%s\n", username, owner, repo)
	} else {
		fmt.Printf("✅ Updated permissions for user %s on repository %s/%s\n", username, owner, repo)
	}

	if hasOldAdmin {
		fmt.Printf("\n💡 TIP: Consider removing admin users that were added more than 24 hours ago\n")
	}

	return nil
}

// listRepositoryCollaborators lists all collaborators on a repository with their permissions and when they were added
func listRepositoryCollaborators(ctx context.Context, client *github.Client, owner, repo string) error {
	log.Printf("Fetching collaborators for repository %s/%s", owner, repo)

	// List all collaborators
	opts := &github.ListCollaboratorsOptions{
		ListOptions: github.ListOptions{PerPage: 100},
		Affiliation: "direct", // Only direct collaborators, not team members
	}

	collaborators, _, err := client.Repositories.ListCollaborators(ctx, owner, repo, opts)
	if err != nil {
		return fmt.Errorf("failed to list collaborators: %w", err)
	}

	if len(collaborators) == 0 {
		fmt.Printf("📋 No direct collaborators found for repository %s/%s\n", owner, repo)
		fmt.Printf("   (Note: Team members are not included in this list)\n")
		return nil
	}

	fmt.Printf("📋 Collaborators for repository %s/%s:\n\n", owner, repo)

	// Get invitations to help determine when collaborators were added
	invitations, _, _ := client.Repositories.ListInvitations(ctx, owner, repo, nil)
	invitationMap := make(map[string]*github.RepositoryInvitation)
	for _, inv := range invitations {
		if inv.Invitee != nil {
			invitationMap[*inv.Invitee.Login] = inv
		}
	}

	// Get repository events to find when members were added
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
		fmt.Printf("%d. 👤 User: %s\n", i+1, *collab.Login)

		// Determine permission level
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
			fmt.Printf("   🔐 Permissions: %s\n", strings.Join(permissions, ", "))
		}

		// Get detailed permission level
		permission, _, permErr := client.Repositories.GetPermissionLevel(ctx, owner, repo, *collab.Login)
		if permErr == nil && permission.Permission != nil {
			fmt.Printf("   📊 Access Level: %s\n", *permission.Permission)
		}

		// Try to determine when they were added
		var addedTime time.Time
		var addedSource string

		// Check invitations first
		if inv, exists := invitationMap[*collab.Login]; exists {
			addedTime = inv.CreatedAt.Time
			addedSource = "invitation"
		}

		// Check events if not found in invitations
		if addedTime.IsZero() {
			if eventTime, exists := eventMap[*collab.Login]; exists {
				addedTime = eventTime
				addedSource = "event"
			}
		}

		if !addedTime.IsZero() {
			duration := now.Sub(addedTime)
			fmt.Printf("   📅 Added: %s (%.1f hours ago)\n", addedTime.Format("2006-01-02 15:04:05"), duration.Hours())
			if addedSource == "invitation" {
				fmt.Printf("   ℹ️  Status: Invitation pending\n")
			}

			// Warn if admin access is old
			if collab.Permissions.GetAdmin() && duration.Hours() > 24 {
				fmt.Printf("   ⚠️  WARNING: Admin access granted >24 hours ago - consider reviewing\n")
			}
		} else {
			fmt.Printf("   📅 Added: Unknown (not found in recent events)\n")
		}

		if collab.HTMLURL != nil {
			fmt.Printf("   🔗 Profile: %s\n", *collab.HTMLURL)
		}

		fmt.Printf("\n")
	}

	fmt.Printf("📊 Total: %d direct collaborator(s)\n", len(collaborators))

	return nil
}

// adminGrantResult holds information about a single admin-access grant event
// that occurred within the recent lookback window.
type adminGrantResult struct {
	repo        string
	login       string
	actor       string
	grantedAt   time.Time
	accessURL   string
	active      bool
	currentPerm string
	// revokedAt is zero when access is still active or no matching removal was found.
	revokedAt time.Time
	heldFor   time.Duration
}

// adminRemovalResult holds information about a single admin-access removal event
// that occurred within the recent lookback window.
type adminRemovalResult struct {
	repo        string
	login       string
	actor       string
	removedAt   time.Time
	accessURL   string
	currentPerm string
}

// adminGrantLookbackSince returns the lower bound timestamp for admin-grant
// queries. Default is the last 48 hours; on Mondays we roll back to Friday
// by using a 72-hour lookback window.
func adminGrantLookbackSince(now time.Time) time.Time {
	lookback := 48 * time.Hour
	if now.Weekday() == time.Monday {
		lookback = 72 * time.Hour
	}
	return now.Add(-lookback)
}

// getOrgRepos returns all repositories for the given organization.
// findRecentAdminGrants queries the organisation audit log for repo.add_member
// events in the recent lookback window where the permission granted was admin,
// and checks whether each user still holds that access. Grants that are no longer
// active are dropped unless includeRevoked is set.
//
// Audit log approach: a handful of paginated API calls regardless of repo count,
// versus the old per-repo event-scan which made O(repos) parallel calls.
//
// Requires the token to have org owner permissions (needed to read the audit log).
func findRecentAdminGrants(ctx context.Context, client *github.Client, org string, includeRevoked bool) ([]adminGrantResult, error) {
	now := time.Now()
	since := adminGrantLookbackSince(now)
	phrase := "action:repo.add_member"
	order := "desc"

	opts := &github.GetAuditLogOptions{
		Phrase:            &phrase,
		Order:             &order,
		ListCursorOptions: github.ListCursorOptions{PerPage: 100},
	}

	seen := make(map[string]bool) // "repo:login" – deduplicate multiple events for the same pair
	var results []adminGrantResult

	for {
		entries, resp, err := client.Organizations.GetAuditLog(ctx, org, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to query audit log: %w", err)
		}

		done := false
		for _, entry := range entries {
			ts := entry.GetCreatedAt()
			if ts.Time.Before(since) {
				done = true
				break
			}

			if entry.GetAction() != "repo.add_member" {
				continue
			}

			login := entry.GetUser()
			if login == "" {
				continue
			}
			actor := entry.GetActor()
			if actor == "" {
				actor = "unknown"
			}

			// "repo" is not a first-class AuditEntry field; it lands in AdditionalFields.
			repoFull, _ := entry.AdditionalFields["repo"].(string)
			if repoFull == "" {
				continue
			}
			parts := strings.SplitN(repoFull, "/", 2)
			if len(parts) != 2 || parts[0] != org {
				continue
			}
			repoName := parts[1]

			// Skip non-admin grants immediately if the permission is present in the log.
			if perm, ok := entry.AdditionalFields["permission"].(string); ok && perm != "admin" {
				continue
			}

			key := repoName + ":" + login
			if seen[key] {
				continue
			}
			seen[key] = true

			// Check whether the user still holds admin on this repo (the grant may have been revoked).
			currentPerm := "none"
			current, permResp, permErr := client.Repositories.GetPermissionLevel(ctx, org, repoName, login)
			if permErr == nil {
				currentPerm = current.GetPermission()
			} else if !includeRevoked || permResp == nil || permResp.StatusCode != http.StatusNotFound {
				if includeRevoked {
					log.Printf("Skipping grant verification for %s/%s (%s): %v", org, repoName, login, permErr)
				}
				continue
			}
			active := currentPerm == "admin"
			if !active && !includeRevoked {
				continue
			}

			result := adminGrantResult{
				repo:        repoName,
				login:       login,
				actor:       actor,
				grantedAt:   ts.Time,
				accessURL:   fmt.Sprintf("https://github.com/%s/%s/settings/access", org, repoName),
				active:      active,
				currentPerm: currentPerm,
			}
			if active {
				result.heldFor = now.Sub(ts.Time)
			}
			results = append(results, result)
		}

		if done || resp == nil || resp.After == "" {
			break
		}
		opts.After = resp.After
	}

	if includeRevoked {
		if err := attachAdminRevocationTimes(ctx, client, org, since, results); err != nil {
			return nil, err
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].repo != results[j].repo {
			return results[i].repo < results[j].repo
		}
		return results[i].login < results[j].login
	})
	return results, nil
}

// attachAdminRevocationTimes looks up repo.remove_member audit events for revoked
// grants and sets revokedAt/heldFor using the first removal after each grant.
func attachAdminRevocationTimes(ctx context.Context, client *github.Client, org string, since time.Time, results []adminGrantResult) error {
	pending := make(map[string]int)
	for i, r := range results {
		if !r.active {
			pending[r.repo+":"+r.login] = i
		}
	}
	if len(pending) == 0 {
		return nil
	}

	phrase := "action:repo.remove_member"
	order := "desc"
	opts := &github.GetAuditLogOptions{
		Phrase:            &phrase,
		Order:             &order,
		ListCursorOptions: github.ListCursorOptions{PerPage: 100},
	}

	for {
		entries, resp, err := client.Organizations.GetAuditLog(ctx, org, opts)
		if err != nil {
			return fmt.Errorf("failed to query audit log for removals: %w", err)
		}

		done := false
		for _, entry := range entries {
			ts := entry.GetCreatedAt().Time
			if ts.Before(since) {
				done = true
				break
			}
			if entry.GetAction() != "repo.remove_member" {
				continue
			}
			repoFull, _ := entry.AdditionalFields["repo"].(string)
			parts := strings.SplitN(repoFull, "/", 2)
			if len(parts) != 2 || parts[0] != org {
				continue
			}
			idx, ok := pending[parts[1]+":"+entry.GetUser()]
			if !ok {
				continue
			}
			r := &results[idx]
			// Entries are newest first, so keep overwriting to land on the earliest removal after the grant.
			if !ts.Before(r.grantedAt) {
				r.revokedAt = ts
				r.heldFor = ts.Sub(r.grantedAt)
			}
		}

		if done || resp == nil || resp.After == "" {
			break
		}
		opts.After = resp.After
	}
	return nil
}

// formatAccessDuration renders a duration as e.g. "1d 3h 25m".
func formatAccessDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Truncate(time.Minute)
	days := int(d / (24 * time.Hour))
	hours := int((d % (24 * time.Hour)) / time.Hour)
	minutes := int((d % time.Hour) / time.Minute)
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	}
	return fmt.Sprintf("%dh %dm", hours, minutes)
}

// findRecentAdminRemovals queries the organisation audit log for repo.remove_member
// events in the recent lookback window where the removed permission was admin,
// and confirms the user no longer has admin access.
func findRecentAdminRemovals(ctx context.Context, client *github.Client, org string) ([]adminRemovalResult, error) {
	since := adminGrantLookbackSince(time.Now())
	phrase := "action:repo.remove_member"
	order := "desc"

	opts := &github.GetAuditLogOptions{
		Phrase:            &phrase,
		Order:             &order,
		ListCursorOptions: github.ListCursorOptions{PerPage: 100},
	}

	seen := make(map[string]bool) // "repo:login" – deduplicate multiple events for the same pair
	var results []adminRemovalResult

	for {
		entries, resp, err := client.Organizations.GetAuditLog(ctx, org, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to query audit log: %w", err)
		}

		done := false
		for _, entry := range entries {
			ts := entry.GetCreatedAt()
			if ts.Time.Before(since) {
				done = true
				break
			}

			if entry.GetAction() != "repo.remove_member" {
				continue
			}

			login := entry.GetUser()
			if login == "" {
				continue
			}
			actor := entry.GetActor()
			if actor == "" {
				actor = "unknown"
			}

			repoFull, _ := entry.AdditionalFields["repo"].(string)
			if repoFull == "" {
				continue
			}
			parts := strings.SplitN(repoFull, "/", 2)
			if len(parts) != 2 || parts[0] != org {
				continue
			}
			repoName := parts[1]

			// Skip removals that were not admin if permission is present in the log.
			if perm, ok := entry.AdditionalFields["permission"].(string); ok && perm != "admin" {
				continue
			}

			key := repoName + ":" + login
			if seen[key] {
				continue
			}
			seen[key] = true

			currentPerm := "none"
			current, resp, permErr := client.Repositories.GetPermissionLevel(ctx, org, repoName, login)
			if permErr == nil {
				currentPerm = current.GetPermission()
				if currentPerm == "admin" {
					continue
				}
			} else {
				if resp == nil || resp.StatusCode != http.StatusNotFound {
					log.Printf("Skipping removal verification for %s/%s (%s): %v", org, repoName, login, permErr)
					continue
				}
			}

			results = append(results, adminRemovalResult{
				repo:        repoName,
				login:       login,
				actor:       actor,
				removedAt:   ts.Time,
				accessURL:   fmt.Sprintf("https://github.com/%s/%s/settings/access", org, repoName),
				currentPerm: currentPerm,
			})
		}

		if done || resp == nil || resp.After == "" {
			break
		}
		opts.After = resp.After
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].repo != results[j].repo {
			return results[i].repo < results[j].repo
		}
		return results[i].login < results[j].login
	})
	return results, nil
}

// reportRecentAdminGrants formats the results of findRecentAdminGrants for display.
func reportRecentAdminGrants(org string, results []adminGrantResult, includeRevoked bool) string {
	if len(results) == 0 {
		return fmt.Sprintf("No recent admin access grants detected across %s repositories.\n", org)
	}
	var b strings.Builder
	if includeRevoked {
		activeCount := 0
		for _, r := range results {
			if r.active {
				activeCount++
			}
		}
		fmt.Fprintf(&b, "Users granted recent admin access in %s (%d added, %d still active):\n\n", org, len(results), activeCount)
	} else {
		fmt.Fprintf(&b, "Users granted recent admin access (still active) in %s:\n\n", org)
	}
	for i, r := range results {
		fmt.Fprintf(&b, "%d. %s → %s\n   Granted by: %s\n   Granted: %s\n",
			i+1,
			r.login,
			r.repo,
			r.actor,
			r.grantedAt.UTC().Format("2006-01-02 15:04:05 UTC"),
		)
		switch {
		case r.active:
			fmt.Fprintf(&b, "   Status: active\n   Access held: %s (so far)\n", formatAccessDuration(r.heldFor))
		case !r.revokedAt.IsZero():
			fmt.Fprintf(&b, "   Status: revoked (current access: %s)\n   Revoked: %s\n   Access held: %s\n",
				r.currentPerm,
				r.revokedAt.UTC().Format("2006-01-02 15:04:05 UTC"),
				formatAccessDuration(r.heldFor),
			)
		default:
			fmt.Fprintf(&b, "   Status: revoked (current access: %s)\n   Access held: unknown (no removal event found)\n", r.currentPerm)
		}
		fmt.Fprintf(&b, "   Access settings: %s\n\n", r.accessURL)
	}
	return b.String()
}

// listRecentAdminGrants is the top-level command handler for --recent-admin-grants.
func listRecentAdminGrants(ctx context.Context, client *github.Client, org string, includeRevoked bool) error {
	now := time.Now()
	since := adminGrantLookbackSince(now)
	log.Printf("Scanning all repositories in %s for admin grants since %s...", org, since.UTC().Format("2006-01-02 15:04:05 UTC"))
	results, err := findRecentAdminGrants(ctx, client, org, includeRevoked)
	if err != nil {
		return err
	}
	fmt.Print(reportRecentAdminGrants(org, results, includeRevoked))
	return nil
}

// reportRecentAdminRemovals formats the results of findRecentAdminRemovals for display.
func reportRecentAdminRemovals(org string, results []adminRemovalResult) string {
	if len(results) == 0 {
		return fmt.Sprintf("No recent admin access removals detected across %s repositories.\n", org)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Users recently removed from admin access in %s:\n\n", org)
	for i, r := range results {
		fmt.Fprintf(&b, "%d. %s ← %s\n   Removed by: %s\n   Removed: %s\n   Current access: %s\n   Access settings: %s\n\n",
			i+1,
			r.login,
			r.repo,
			r.actor,
			r.removedAt.UTC().Format("2006-01-02 15:04:05 UTC"),
			r.currentPerm,
			r.accessURL,
		)
	}
	return b.String()
}

// listRecentAdminRemovals is the top-level command handler for --recent-admin-removals.
func listRecentAdminRemovals(ctx context.Context, client *github.Client, org string) error {
	now := time.Now()
	since := adminGrantLookbackSince(now)
	log.Printf("Scanning all repositories in %s for admin removals since %s...", org, since.UTC().Format("2006-01-02 15:04:05 UTC"))
	results, err := findRecentAdminRemovals(ctx, client, org)
	if err != nil {
		return err
	}
	fmt.Print(reportRecentAdminRemovals(org, results))
	return nil
}

// userRepoTeamAccess holds a team and the permission level it grants on a specific repository.
type userRepoTeamAccess struct {
	team       teamInfo
	permission string
}

// normalizePermission maps raw GitHub API permission strings to human-readable labels.
func normalizePermission(raw string) string {
	switch raw {
	case "pull":
		return "read"
	case "push":
		return "write"
	default:
		return raw
	}
}

// permissionLevel returns a numeric ranking for a permission string (higher = more access).
func permissionLevel(perm string) int {
	switch perm {
	case "admin":
		return 5
	case "maintain":
		return 4
	case "write":
		return 3
	case "triage":
		return 2
	case "read":
		return 1
	default:
		return 0
	}
}

// reportUserRepoAccess prints a report of a user's effective access to a repository
// by cross-referencing their team memberships with the teams that have access to the repo.
func reportUserRepoAccess(ctx context.Context, client *github.Client, tc *http.Client, org, userLogin, repoName string) error {
	userTeams, err := getUserTeams(ctx, tc, org, userLogin)
	if err != nil {
		return fmt.Errorf("unable to get teams for user %s: %w", userLogin, err)
	}

	repoTeams, err := getRepositoryTeams(ctx, client, org, repoName)
	if err != nil {
		return fmt.Errorf("unable to get teams for repository %s: %w", repoName, err)
	}

	// Build a slug → normalized-permission map for teams with repo access.
	repoTeamPermissions := make(map[string]string)
	for _, t := range repoTeams {
		repoTeamPermissions[t.slug] = normalizePermission(t.access)
	}

	// Find which of the user's teams also have access to this repo.
	var matches []userRepoTeamAccess
	for _, ut := range userTeams {
		if perm, ok := repoTeamPermissions[ut.slug]; ok {
			matches = append(matches, userRepoTeamAccess{team: ut, permission: perm})
		}
	}

	if len(matches) == 0 {
		fmt.Printf("User %s has no team-based access to repository %s/%s\n", userLogin, org, repoName)
		return nil
	}

	// Determine the highest (most permissive) level across all matching teams.
	effectivePerm := ""
	for _, m := range matches {
		if permissionLevel(m.permission) > permissionLevel(effectivePerm) {
			effectivePerm = m.permission
		}
	}

	fmt.Printf("Access report: %s → %s/%s\n\n", userLogin, org, repoName)
	fmt.Printf("Effective permission: %s\n\n", effectivePerm)
	fmt.Printf("Via teams:\n")
	for _, m := range matches {
		fmt.Printf("  - %s (%s): %s\n", m.team.name, m.team.url, m.permission)
	}

	return nil
}

// orgRepoAccessResult holds a user's effective access to one repository.
type orgRepoAccessResult struct {
	repoName      string
	effectivePerm string
	matchingTeams []userRepoTeamAccess
	err           error
}

// orgAdminAccessReport contains repositories where the user has effective admin access.
type orgAdminAccessReport struct {
	userLogin       string
	org             string
	results         []orgRepoAccessResult
	processedRepos  int
	skippedRepos    int
	resumeAfterRepo string
}

// orgAdminUserRepoResult holds one user's team-based admin access on a repository.
type orgAdminUserRepoResult struct {
	login           string
	repoName        string
	matchingTeams   []teamInfo
	hasDirectAccess bool
}

// orgWideAdminAccessReport contains all user/repository pairs where team-based
// effective access is admin.
type orgWideAdminAccessReport struct {
	org             string
	results         []orgAdminUserRepoResult
	processedRepos  int
	skippedRepos    int
	resumeAfterRepo string
}

// getOrgRepos returns all repositories for the given organization.
func getOrgRepos(ctx context.Context, client *github.Client, org string) ([]string, error) {
	opts := &github.RepositoryListByOrgOptions{ListOptions: github.ListOptions{PerPage: 100}}
	var repoNames []string

	for {
		repos, resp, err := client.Repositories.ListByOrg(ctx, org, opts)
		if err != nil {
			return nil, err
		}
		for _, repo := range repos {
			if repo.Name != nil {
				repoNames = append(repoNames, *repo.Name)
			}
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	sort.Strings(repoNames)
	return repoNames, nil
}

func buildUserRepoMatches(userTeams []teamInfo, repoTeams []teamInfo) []userRepoTeamAccess {
	repoTeamPermissions := make(map[string]string, len(repoTeams))
	for _, team := range repoTeams {
		repoTeamPermissions[team.slug] = normalizePermission(team.access)
	}

	var matches []userRepoTeamAccess
	for _, userTeam := range userTeams {
		if perm, ok := repoTeamPermissions[userTeam.slug]; ok {
			matches = append(matches, userRepoTeamAccess{team: userTeam, permission: perm})
		}
	}

	return matches
}

func effectivePermission(matches []userRepoTeamAccess) string {
	effectivePerm := ""
	for _, match := range matches {
		if permissionLevel(match.permission) > permissionLevel(effectivePerm) {
			effectivePerm = match.permission
		}
	}
	return effectivePerm
}

func adminRepoWorkerCount(totalRepos int) int {
	if totalRepos <= 0 {
		return 1
	}
	workers := runtime.NumCPU()
	if workers < 4 {
		workers = 4
	}
	if workers > 12 {
		workers = 12
	}
	if workers > totalRepos {
		workers = totalRepos
	}
	return workers
}

func getTeamMemberLoginsBySlug(ctx context.Context, client *github.Client, org, teamSlug string) ([]string, error) {
	opts := &github.TeamListTeamMembersOptions{ListOptions: github.ListOptions{PerPage: 100}}
	var logins []string

	for {
		members, resp, err := client.Teams.ListTeamMembersBySlug(ctx, org, teamSlug, opts)
		if err != nil {
			return nil, err
		}
		for _, member := range members {
			if member.Login != nil {
				logins = append(logins, *member.Login)
			}
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return logins, nil
}

func getDirectAdminCollaborators(ctx context.Context, client *github.Client, org, repoName string) ([]string, error) {
	opts := &github.ListCollaboratorsOptions{
		ListOptions: github.ListOptions{PerPage: 100},
		Affiliation: "direct",
	}
	var logins []string

	for {
		collaborators, resp, err := client.Repositories.ListCollaborators(ctx, org, repoName, opts)
		if err != nil {
			return nil, err
		}
		for _, collab := range collaborators {
			if collab.Login == nil || collab.Permissions == nil {
				continue
			}
			if collab.Permissions.GetAdmin() {
				logins = append(logins, *collab.Login)
			}
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return logins, nil
}

// findUserAdminRepoAccess analyzes all org repositories and returns those where
// the user's effective team-based permission is admin. Processing is bounded and
// parallel to keep runtime reasonable without issuing an unbounded burst of API calls.
func findUserAdminRepoAccess(ctx context.Context, client *github.Client, tc *http.Client, org, userLogin, resumeAfterRepo string) (*orgAdminAccessReport, error) {
	userTeams, err := getUserTeams(ctx, tc, org, userLogin)
	if err != nil {
		return nil, fmt.Errorf("unable to get teams for user %s: %w", userLogin, err)
	}

	repoNames, err := getOrgRepos(ctx, client, org)
	if err != nil {
		return nil, fmt.Errorf("unable to list repositories for %s: %w", org, err)
	}

	startIndex := 0
	if resumeAfterRepo != "" {
		startIndex = len(repoNames)
		for i, repoName := range repoNames {
			if repoName > resumeAfterRepo {
				startIndex = i
				break
			}
		}
	}
	if startIndex >= len(repoNames) {
		return &orgAdminAccessReport{userLogin: userLogin, org: org, resumeAfterRepo: resumeAfterRepo}, nil
	}

	repoNames = repoNames[startIndex:]
	jobs := make(chan string)
	results := make(chan orgRepoAccessResult, len(repoNames))

	workerCount := adminRepoWorkerCount(len(repoNames))
	var wg sync.WaitGroup
	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for repoName := range jobs {
				repoTeams, repoErr := getRepositoryTeams(ctx, client, org, repoName)
				if repoErr != nil {
					results <- orgRepoAccessResult{repoName: repoName, err: repoErr}
					continue
				}

				matches := buildUserRepoMatches(userTeams, repoTeams)
				results <- orgRepoAccessResult{
					repoName:      repoName,
					matchingTeams: matches,
					effectivePerm: effectivePermission(matches),
				}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, repoName := range repoNames {
			jobs <- repoName
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	report := &orgAdminAccessReport{userLogin: userLogin, org: org, resumeAfterRepo: resumeAfterRepo}
	for result := range results {
		report.processedRepos++
		if result.repoName > report.resumeAfterRepo {
			report.resumeAfterRepo = result.repoName
		}
		if result.err != nil {
			report.skippedRepos++
			log.Printf("Skipping repository %s due to error: %v", result.repoName, result.err)
			continue
		}
		if result.effectivePerm == "admin" {
			report.results = append(report.results, result)
		}
	}

	sort.Slice(report.results, func(i, j int) bool {
		return report.results[i].repoName < report.results[j].repoName
	})

	return report, nil
}

func formatUserAdminRepoAccessReport(report *orgAdminAccessReport) string {
	if report == nil {
		return ""
	}

	var b strings.Builder
	w := csv.NewWriter(&b)
	_ = w.Write([]string{"repo", "user", "access_type", "team_name", "access_url"})

	for _, result := range report.results {
		for _, match := range result.matchingTeams {
			_ = w.Write([]string{
				report.org + "/" + result.repoName,
				report.userLogin,
				"team",
				match.team.name,
				match.team.url,
			})
		}
	}
	w.Flush()

	return b.String()
}

func reportUserAdminRepoAccess(ctx context.Context, client *github.Client, tc *http.Client, org, userLogin, resumeAfterRepo string) error {
	report, err := findUserAdminRepoAccess(ctx, client, tc, org, userLogin, resumeAfterRepo)
	if err != nil {
		return err
	}
	fmt.Print(formatUserAdminRepoAccessReport(report))
	return nil
}

type repoWideAdminResult struct {
	repoName string
	results  []orgAdminUserRepoResult
	err      error
}

func sortAdminTeams(teams []teamInfo) {
	sort.Slice(teams, func(i, j int) bool {
		if teams[i].slug != teams[j].slug {
			return teams[i].slug < teams[j].slug
		}
		return teams[i].name < teams[j].name
	})
}

func sortOrgAdminRepoResults(results []orgAdminUserRepoResult) {
	for i := range results {
		sortAdminTeams(results[i].matchingTeams)
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].login < results[j].login
	})
}

func collectOrgAdminRepoAccess(ctx context.Context, client *github.Client, org, repoName string) repoWideAdminResult {
	repoTeams, repoErr := getRepositoryTeams(ctx, client, org, repoName)
	if repoErr != nil {
		return repoWideAdminResult{repoName: repoName, err: repoErr}
	}

	var adminTeams []teamInfo
	for _, team := range repoTeams {
		if normalizePermission(team.access) == "admin" {
			adminTeams = append(adminTeams, team)
		}
	}
	sortAdminTeams(adminTeams)

	repoUsers := make(map[string]*orgAdminUserRepoResult)
	for _, team := range adminTeams {
		logins, teamErr := getTeamMemberLoginsBySlug(ctx, client, org, team.slug)
		if teamErr != nil {
			return repoWideAdminResult{repoName: repoName, err: teamErr}
		}
		sort.Strings(logins)
		for _, login := range logins {
			result, ok := repoUsers[login]
			if !ok {
				result = &orgAdminUserRepoResult{login: login, repoName: repoName}
				repoUsers[login] = result
			}
			result.matchingTeams = append(result.matchingTeams, team)
		}
	}

	directAdminLogins, directErr := getDirectAdminCollaborators(ctx, client, org, repoName)
	if directErr != nil {
		return repoWideAdminResult{repoName: repoName, err: directErr}
	}
	sort.Strings(directAdminLogins)
	for _, login := range directAdminLogins {
		result, ok := repoUsers[login]
		if !ok {
			result = &orgAdminUserRepoResult{login: login, repoName: repoName}
			repoUsers[login] = result
		}
		result.hasDirectAccess = true
	}

	var repoResults []orgAdminUserRepoResult
	for _, userResult := range repoUsers {
		repoResults = append(repoResults, *userResult)
	}
	sortOrgAdminRepoResults(repoResults)

	return repoWideAdminResult{repoName: repoName, results: repoResults}
}

func resolveResumeAfterRepo(resumeAfterRepo, checkpointFile string) (string, error) {
	if resumeAfterRepo != "" || checkpointFile == "" {
		return resumeAfterRepo, nil
	}
	data, err := os.ReadFile(checkpointFile)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func writeResumeCheckpoint(checkpointFile, repoName string) error {
	if checkpointFile == "" {
		return nil
	}
	return os.WriteFile(checkpointFile, []byte(repoName+"\n"), 0o600)
}

func writeAdminCSVHeader(w io.Writer) error {
	csvWriter := csv.NewWriter(w)
	if err := csvWriter.Write([]string{"repo", "user", "access_type", "team_name", "access_url"}); err != nil {
		return err
	}
	csvWriter.Flush()
	return csvWriter.Error()
}

func writeOrgAdminRepoCSVRows(w io.Writer, org string, repoResult repoWideAdminResult) error {
	csvWriter := csv.NewWriter(w)
	for _, result := range repoResult.results {
		for _, team := range result.matchingTeams {
			if err := csvWriter.Write([]string{
				org + "/" + result.repoName,
				result.login,
				"team",
				team.name,
				team.url,
			}); err != nil {
				return err
			}
		}
		if result.hasDirectAccess {
			if err := csvWriter.Write([]string{
				org + "/" + result.repoName,
				result.login,
				"collaborator",
				"",
				fmt.Sprintf("https://github.com/%s/%s/settings/access", org, result.repoName),
			}); err != nil {
				return err
			}
		}
	}
	csvWriter.Flush()
	return csvWriter.Error()
}

// findOrgAdminRepoAccess analyzes all org repositories and returns all users
// who currently have team-based admin access on each repository.
func findOrgAdminRepoAccess(ctx context.Context, client *github.Client, org, resumeAfterRepo string) (*orgWideAdminAccessReport, error) {
	repoNames, err := getOrgRepos(ctx, client, org)
	if err != nil {
		return nil, fmt.Errorf("unable to list repositories for %s: %w", org, err)
	}

	startIndex := 0
	if resumeAfterRepo != "" {
		startIndex = len(repoNames)
		for i, repoName := range repoNames {
			if repoName > resumeAfterRepo {
				startIndex = i
				break
			}
		}
	}
	if startIndex >= len(repoNames) {
		return &orgWideAdminAccessReport{org: org, resumeAfterRepo: resumeAfterRepo}, nil
	}

	repoNames = repoNames[startIndex:]
	jobs := make(chan string)
	results := make(chan repoWideAdminResult, len(repoNames))

	workerCount := adminRepoWorkerCount(len(repoNames))
	var wg sync.WaitGroup
	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for repoName := range jobs {
				results <- collectOrgAdminRepoAccess(ctx, client, org, repoName)
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, repoName := range repoNames {
			jobs <- repoName
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	report := &orgWideAdminAccessReport{org: org, resumeAfterRepo: resumeAfterRepo}
	for result := range results {
		report.processedRepos++
		if result.repoName > report.resumeAfterRepo {
			report.resumeAfterRepo = result.repoName
		}
		if result.err != nil {
			report.skippedRepos++
			log.Printf("Skipping repository %s due to error: %v", result.repoName, result.err)
			continue
		}
		report.results = append(report.results, result.results...)
	}

	sort.Slice(report.results, func(i, j int) bool {
		if report.results[i].repoName != report.results[j].repoName {
			return report.results[i].repoName < report.results[j].repoName
		}
		return report.results[i].login < report.results[j].login
	})

	return report, nil
}

func formatOrgAdminRepoAccessReport(report *orgWideAdminAccessReport) string {
	if report == nil {
		return ""
	}

	var b strings.Builder
	w := csv.NewWriter(&b)
	_ = w.Write([]string{"repo", "user", "access_type", "team_name", "access_url"})

	for _, result := range report.results {
		for _, team := range result.matchingTeams {
			_ = w.Write([]string{
				report.org + "/" + result.repoName,
				result.login,
				"team",
				team.name,
				team.url,
			})
		}
		if result.hasDirectAccess {
			_ = w.Write([]string{
				report.org + "/" + result.repoName,
				result.login,
				"collaborator",
				"",
				fmt.Sprintf("https://github.com/%s/%s/settings/access", report.org, result.repoName),
			})
		}
	}
	w.Flush()

	return b.String()
}

func streamOrgAdminRepoAccessReport(ctx context.Context, client *github.Client, w io.Writer, org, resumeAfterRepo, checkpointFile string) error {
	resolvedResumeAfterRepo, err := resolveResumeAfterRepo(resumeAfterRepo, checkpointFile)
	if err != nil {
		return fmt.Errorf("unable to resolve resume checkpoint: %w", err)
	}

	repoNames, err := getOrgRepos(ctx, client, org)
	if err != nil {
		return fmt.Errorf("unable to list repositories for %s: %w", org, err)
	}

	startIndex := 0
	if resolvedResumeAfterRepo != "" {
		startIndex = len(repoNames)
		for i, repoName := range repoNames {
			if repoName > resolvedResumeAfterRepo {
				startIndex = i
				break
			}
		}
	}
	remainingRepos := repoNames[startIndex:]

	if resolvedResumeAfterRepo == "" {
		if err := writeAdminCSVHeader(w); err != nil {
			return err
		}
	}
	if len(remainingRepos) == 0 {
		return nil
	}

	jobs := make(chan string)
	results := make(chan repoWideAdminResult, len(remainingRepos))
	workerCount := adminRepoWorkerCount(len(remainingRepos))
	var wg sync.WaitGroup

	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for repoName := range jobs {
				results <- collectOrgAdminRepoAccess(ctx, client, org, repoName)
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, repoName := range remainingRepos {
			jobs <- repoName
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	indexByRepo := make(map[string]int, len(remainingRepos))
	for i, repoName := range remainingRepos {
		indexByRepo[repoName] = i
	}
	pending := make(map[int]repoWideAdminResult, len(remainingRepos))
	nextToFlush := 0

	for result := range results {
		pending[indexByRepo[result.repoName]] = result
		for {
			nextResult, ok := pending[nextToFlush]
			if !ok {
				break
			}
			delete(pending, nextToFlush)
			if nextResult.err != nil {
				return fmt.Errorf("processing repository %s: %w", nextResult.repoName, nextResult.err)
			}
			if err := writeOrgAdminRepoCSVRows(w, org, nextResult); err != nil {
				return err
			}
			if err := writeResumeCheckpoint(checkpointFile, nextResult.repoName); err != nil {
				return fmt.Errorf("unable to update checkpoint file: %w", err)
			}
			nextToFlush++
		}
	}

	return nil
}

func reportOrgAdminRepoAccess(ctx context.Context, client *github.Client, org, resumeAfterRepo, checkpointFile string) error {
	return streamOrgAdminRepoAccessReport(ctx, client, os.Stdout, org, resumeAfterRepo, checkpointFile)
}
