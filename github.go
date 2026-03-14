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
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"gg-scm.io/pkg/git"
	"github.com/shurcooL/githubv4"
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
		qualifiedHeadRef := pr.HeadRefName
		if pr.HeadRepository.ID != baseRepo.ID {
			qualifiedHeadRef = pr.HeadRepository.Owner.Login + ":" + pr.HeadRefName
		}
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

	err := client.Mutate(ctx, &mutation, githubv4.UpdatePullRequestInput{
		PullRequestID: pr.ID,
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
		err := client.Mutate(ctx, &mutation, githubv4.MarkPullRequestReadyForReviewInput{
			PullRequestID: pr.ID,
		}, nil)
		if err != nil {
			return fmt.Errorf("mark %v#%d as ready for review: %v", baseRepoPath, pr.Number, err)
		}
	}

	return nil
}

var errPullRequestNotFound = errors.New("pull request not found")

func newGitHubHTTPClient(token string) *http.Client {
	return &http.Client{
		Transport: tokenTransport{
			host:  "api.github.com",
			token: token,
			rt:    http.DefaultTransport,
		},
	}
}

// gitHubToken obtains a GitHub personal access token from the environment.
func gitHubToken(ctx context.Context, lookupEnv lookupEnvFunc, lookPath lookPathFunc) (string, error) {
	const varName = "GITHUB_TOKEN"

	if token := lookupEnv.get(varName); token != "" {
		return token, nil
	}

	if tokenData, err := readConfigFile(lookupEnv, "github-token"); err == nil {
		return string(bytes.TrimSpace(tokenData)), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	ghExe, err := lookPath("gh")
	if err != nil {
		// If the gh CLI is not installed, prompt the user to set the environment variable.
		if errors.Is(err, exec.ErrNotFound) {
			return "", fmt.Errorf("%s not set", varName)
		}
		return "", fmt.Errorf("gh auth token: %v", err)
	}
	cmd := exec.CommandContext(ctx, ghExe, "auth", "token", "--hostname=github.com")
	raw, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gh auth token: %v", err)
	}
	return string(bytes.TrimSpace(raw)), nil
}
