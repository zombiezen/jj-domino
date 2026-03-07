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
	"cmp"
	"context"
	"errors"
	"fmt"
	"iter"
	"log"
	"os"
	"slices"
	"strconv"
	"strings"

	"gg-scm.io/pkg/git"
	"github.com/alecthomas/kong"
	jsonv2 "github.com/go-json-experiment/json"
	"github.com/shurcooL/githubv4"
	"zombiezen.com/go/jj-domino/internal/jujutsu"
)

type cli struct {
	Submit submitCmd `cmd:"" help:"Submit a review stack"`
	Doctor doctorCmd `cmd:"" help:"Verify auth and config settings"`
}

type submitCmd struct {
	Draft        bool    `short:"d" help:"Submit PR as draft"`
	TemplatePath *string `short:"t" help:"Template path"`
	Bookmark     string  `short:"b" help:"Bookmark to send"`
	Root         *string `short:"R" help:"Optional repository root (defaults to \"jj root\")"`
	DryRun       bool    `short:"n" help:"Don't send to GitHub"`
}

func (c *submitCmd) Run(ctx context.Context) error {
	const defaultPRNumberWidth = 3

	opts := jujutsu.Options{}
	if c.Root != nil {
		opts.Dir = *c.Root
	}
	jj, err := jujutsu.New(opts)
	if err != nil {
		return err
	}
	bookmarks, err := jj.ListBookmarks(ctx)
	if err != nil {
		return err
	}

	stack, err := stackForBookmark(ctx, jj, bookmarks, c.Bookmark)
	if err != nil {
		return err
	}

	cfg, err := jj.ReadSettings(ctx)
	if err != nil {
		return err
	}
	var trunkRevset string
	if err := jsonv2.Unmarshal(cfg[`revset-aliases."trunk()"`], &trunkRevset); err != nil {
		return fmt.Errorf("trunk: %v", err)
	}
	trunkRefSymbol, err := jujutsu.ParseRefSymbol(trunkRevset)
	if err != nil {
		return fmt.Errorf("trunk %q: %v", trunkRevset, err)
	}
	if trunkRefSymbol.Remote == "" {
		return fmt.Errorf("trunk() does not have an associated remote")
	}
	pushRemoteName := "origin"
	if pushSetting := cfg["git.push"]; len(pushSetting) > 0 {
		if err := jsonv2.Unmarshal(pushSetting, &pushRemoteName); err != nil {
			return fmt.Errorf("git.push: %v", err)
		}
	}

	gitRoot, err := jj.GitRoot(ctx)
	if err != nil {
		return err
	}
	g, err := git.New(git.Options{Dir: gitRoot})
	if err != nil {
		return err
	}
	gitConfig, err := g.ReadConfig(ctx)
	if err != nil {
		return err
	}
	remotes := gitConfig.ListRemotes()
	baseRemote := remotes[trunkRefSymbol.Remote]
	if baseRemote == nil {
		return fmt.Errorf("unknown remote %s from trunk()", trunkRefSymbol.Remote)
	}
	baseRepoPath, err := gitHubRepositoryForURL(baseRemote.FetchURL)
	if err != nil {
		return fmt.Errorf("trunk() remote: %v", err)
	}
	pushRemote := remotes[pushRemoteName]
	if pushRemote == nil {
		return fmt.Errorf("unknown remote %s from git.push", pushRemoteName)
	}
	headRepoPath, err := gitHubRepositoryForURL(pushRemote.PushURL)
	if err != nil {
		return fmt.Errorf("push remote: %v", err)
	}

	token, err := gitHubToken(ctx)
	if err != nil {
		if !c.DryRun {
			return err
		}
		// If we're doing a dry run, don't worry about GitHub API.
		// We can still display the proposed PRs.
		log.Printf("Unable to authenticate to GitHub: %v", err)

		plan := planPullRequests(baseRepoPath, trunkRefSymbol.Name, placeholderGitHubRepository(headRepoPath), stack)
		sb := new(strings.Builder)
		for _, pr := range plan {
			sb.WriteString(prNumberPlaceholder(defaultPRNumberWidth))
			sb.WriteString(": ")
			pr.writeLogLine(sb)
			sb.WriteString("\n")
		}
		os.Stdout.WriteString(sb.String())
		return nil
	}

	httpClient := newGitHubHTTPClient(token)
	defer httpClient.CloseIdleConnections()
	gitHubClient := githubv4.NewClient(httpClient)

	headRepo := placeholderGitHubRepository(headRepoPath)
	var planError error
	if !c.DryRun {
		newHeadRepo, err := fetchRepository(ctx, gitHubClient, headRepoPath)
		if err != nil {
			// If we fail to fetch the head repository, don't stop immediately.
			// We need it to create pull requests,
			// but we don't need it for planning.
			planError = errors.Join(planError, err)
		} else {
			headRepo = newHeadRepo
		}
	}

	plan := planPullRequests(baseRepoPath, trunkRefSymbol.Name, headRepo, stack)

	var baseRepo *gitHubRepository
	for _, pr := range plan {
		newBaseRepo, existingPR, err := findOpenPullRequestForHead(ctx, gitHubClient, baseRepoPath, headRepoPath, string(pr.HeadRefName))
		baseRepo = cmp.Or(newBaseRepo, baseRepo)
		if err != nil && !errors.Is(err, errPullRequestNotFound) {
			planError = errors.Join(planError, err)
			continue
		}
		if existingPR != nil {
			pr.ID = existingPR.ID
			pr.Number = existingPR.Number
			pr.Title = existingPR.Title
			pr.HeadRepository = existingPR.HeadRepository
			// TODO(#10): Modify body in place with footer.
			pr.Body = existingPR.Body
		}
	}
	if planError != nil {
		return planError
	}

	if c.DryRun {
		prNumberWidth := cmp.Or(maxIntWidth(func(yield func(githubv4.Int) bool) {
			for _, pr := range plan {
				if pr.ID != nil {
					if !yield(pr.Number) {
						return
					}
				}
			}
		}), defaultPRNumberWidth)
		sb := new(strings.Builder)
		for _, pr := range plan {
			if pr.ID == nil {
				sb.WriteString(prNumberPlaceholder(prNumberWidth))
			} else {
				sb.WriteString(formatPRNumber(pr.Number, prNumberWidth))
			}
			sb.WriteString(": ")
			pr.writeLogLine(sb)
			sb.WriteString("\n")
		}
		os.Stdout.WriteString(sb.String())
		return nil
	}

	prNumberWidth := cmp.Or(maxIntWidth(func(yield func(githubv4.Int) bool) {
		for _, pr := range plan {
			if pr.ID != nil {
				if !yield(pr.Number) {
					return
				}
			}
		}
	}), defaultPRNumberWidth)
	for _, pr := range plan {
		isNew := pr.ID == nil
		if isNew {
			if err := createPullRequest(ctx, gitHubClient, baseRepo, &pr.pullRequest); err != nil {
				return err
			}
		} else {
			if err := updatePullRequest(ctx, gitHubClient, baseRepoPath, &pr.pullRequest); err != nil {
				return err
			}
		}
		prNumberWidth = max(prNumberWidth, maxIntWidth(func(yield func(githubv4.Int) bool) {
			yield(pr.Number)
		}))
		sb := new(strings.Builder)
		sb.WriteString(formatPRNumber(pr.Number, prNumberWidth))
		sb.WriteString(": ")
		pr.writeLogLine(sb)
		if isNew {
			sb.WriteString(" (new)")
		}
		sb.WriteString("\n")
		os.Stdout.WriteString(sb.String())
	}

	return nil
}

