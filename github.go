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

package main

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
	"zombiezen.com/go/jj-domino/internal/jujutsu"
	"zombiezen.com/go/log"
)

type gitHubRepositoryPath struct {
	Owner string
	Repo  string
}

func gitHubRepositoryForURL(urlstr string) (gitHubRepositoryPath, error) {
	u, err := git.ParseURL(urlstr)
	if err != nil {
		return gitHubRepositoryPath{}, err
	}
	if u.Host != "github.com" || !(u.Scheme == "https" || u.Scheme == "ssh" && u.User.Username() == "git") {
		return gitHubRepositoryPath{}, fmt.Errorf("%s is not a GitHub repository", urlstr)
	}
	var p gitHubRepositoryPath
	var ok bool
	p.Owner, p.Repo, ok = strings.Cut(strings.TrimPrefix(u.Path, "/"), "/")
	if !ok || strings.Contains(p.Repo, "/") {
		return gitHubRepositoryPath{}, fmt.Errorf("%s is not a GitHub repository", urlstr)
	}
	p.Repo = strings.TrimSuffix(p.Repo, ".git")
	return p, nil
}

func (path gitHubRepositoryPath) String() string {
	return url.PathEscape(path.Owner) + "/" + url.PathEscape(path.Repo)
}

// gitHubAuthenticatedUser returns the username associated with the client's authentication token.
func gitHubAuthenticatedUser(ctx context.Context, client *githubv4.Client) (githubv4.String, error) {
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

type pullRequest struct {
	ID      githubv4.ID
	Number  githubv4.Int
	Title   githubv4.String
	IsDraft githubv4.Boolean
	Body    githubv4.String
	URL     githubv4.URI

	BaseRefName    githubv4.String
	HeadRepository *gitHubRepository
	HeadRefName    githubv4.String
}

// changesURL constructs a URL to the "Files Changed" tab.
func (pr *pullRequest) changesURL(from, to jujutsu.CommitID) githubv4.URI {
	switch {
	case from.IsZero() || to.IsZero():
		return githubv4.URI{URL: pr.URL.JoinPath("changes")}
	case from.Equal(to):
		return githubv4.URI{URL: pr.URL.JoinPath("changes", from.String())}
	default:
		return githubv4.URI{URL: pr.URL.JoinPath("changes", from.String()+".."+to.String())}
	}
}

type gitHubRepository struct {
	ID    githubv4.ID
	Name  githubv4.String
	Owner *gitHubRepositoryOwner
}

func placeholderGitHubRepository(path gitHubRepositoryPath) *gitHubRepository {
	return &gitHubRepository{
		Owner: &gitHubRepositoryOwner{Login: githubv4.String(path.Owner)},
		Name:  githubv4.String(path.Repo),
	}
}

func fetchRepository(ctx context.Context, client *githubv4.Client, path gitHubRepositoryPath) (*gitHubRepository, error) {

	var query struct {
		Repository *gitHubRepository `graphql:"repository(owner: $owner, name: $name)"`
	}
	log.Debugf(ctx, "Getting information about %v...", path)
	err := client.Query(ctx, &query, map[string]any{
		"owner": githubv4.String(path.Owner),
		"name":  githubv4.String(path.Repo),
	})
	if err != nil {
		return nil, fmt.Errorf("get %v: %v", path, err)
	}
	return query.Repository, nil
}

func (repo *gitHubRepository) path() gitHubRepositoryPath {
	p := gitHubRepositoryPath{Repo: string(repo.Name)}
	if repo.Owner != nil {
		p.Owner = string(repo.Owner.Login)
	}
	return p
}

type gitHubRepositoryOwner struct {
	Login githubv4.String
}

func (owner *gitHubRepositoryOwner) String() string {
	if owner == nil {
		return ""
	}
	return string(owner.Login)
}

type gitHubPageInfo struct {
	EndCursor   githubv4.String
	HasNextPage githubv4.Boolean
}

func findOpenPullRequestForHead(ctx context.Context, client *githubv4.Client, baseRepoPath gitHubRepositoryPath, headRepoPath gitHubRepositoryPath, headRef string) (baseRepo *gitHubRepository, result *pullRequest, err error) {
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
				gitHubRepository

				PullRequests struct {
					Nodes    []*pullRequest
					PageInfo gitHubPageInfo
				} `graphql:"pullRequests(headRefName: $headRef, states: [OPEN], first: 50, after: $cursor)"`
			} `graphql:"repository(owner: $baseOwner, name: $baseRepo)"`
		}
		log.Debugf(ctx, "Searching for open pull requests on %v for %s...", baseRepoPath, headRef)
		if err := client.Query(ctx, &query, vars); err != nil {
			// Make error opaque.
			return nil, nil, errors.New(err.Error())
		}
		baseRepo = &query.Repository.gitHubRepository

		for _, pr := range query.Repository.PullRequests.Nodes {
			if pr.HeadRepository != nil && pr.HeadRepository.path() == headRepoPath {
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
		return baseRepo, nil, errPullRequestNotFound
	}
	return baseRepo, result, nil
}

func createPullRequest(ctx context.Context, client *githubv4.Client, baseRepo *gitHubRepository, pr *pullRequest) error {
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
		baseRepo.path(), pr.BaseRefName, qualifiedHeadRef)
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
		return fmt.Errorf("create pull request on %v for %s: %v", baseRepo.path(), qualifiedHeadRef, err)
	}

	pr.ID = mutation.CreatePullRequest.PullRequest.ID
	pr.Number = mutation.CreatePullRequest.PullRequest.Number
	pr.URL = mutation.CreatePullRequest.PullRequest.URL
	return nil
}

// updatePullRequest updates the body and base ref name of the pull request.
func updatePullRequest(ctx context.Context, client *githubv4.Client, baseRepoPath gitHubRepositoryPath, pr *pullRequest) error {
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

// updatePullRequestDraftStatus updates the draft status of the pull request
// to the value of pr.IsDraft.
func updatePullRequestDraftStatus(ctx context.Context, client *githubv4.Client, baseRepoPath gitHubRepositoryPath, pr *pullRequest) error {
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

func readGitHubPullRequestTemplate(ctx context.Context, jj *jujutsu.Jujutsu, revision string) string {
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

var errPullRequestNotFound = errors.New("pull request not found")

func newGitHubHTTPClient(token string) *http.Client {
	return &http.Client{
		Transport: userAgentTransport{
			userAgent: "https://github.com/zombiezen/jj-domino",
			rt: tokenTransport{
				host:  "api.github.com",
				token: token,
				rt:    http.DefaultTransport,
			},
		},
	}
}
