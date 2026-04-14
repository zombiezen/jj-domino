// Copyright 2026 Roxy Light and Benjamin Pollack
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is furnished
// to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice (including the next
// paragraph) shall be included in all copies or substantial portions of the
// Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS
// FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS
// OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY,
// WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF
// OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
//
// SPDX-License-Identifier: MIT

// Package github provides high-level data types and operations for GitHub
// that pertain to jj-domino.
package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"gg-scm.io/pkg/git"
	"github.com/shurcooL/githubv4"
	"zombiezen.com/go/jj-domino/internal/httptransport"
	"zombiezen.com/go/jj-domino/internal/jujutsu"
	"zombiezen.com/go/log"
)

// A RepositoryPath is a reference to a [Repository].
type RepositoryPath struct {
	Owner string
	Repo  string
}

// RepositoryPathForURL extracts the [RepositoryPath] from a Git-style URL.
func RepositoryPathForURL(urlstr string) (RepositoryPath, error) {
	u, err := git.ParseURL(urlstr)
	if err != nil {
		return RepositoryPath{}, err
	}
	if u.Host != "github.com" || !(u.Scheme == "https" || u.Scheme == "ssh" && u.User.Username() == "git") {
		return RepositoryPath{}, fmt.Errorf("%s is not a GitHub repository", urlstr)
	}
	var p RepositoryPath
	var ok bool
	p.Owner, p.Repo, ok = strings.Cut(strings.TrimPrefix(u.Path, "/"), "/")
	if !ok || strings.Contains(p.Repo, "/") {
		return RepositoryPath{}, fmt.Errorf("%s is not a GitHub repository", urlstr)
	}
	p.Repo = strings.TrimSuffix(p.Repo, ".git")
	return p, nil
}

// String returns the URL-encoded path, like "foo%20bar/baz".
func (path RepositoryPath) String() string {
	return url.PathEscape(path.Owner) + "/" + url.PathEscape(path.Repo)
}

// AuthenticatedUser returns the username associated with the client's authentication token.
func AuthenticatedUser(ctx context.Context, client *githubv4.Client) (githubv4.String, error) {
	var query struct {
		Viewer struct {
			Login githubv4.String
		}
	}
	if err := client.Query(ctx, &query, nil); err != nil {
		return "", fmt.Errorf("get github user: %v", err)
	}
	return query.Viewer.Login, nil
}

// PullRequest holds information about a GitHub pull request.
type PullRequest struct {
	ID      githubv4.ID
	Number  githubv4.Int
	Title   githubv4.String
	IsDraft githubv4.Boolean
	Body    githubv4.String
	URL     githubv4.URI

	BaseRefName    githubv4.String
	HeadRepository *Repository
	HeadRefName    githubv4.String
}

// ChangesURL constructs a URL to the "Files Changed" tab.
func (pr *PullRequest) ChangesURL(from, to jujutsu.CommitID) githubv4.URI {
	switch {
	case from.IsZero() || to.IsZero():
		return githubv4.URI{URL: pr.URL.JoinPath("changes")}
	case from.Equal(to):
		return githubv4.URI{URL: pr.URL.JoinPath("changes", from.String())}
	default:
		return githubv4.URI{URL: pr.URL.JoinPath("changes", from.String()+".."+to.String())}
	}
}

// Repository holds information about a GitHub repository.
type Repository struct {
	ID    githubv4.ID
	Name  githubv4.String
	Owner *RepositoryOwner
}

// PlaceholderRepository returns a [Repository] from the details in a [RepositoryPath].
// The ID field is left blank.
func PlaceholderRepository(path RepositoryPath) *Repository {
	return &Repository{
		Owner: &RepositoryOwner{Login: githubv4.String(path.Owner)},
		Name:  githubv4.String(path.Repo),
	}
}

// FetchRepository reads information about the repository at the given path.
func FetchRepository(ctx context.Context, client *githubv4.Client, path RepositoryPath) (*Repository, error) {

	var query struct {
		Repository *Repository `graphql:"repository(owner: $owner, name: $name)"`
	}
	log.Debugf(ctx, "Getting information about %v...", path)
	err := client.Query(ctx, &query, map[string]any{
		"owner": githubv4.String(path.Owner),
		"name":  githubv4.String(path.Repo),
	})
	if err != nil {
		return nil, fmt.Errorf("get %v: %v", path, err)
	}
	if query.Repository == nil {
		return nil, fmt.Errorf("get %v: not found", path)
	}
	return query.Repository, nil
}