type plannedPullRequest struct {
	pullRequest
	baseRepositoryPath gitHubRepositoryPath
}

func planPullRequests(baseRepoPath gitHubRepositoryPath, baseRefName string, headRepo *gitHubRepository, stack []localCommitRef) []*plannedPullRequest {
	plan := make([]*plannedPullRequest, 0, len(stack))
	isFork := headRepo.path() != baseRepoPath
	for i, bookmark := range stack {
		var prBase string
		if i == 0 || isFork {
			// A pull request's base ref must be in the pull request's repository.
			// If we're pulling from a fork, then always use the trunk.
			prBase = baseRefName
		} else {
			prBase = stack[i-1].name
		}
		title, body := cutCommitDescription(bookmark.commit.Description)
		// TODO(#10): Add footer.
		plan = append(plan, &plannedPullRequest{
			baseRepositoryPath: baseRepoPath,
			pullRequest: pullRequest{
				BaseRefName:    githubv4.String(prBase),
				HeadRepository: headRepo,
				HeadRefName:    githubv4.String(bookmark.name),

				Title:   githubv4.String(title),
				Body:    githubv4.String(body),
				IsDraft: githubv4.Boolean(i > 0),
			},
		})
	}
	return plan
}

func (pr *plannedPullRequest) writeLogLine(sb *strings.Builder) {
	if pr.IsDraft {
		sb.WriteString("[DRAFT] ")
	}
	sb.WriteString(string(pr.Title))
	sb.WriteString(" [")
	if pr.HeadRepository.path() == pr.baseRepositoryPath {
		sb.WriteString(string(pr.BaseRefName))
		sb.WriteString(" ← ")
		sb.WriteString(string(pr.HeadRefName))
	} else {
		sb.WriteString(pr.baseRepositoryPath.Owner)
		sb.WriteString(":")
		sb.WriteString(string(pr.BaseRefName))
		sb.WriteString(" ← ")
		sb.WriteString(string(pr.HeadRepository.Owner.Login))
		sb.WriteString(":")
		sb.WriteString(string(pr.HeadRefName))
	}
	sb.WriteString("]")
}

