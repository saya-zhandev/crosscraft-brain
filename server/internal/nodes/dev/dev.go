// Package dev provides developer-tools integration nodes: GitHub, GitLab, and Sentry.
// All use the declarative REST framework. GitLab uses a PRIVATE-TOKEN header instead
// of the standard Authorization header.
package dev

import (
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/rest"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

func sp(name, label string, required bool) schema.ParamSchema {
	return schema.ParamSchema{Name: name, Label: label, Type: "string", Required: required}
}
func ip(name, label string, def int) schema.ParamSchema {
	return schema.ParamSchema{Name: name, Label: label, Type: "number", Default: def}
}
func jp(name, label string) schema.ParamSchema {
	return schema.ParamSchema{Name: name, Label: label, Type: "json"}
}

// Nodes returns the full dev/DevOps node pack.
func Nodes() []schema.NodeDefinition {
	return []schema.NodeDefinition{
		GitHub().Build(),
		GitLab().Build(),
		Sentry().Build(),
	}
}

// ── GitHub ────────────────────────────────────────────────────────────────────

func GitHub() rest.Node {
	owner := sp("owner", "Owner (user or org)", true)
	repo := sp("repo", "Repository name", true)
	issueNumber := ip("issueNumber", "Issue Number", 0)
	prNumber := ip("prNumber", "PR Number", 0)
	path := sp("path", "File path", true)
	body := jp("body", "Body (JSON)")
	limit := ip("per_page", "Max per page", 30)
	ref := sp("ref", "Branch/Tag/SHA", false)
	return rest.Node{
		Type: "dev.github", Label: "GitHub", Group: "integration", Icon: "Github",
		Description: "Manage GitHub repos, issues, pull requests, and files.",
		BaseURL:     "https://api.github.com",
		CredType:    "githubApi",
		Headers:     map[string]string{"X-GitHub-Api-Version": "2022-11-28"},
		Auth:        rest.Auth{Kind: "header", Header: "Authorization", Prefix: "Bearer ", ValueField: "accessToken"},
		Ops: []rest.Op{
			// Repos
			{Resource: "repo", Name: "list", Label: "List Repos (user)", Method: "GET",
				Path: "/user/repos", ItemsPath: "",
				Query: map[string]string{"per_page": "per_page"},
				Params: []schema.ParamSchema{limit}},
			{Resource: "repo", Name: "get", Label: "Get Repo", Method: "GET",
				Path: "/repos/{owner}/{repo}",
				Params: []schema.ParamSchema{owner, repo}},
			// Issues
			{Resource: "issue", Name: "list", Label: "List Issues", Method: "GET",
				Path: "/repos/{owner}/{repo}/issues",
				Query: map[string]string{"per_page": "per_page"},
				Params: []schema.ParamSchema{owner, repo, limit}},
			{Resource: "issue", Name: "get", Label: "Get Issue", Method: "GET",
				Path: "/repos/{owner}/{repo}/issues/{issueNumber}",
				Params: []schema.ParamSchema{owner, repo, ip("issueNumber", "Issue Number", 0)}},
			{Resource: "issue", Name: "create", Label: "Create Issue", Method: "POST",
				Path: "/repos/{owner}/{repo}/issues", BodyParam: "body",
				Params: []schema.ParamSchema{owner, repo, body}},
			{Resource: "issue", Name: "update", Label: "Update Issue", Method: "PATCH",
				Path: "/repos/{owner}/{repo}/issues/{issueNumber}", BodyParam: "body",
				Params: []schema.ParamSchema{owner, repo, issueNumber, body}},
			// Pull Requests
			{Resource: "pull", Name: "list", Label: "List Pull Requests", Method: "GET",
				Path: "/repos/{owner}/{repo}/pulls",
				Query: map[string]string{"per_page": "per_page"},
				Params: []schema.ParamSchema{owner, repo, limit}},
			{Resource: "pull", Name: "get", Label: "Get Pull Request", Method: "GET",
				Path: "/repos/{owner}/{repo}/pulls/{prNumber}",
				Params: []schema.ParamSchema{owner, repo, prNumber}},
			{Resource: "pull", Name: "create", Label: "Create Pull Request", Method: "POST",
				Path: "/repos/{owner}/{repo}/pulls", BodyParam: "body",
				Params: []schema.ParamSchema{owner, repo, body}},
			// File contents
			{Resource: "file", Name: "get", Label: "Get File Contents", Method: "GET",
				Path: "/repos/{owner}/{repo}/contents/{path}",
				Query: map[string]string{"ref": "ref"},
				Params: []schema.ParamSchema{owner, repo, path, ref}},
			{Resource: "file", Name: "createOrUpdate", Label: "Create or Update File", Method: "PUT",
				Path: "/repos/{owner}/{repo}/contents/{path}", BodyParam: "body",
				Params: []schema.ParamSchema{owner, repo, path, body}},
			// Commits
			{Resource: "commit", Name: "list", Label: "List Commits", Method: "GET",
				Path: "/repos/{owner}/{repo}/commits",
				Query: map[string]string{"per_page": "per_page", "sha": "ref"},
				Params: []schema.ParamSchema{owner, repo, ref, limit}},
			// Releases
			{Resource: "release", Name: "list", Label: "List Releases", Method: "GET",
				Path: "/repos/{owner}/{repo}/releases",
				Params: []schema.ParamSchema{owner, repo}},
			{Resource: "release", Name: "latest", Label: "Get Latest Release", Method: "GET",
				Path: "/repos/{owner}/{repo}/releases/latest",
				Params: []schema.ParamSchema{owner, repo}},
		},
	}
}

// ── GitLab ────────────────────────────────────────────────────────────────────

func GitLab() rest.Node {
	projectID := sp("projectId", "Project ID or path (URL-encoded)", true)
	issueIID := ip("issueIid", "Issue IID", 0)
	mrIID := ip("mrIid", "Merge Request IID", 0)
	body := jp("body", "Body (JSON)")
	limit := ip("per_page", "Max per page", 20)
	return rest.Node{
		Type: "dev.gitlab", Label: "GitLab", Group: "integration", Icon: "GitMerge",
		Description: "Manage GitLab projects, issues, and merge requests.",
		BaseURL:     "https://gitlab.com/api/v4",
		BaseURLParam: "baseUrl",
		CredType:    "gitlabApi",
		Auth:        rest.Auth{Kind: "header", Header: "PRIVATE-TOKEN", Prefix: "", ValueField: "accessToken"},
		Ops: []rest.Op{
			// Projects
			{Resource: "project", Name: "list", Label: "List Projects (yours)", Method: "GET",
				Path: "/projects",
				Query: map[string]string{"membership": "=true", "per_page": "per_page"},
				Params: []schema.ParamSchema{limit}},
			{Resource: "project", Name: "get", Label: "Get Project", Method: "GET",
				Path: "/projects/{projectId}",
				Params: []schema.ParamSchema{projectID}},
			// Issues
			{Resource: "issue", Name: "list", Label: "List Issues", Method: "GET",
				Path: "/projects/{projectId}/issues",
				Query: map[string]string{"per_page": "per_page"},
				Params: []schema.ParamSchema{projectID, limit}},
			{Resource: "issue", Name: "get", Label: "Get Issue", Method: "GET",
				Path: "/projects/{projectId}/issues/{issueIid}",
				Params: []schema.ParamSchema{projectID, issueIID}},
			{Resource: "issue", Name: "create", Label: "Create Issue", Method: "POST",
				Path: "/projects/{projectId}/issues", BodyParam: "body",
				Params: []schema.ParamSchema{projectID, body}},
			{Resource: "issue", Name: "update", Label: "Update Issue", Method: "PUT",
				Path: "/projects/{projectId}/issues/{issueIid}", BodyParam: "body",
				Params: []schema.ParamSchema{projectID, issueIID, body}},
			// Merge Requests
			{Resource: "mergeRequest", Name: "list", Label: "List Merge Requests", Method: "GET",
				Path: "/projects/{projectId}/merge_requests",
				Query: map[string]string{"per_page": "per_page"},
				Params: []schema.ParamSchema{projectID, limit}},
			{Resource: "mergeRequest", Name: "get", Label: "Get Merge Request", Method: "GET",
				Path: "/projects/{projectId}/merge_requests/{mrIid}",
				Params: []schema.ParamSchema{projectID, mrIID}},
			{Resource: "mergeRequest", Name: "create", Label: "Create Merge Request", Method: "POST",
				Path: "/projects/{projectId}/merge_requests", BodyParam: "body",
				Params: []schema.ParamSchema{projectID, body}},
			// Commits
			{Resource: "commit", Name: "list", Label: "List Commits", Method: "GET",
				Path: "/projects/{projectId}/repository/commits",
				Query: map[string]string{"per_page": "per_page"},
				Params: []schema.ParamSchema{projectID, limit}},
			// Pipelines
			{Resource: "pipeline", Name: "list", Label: "List Pipelines", Method: "GET",
				Path: "/projects/{projectId}/pipelines",
				Params: []schema.ParamSchema{projectID}},
		},
	}
}

// ── Sentry ────────────────────────────────────────────────────────────────────

func Sentry() rest.Node {
	orgSlug := sp("orgSlug", "Organization Slug", true)
	projectSlug := sp("projectSlug", "Project Slug", false)
	issueID := sp("issueId", "Issue ID", true)
	body := jp("body", "Body (JSON)")
	limit := ip("limit", "Max results", 25)
	return rest.Node{
		Type: "dev.sentry", Label: "Sentry", Group: "integration", Icon: "Bug",
		Description: "List and manage Sentry issues and events.",
		BaseURL:     "https://sentry.io/api/0",
		CredType:    "sentryApi",
		Auth:        rest.Auth{Kind: "header", Header: "Authorization", Prefix: "Bearer ", ValueField: "accessToken"},
		Ops: []rest.Op{
			{Resource: "organization", Name: "list", Label: "List Organizations", Method: "GET",
				Path: "/organizations/"},
			{Resource: "project", Name: "list", Label: "List Projects", Method: "GET",
				Path: "/organizations/{orgSlug}/projects/",
				Params: []schema.ParamSchema{orgSlug}},
			{Resource: "issue", Name: "list", Label: "List Issues", Method: "GET",
				Path: "/projects/{orgSlug}/{projectSlug}/issues/",
				Query: map[string]string{"limit": "limit"},
				Params: []schema.ParamSchema{orgSlug, projectSlug, limit}},
			{Resource: "issue", Name: "get", Label: "Get Issue", Method: "GET",
				Path: "/issues/{issueId}/",
				Params: []schema.ParamSchema{issueID}},
			{Resource: "issue", Name: "update", Label: "Update Issue", Method: "PUT",
				Path: "/issues/{issueId}/", BodyParam: "body",
				Params: []schema.ParamSchema{issueID, body}},
			{Resource: "issue", Name: "delete", Label: "Delete Issue", Method: "DELETE",
				Path: "/issues/{issueId}/",
				Params: []schema.ParamSchema{issueID}},
			{Resource: "event", Name: "list", Label: "List Events for Issue", Method: "GET",
				Path: "/issues/{issueId}/events/",
				Params: []schema.ParamSchema{issueID}},
			{Resource: "event", Name: "listProject", Label: "List Project Events", Method: "GET",
				Path: "/projects/{orgSlug}/{projectSlug}/events/",
				Query: map[string]string{"limit": "limit"},
				Params: []schema.ParamSchema{orgSlug, projectSlug, limit}},
		},
	}
}