// Path returns the repository's path.
func (repo *Repository) Path() RepositoryPath {
	p := RepositoryPath{Repo: string(repo.Name)}
	if repo.Owner != nil {
		p.Owner = string(repo.Owner.Login)
	}
	return p
}

// RepositoryOwner represents entities that can own GitHub repositories
// (i.e. either a user or an organization).
type RepositoryOwner struct {
	Login githubv4.String
}

// String returns the name of the owner as it appears in URLs.
func (owner *RepositoryOwner) String() string {
	if owner == nil {
		return ""
	}
	return string(owner.Login)
}

type pageInfo struct {
	EndCursor   githubv4.String
	HasNextPage githubv4.Boolean
}

// FindOpenPullRequestForHead searches for an open pull request
// in the given base repository that intends to merge the given head ref.
// If no such pull request exists, then FindOpenPullRequestForHead returns an error
// that wraps [ErrPullRequestNotFound].
func FindOpenPullRequestForHead(ctx context.Context, client *githubv4.Client, baseRepoPath RepositoryPath, headRepoPath RepositoryPath, headRef string) (baseRepo *Repository, result *PullRequest, err error) {
	defer func() {
		if err != nil {
			qualifiedHeadRef := headRef
			if headRepoPath != baseRepoPath {
				qualifiedHeadRef = headRepoPath.Owner + ":" + headRef
			}
			err = fmt.Errorf("find pull request on %v for %s: %w", baseRepoPath, qualifiedHeadRef, err)
		}
	}()

	vars := map[string]any{
		"baseOwner": githubv4.String(baseRepoPath.Owner),
		"baseRepo":  githubv4.String(baseRepoPath.Repo),
		"headRef":   githubv4.String(headRef),
		"cursor":    (*githubv4.String)(nil),
	}
	for {
		var query struct {
			Repository struct {
				Repository

				PullRequests struct {
					Nodes    []*PullRequest
					PageInfo pageInfo
				} `graphql:"pullRequests(headRefName: $headRef, states: [OPEN], first: 50, after: $cursor)"`
			} `graphql:"repository(owner: $baseOwner, name: $baseRepo)"`
		}
		log.Debugf(ctx, "Searching for open pull requests on %v for %s...", baseRepoPath, headRef)
		if err := client.Query(ctx, &query, vars); err != nil {
			// Make error opaque.
			return nil, nil, errors.New(err.Error())
		}
		baseRepo = &query.Repository.Repository

		for _, pr := range query.Repository.PullRequests.Nodes {
			if pr.HeadRepository != nil && pr.HeadRepository.Path() == headRepoPath {
				if result != nil {
					return baseRepo, nil, fmt.Errorf("found multiple (#%d and #%d)", result.Number, pr.Number)
				}
				result = pr
			}
		}

		if !query.Repository.PullRequests.PageInfo.HasNextPage {
			break
		}
		vars["cursor"] = query.Repository.PullRequests.PageInfo.EndCursor
	}

	if result == nil {
		return baseRepo, nil, ErrPullRequestNotFound
	}
	return baseRepo, result, nil
}

// ErrPullRequestNotFound is returned
// when [FindOpenPullRequestForHead] cannot find a matching pull request.
var ErrPullRequestNotFound = errors.New("pull request not found")

// CreatePullRequest creates a pull request in the given repository.
// The pull request's ID, Number, and URL will be filled in if the request succeeds.
func CreatePullRequest(ctx context.Context, client *githubv4.Client, baseRepo *Repository, pr *PullRequest) error {
	var mutation struct {
		CreatePullRequest struct {
			PullRequest struct {
				ID     githubv4.ID
				Number githubv4.Int
				URL    githubv4.URI
			}
		} `graphql:"createPullRequest(input: $input)"`
	}

	qualifiedHeadRef := pr.HeadRefName
	if pr.HeadRepository.ID != baseRepo.ID {
		qualifiedHeadRef = pr.HeadRepository.Owner.Login + ":" + pr.HeadRefName
	}
	log.Debugf(ctx, "Creating pull request on %v for %s ← %s...",
		baseRepo.Path(), pr.BaseRefName, qualifiedHeadRef)
	err := client.Mutate(ctx, &mutation, githubv4.CreatePullRequestInput{
		RepositoryID: baseRepo.ID,
		Title:        pr.Title,
		Body:         new(pr.Body),
		Draft:        new(pr.IsDraft),

		BaseRefName:      pr.BaseRefName,
		HeadRefName:      pr.HeadRefName,
		HeadRepositoryID: new(pr.HeadRepository.ID),
	}, nil)
	if err != nil {
		return fmt.Errorf("create pull request on %v for %s: %v", baseRepo.Path(), qualifiedHeadRef, err)
	}

	pr.ID = mutation.CreatePullRequest.PullRequest.ID
	pr.Number = mutation.CreatePullRequest.PullRequest.Number
	pr.URL = mutation.CreatePullRequest.PullRequest.URL
	return nil
}

