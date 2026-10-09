package main

import (
	"context"
	"net/http"
	"testing"
)

// ---------------------------------------------------------------------------
// URL normalization
// ---------------------------------------------------------------------------

func TestDecomposeGithubURL_RepositoryURL(t *testing.T) {
	components, err := decomposeGithubURL("https://github.com/example-org/my-repo")
	if err != nil {
		t.Fatalf("decomposeGithubURL returned error: %v", err)
	}
	if components.orgName == nil || *components.orgName != "example-org" {
		t.Fatalf("orgName = %v, want %q", components.orgName, "example-org")
	}
	if components.repoName == nil || *components.repoName != "my-repo" {
		t.Fatalf("repoName = %v, want %q", components.repoName, "my-repo")
	}
}

func TestDecomposeGithubURL_UserURL(t *testing.T) {
	components, err := decomposeGithubURL("https://github.com/someuser")
	if err != nil {
		t.Fatalf("decomposeGithubURL returned error: %v", err)
	}
	if components.userName == nil || *components.userName != "someuser" {
		t.Fatalf("userName = %v, want %q", components.userName, "someuser")
	}
}

func TestDecomposeGithubURL_TeamURL(t *testing.T) {
	components, err := decomposeGithubURL("https://github.com/orgs/example-org/teams/platform-engineering")
	if err != nil {
		t.Fatalf("decomposeGithubURL returned error: %v", err)
	}
	if components.orgName == nil || *components.orgName != "example-org" {
		t.Fatalf("orgName = %v, want %q", components.orgName, "example-org")
	}
	if components.teamName == nil || *components.teamName != "platform-engineering" {
		t.Fatalf("teamName = %v, want %q", components.teamName, "platform-engineering")
	}
}

func TestNormalizeEntityArgument_SlackWrappedURL(t *testing.T) {
	arg := "<https://github.com/example-org/my-repo|my-repo>"
	got := normalizeEntityArgument(arg)
	if got != "my-repo" {
		t.Fatalf("normalizeEntityArgument(%q) = %q, want %q", arg, got, "my-repo")
	}
}

func TestNormalizeEntityArgument_NonURLUnchanged(t *testing.T) {
	arg := "plain-entity"
	got := normalizeEntityArgument(arg)
	if got != arg {
		t.Fatalf("normalizeEntityArgument(%q) = %q, want %q", arg, got, arg)
	}
}

// ---------------------------------------------------------------------------
// contains
// ---------------------------------------------------------------------------

func TestContains_True(t *testing.T) {
	s := []string{"example.com", "shyftanalytics.com", "3ds.com"}
	for _, v := range s {
		if !contains(s, v) {
			t.Errorf("contains(%q) expected true", v)
		}
	}
}

func TestContains_False(t *testing.T) {
	s := []string{"example.com", "shyftanalytics.com", "3ds.com"}
	if contains(s, "github.com") {
		t.Error("contains(github.com) expected false")
	}
}

func TestContains_EmptySlice(t *testing.T) {
	if contains([]string{}, "anything") {
		t.Error("contains on empty slice should return false")
	}
}

func TestContains_EmptyString(t *testing.T) {
	s := []string{"a", "", "b"}
	if !contains(s, "") {
		t.Error("contains(\"\") expected true when empty string is in slice")
	}
}

// ---------------------------------------------------------------------------
// detectEntityType
// ---------------------------------------------------------------------------

func setupDetectEntityMux(repoSlug, userSlug, memberSlug string) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/orgs/example-org", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"login": "example-org"})
	})
	if repoSlug != "" {
		mux.HandleFunc("/repos/example-org/"+repoSlug, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, map[string]interface{}{"name": repoSlug, "owner": map[string]string{"login": "example-org"}})
		})
	}
	if userSlug != "" {
		mux.HandleFunc("/users/"+userSlug, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, map[string]string{"login": userSlug})
		})
	}
	if memberSlug != "" {
		mux.HandleFunc("/orgs/example-org/memberships/"+memberSlug, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, map[string]interface{}{
				"state":        "active",
				"role":         "member",
				"user":         map[string]string{"login": memberSlug},
				"organization": map[string]string{"login": "example-org"},
				"url":          "https://api.github.com/orgs/example-org/memberships/" + memberSlug,
			})
		})
	}
	return mux
}

func TestDetectEntityType_IsRepository(t *testing.T) {
	ctx := context.Background()
	mux := setupDetectEntityMux("my-repo", "", "")
	client, teardown := newTestClient(mux)
	defer teardown()

	entType, name := detectEntityType(ctx, client, nil, "my-repo")
	if entType != entityRepository {
		t.Errorf("entityType = %v, want entityRepository", entType)
	}
	if name != "my-repo" {
		t.Errorf("name = %q, want %q", name, "my-repo")
	}
}

func TestDetectEntityType_IsUserAndMember(t *testing.T) {
	ctx := context.Background()
	// No repo match for this slug, but user exists and is a member.
	mux := setupDetectEntityMux("", "someuser", "someuser")
	// Repo check: org exists but repo 404s.
	mux.HandleFunc("/repos/example-org/someuser", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	})
	client, teardown := newTestClient(mux)
	defer teardown()

	entType, name := detectEntityType(ctx, client, nil, "someuser")
	if entType != entityUser {
		t.Errorf("entityType = %v, want entityUser", entType)
	}
	if name != "someuser" {
		t.Errorf("name = %q, want %q", name, "someuser")
	}
}

func TestDetectEntityType_IsUserNotMember(t *testing.T) {
	ctx := context.Background()
	mux := setupDetectEntityMux("", "outsider", "")
	mux.HandleFunc("/repos/example-org/outsider", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	})
	// Membership check returns 404 → not a member.
	mux.HandleFunc("/orgs/example-org/memberships/outsider", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	})
	client, teardown := newTestClient(mux)
	defer teardown()

	entType, _ := detectEntityType(ctx, client, nil, "outsider")
	if entType != entityUnknown {
		t.Errorf("entityType = %v, want entityUnknown", entType)
	}
}

func TestDetectEntityType_Unknown(t *testing.T) {
	ctx := context.Background()
	mux := http.NewServeMux()
	mux.HandleFunc("/orgs/example-org", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"login": "example-org"})
	})
	mux.HandleFunc("/repos/example-org/nobody", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	})
	mux.HandleFunc("/users/nobody", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	})
	client, teardown := newTestClient(mux)
	defer teardown()

	entType, _ := detectEntityType(ctx, client, nil, "nobody")
	if entType != entityUnknown {
		t.Errorf("entityType = %v, want entityUnknown", entType)
	}
}