type localCommitRef struct {
	name   string
	commit *jujutsu.Commit
}

func stackForBookmark(ctx context.Context, jj *jujutsu.Jujutsu, bookmarks []*jujutsu.Bookmark, bookmark string) ([]localCommitRef, error) {
	type stackFrame struct {
		curr  *jujutsu.Commit
		trail []localCommitRef
	}

	i := slices.IndexFunc(bookmarks, func(b *jujutsu.Bookmark) bool {
		return b.Name == bookmark && b.Remote == ""
	})
	if i < 0 {
		return nil, fmt.Errorf("compute stack for %q: no such bookmark", bookmark)
	}
	b := bookmarks[i]
	headCommitID, ok := b.TargetMerge.Resolved()
	if !ok {
		return nil, fmt.Errorf("compute stack for %q: unresolved bookmark", bookmark)
	}

	revset := "trunk().." + headCommitID.String()
	changes := make(map[string]*jujutsu.Commit)
	err := jj.Log(ctx, revset, func(c *jujutsu.Commit) bool {
		changes[string(c.ID)] = c
		return true
	})
	if err != nil {
		return nil, fmt.Errorf("compute stack for %q: %v", bookmark, err)
	}
	headCommit := changes[string(headCommitID)]
	if headCommit == nil {
		return nil, fmt.Errorf("compute stack for %q: commit %v is ancestor of trunk()", bookmark, headCommitID)
	}

	stack := []localCommitRef{{
		name:   bookmark,
		commit: headCommit,
	}}
	visited := make(map[string]struct{})
	var resultError error
	for curr := headCommit.Parents; len(curr) > 0; {
		var next []jujutsu.CommitID
		for _, id := range curr {
			if _, done := visited[string(id)]; done {
				continue
			}
			visited[string(id)] = struct{}{}

			c := changes[string(id)]
			if c == nil {
				// Trunk or trunk ancestor.
				continue
			}
			var names []string
			for _, b := range bookmarks {
				if target, ok := b.TargetMerge.Resolved(); b.Remote == "" && ok && target.Equal(id) {
					names = append(names, b.Name)
				}
			}
			switch {
			case len(names) == 1:
				stack = append(stack, localCommitRef{
					name:   names[0],
					commit: c,
				})
			case len(names) > 1:
				resultError = errors.Join(resultError, fmt.Errorf("commit %v has multiple bookmarks", id))
			}
			next = append(next, c.Parents...)
		}
		curr = next
	}

	slices.Reverse(stack)
	if resultError != nil {
		resultError = fmt.Errorf("compute stack for %q: %w", bookmark, resultError)
	}
	return stack, resultError
}

type doctorCmd struct{}

func (c *doctorCmd) Run(ctx context.Context) error {
	token, err := gitHubToken(ctx)
	if err != nil {
		return err
	}
	httpClient := newGitHubHTTPClient(token)
	defer httpClient.CloseIdleConnections()
	client := githubv4.NewClient(httpClient)

	var query struct {
		Viewer struct {
			Login githubv4.String
		}
	}
	if err := client.Query(ctx, &query, nil); err != nil {
		return err
	}

	fmt.Printf("Authenticated as: %s\n", query.Viewer.Login)
	return nil
}

func main() {
	ctx := kong.Parse(&cli{}, kong.UsageOnError())
	ctx.BindTo(context.Background(), (*context.Context)(nil))
	if err := ctx.Run(); err != nil {
		log.Fatal(err)
	}
}

func cutCommitDescription(s string) (title, body string) {
	title, body, _ = strings.Cut(s, "\n")
	body = strings.TrimSpace(body)
	return
}

func formatPRNumber(n githubv4.Int, width int) string {
	buf := make([]byte, 0, width+1)
	buf = append(buf, '#')
	buf = strconv.AppendInt(buf, int64(n), 10)
	if n := len(buf); n < width+1 {
		buf = buf[:width+1]
		newStart := width + 1 - n
		copy(buf[newStart:], buf[:n])
		for i := range newStart {
			buf[i] = ' '
		}
	}
	return string(buf)
}

func prNumberPlaceholder(width int) string {
	sb := new(strings.Builder)
	sb.Grow(width + 1)
	sb.WriteByte('#')
	for range width {
		sb.WriteByte('X')
	}
	return sb.String()
}

func maxIntWidth[T ~int | ~int8 | ~int16 | ~int32 | ~int64](nums iter.Seq[T]) int {
	maxWidth := 0
	for n := range nums {
		w := 1
		if n < 0 {
			w++
			n = -n
		}
		for n >= 10 {
			w++
			n /= 10
		}
		maxWidth = max(maxWidth, w)
	}
	return maxWidth
}