// UpdatePullRequest updates the body and base ref name of the pull request.
func UpdatePullRequest(ctx context.Context, client *githubv4.Client, baseRepoPath RepositoryPath, pr *PullRequest) error {
	var mutation struct {
		UpdatePullRequest struct {
			_ struct{} `graphql:"..."`
		} `graphql:"updatePullRequest(input: $input)"`
	}

	log.Debugf(ctx, "Updating %v#%d body and base ref = %s...", baseRepoPath, pr.Number, pr.BaseRefName)
	err := client.Mutate(ctx, &mutation, githubv4.UpdatePullRequestInput{
		PullRequestID: pr.ID,
		Title:         new(pr.Title),
		Body:          new(pr.Body),
		BaseRefName:   new(pr.BaseRefName),
	}, nil)
	if err != nil {
		return fmt.Errorf("update pull request %v#%d body: %v", baseRepoPath, pr.Number, err)
	}
	return nil
}

// UpdatePullRequestDraftStatus updates the draft status of the pull request
// to the value of pr.IsDraft.
func UpdatePullRequestDraftStatus(ctx context.Context, client *githubv4.Client, baseRepoPath RepositoryPath, pr *PullRequest) error {
	if pr.IsDraft {
		var mutation struct {
			ConvertPullRequestToDraft struct {
				_ struct{} `graphql:"..."`
			} `graphql:"convertPullRequestToDraft(input: $input)"`
		}
		log.Debugf(ctx, "Converting %v#%d to draft...", baseRepoPath, pr.Number)
		err := client.Mutate(ctx, &mutation, githubv4.ConvertPullRequestToDraftInput{
			PullRequestID: pr.ID,
		}, nil)
		if err != nil {
			return fmt.Errorf("convert %v#%d to draft: %v", baseRepoPath, pr.Number, err)
		}
	} else {
		var mutation struct {
			MarkPullRequestReadyForReview struct {
				_ struct{} `graphql:"..."`
			} `graphql:"markPullRequestReadyForReview(input: $input)"`
		}
		log.Debugf(ctx, "Marking %v#%d as ready for review...", baseRepoPath, pr.Number)
		err := client.Mutate(ctx, &mutation, githubv4.MarkPullRequestReadyForReviewInput{
			PullRequestID: pr.ID,
		}, nil)
		if err != nil {
			return fmt.Errorf("mark %v#%d as ready for review: %v", baseRepoPath, pr.Number, err)
		}
	}

	return nil
}

// ReadPullRequestTemplate attempts to read the GitHub [pull request template]
// from the Jujutsu repository at the given revision.
//
// [pull request template]: https://docs.github.com/en/communities/using-templates-to-encourage-useful-issues-and-pull-requests/about-issue-and-pull-request-templates
func ReadPullRequestTemplate(ctx context.Context, jj *jujutsu.Jujutsu, revision string) string {
	potential := []string{
		"root-file:pull_request_template.md",
		"root-file:PULL_REQUEST_TEMPLATE/pull_request_template.md",
		"root-file:docs/pull_request_template.md",
		"root-file:docs/PULL_REQUEST_TEMPLATE/pull_request_template.md",
		"root-file:.github/pull_request_template.md",
		"root-file:.github/PULL_REQUEST_TEMPLATE/pull_request_template.md",
	}
	for _, p := range potential {
		rc, err := jj.ShowFile(ctx, revision, p)
		if err != nil {
			log.Debugf(ctx, "Find pull request template: %v", err)
			continue
		}
		content := new(strings.Builder)
		_, err = io.Copy(content, rc)
		rc.Close()
		if err != nil {
			continue
		}
		return content.String()
	}
	return ""
}

// NewHTTPClient returns an [*http.Client]
// that will authenticate requests to the GitHub API with the given token.
func NewHTTPClient(token string, transport http.RoundTripper) *http.Client {
	return &http.Client{
		Transport: httptransport.UserAgent{
			UserAgent: "https://github.com/zombiezen/jj-domino",
			RoundTripper: httptransport.BearerToken{
				Host:         "api.github.com",
				Token:        token,
				RoundTripper: transport,
			},
		},
	}
}
