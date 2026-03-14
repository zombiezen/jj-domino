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
	"io"
	"iter"
	"log"
	"slices"
	"strconv"
	"strings"

	"gg-scm.io/pkg/git"
	"github.com/alecthomas/kong"
	jsonv2 "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
	"github.com/shurcooL/githubv4"
	"zombiezen.com/go/jj-domino/internal/jujutsu"
)

type submitCmd struct {
	Bookmark  string   `kong:"short=b,help=Push a stack with the bookmark as the head,placeholder=NAME,xor=revisions"`
	Revisions []string `kong:"short=r,sep=none,help=Push stacks pointing to these commits (can be repeated) (default: trunk()..@),placeholder=REVSETS,xor=revisions"`
	Changes   []string `kong:"name=change,short=c,sep=none,help=Push stacks by creating bookmarks (can be repeated),placeholder=REVSETS,xor=revisions"`
	Draft     bool     `kong:"short=d,help=Mark first pull request in stack as draft"`
	DryRun    bool     `kong:"short=n,help=Don\\'t send to GitHub"`
	Remote    string   `kong:"help=The remote to push to,placeholder=REMOTE"`
	Base      string   `kong:"help=Base remote bookmark to open pull requests against (default: trunk()),placeholder=BOOKMARK@REMOTE"`
	Push      bool     `kong:"negatable,help=Push commits to GitHub (on by default),default=true"`
}

func (c *submitCmd) Run(ctx context.Context, k *kong.Kong) error {
	const defaultPRNumberWidth = 3

	if c.shouldCreatePushBookmarks() && !c.Push {
		return fmt.Errorf("cannot combine --no-push with --change")
	}

	jj, err := jujutsu.New(jujutsu.Options{})
	if err != nil {
		return err
	}
	var jjSettings map[string]jsontext.Value
	if c.Remote == "" || c.Base == "" {
		// If the user fully specifies the base ref and remote,
		// we do not need to read Jujutsu's settings.
		// But in most cases, we need to.
		var err error
		jjSettings, err = jj.ReadSettings(ctx)
		if err != nil {
			return err
		}
	}
	pushRemoteName := c.Remote
	if pushRemoteName == "" {
		if pushSetting := jjSettings["git.push"]; len(pushSetting) > 0 {
			if err := jsonv2.Unmarshal(pushSetting, &pushRemoteName); err != nil {
				return fmt.Errorf("git.push: %v", err)
			}
		} else {
			pushRemoteName = "origin"
		}
	}
	pushOutput := k.Stderr
	if c.DryRun {
		pushOutput = k.Stdout
	}
	var baseRef jujutsu.RefSymbol
	if c.Base != "" {
		var err error
		baseRef, err = jujutsu.ParseRefSymbol(c.Base)
		if err != nil {
			return fmt.Errorf("--base: %v", err)
		}
		if baseRef.Remote == "" {
			return fmt.Errorf("--base=%s does not have an associated remote", c.Base)
		}
	} else {
		var trunkRevset string
		if err := jsonv2.Unmarshal(jjSettings[`revset-aliases."trunk()"`], &trunkRevset); err != nil {
			return fmt.Errorf("trunk: %v", err)
		}
		var err error
		baseRef, err = jujutsu.ParseRefSymbol(trunkRevset)
		if err != nil {
			return fmt.Errorf("trunk %q: %v", trunkRevset, err)
		}
		if baseRef.Remote == "" {
			return fmt.Errorf("trunk() does not have an associated remote")
		}
	}

	bookmarks, headBookmark, err := c.determineStackHead(ctx, jj, baseRef, pushRemoteName, pushOutput)
	if err != nil {
		return err
	}
	if headBookmark == "" {
		// We don't have a head bookmark (jj-domino submit --change REVSET --dry-run),
		// so exit now.
		return nil
	}

	stack, err := stackForBookmark(ctx, jj, bookmarks, baseRef, headBookmark)
	if err != nil {
		return err
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
	baseRemote := remotes[baseRef.Remote]
	if baseRemote == nil {
		return fmt.Errorf("unknown remote %s from base", baseRef.Remote)
	}
	baseRepoPath, err := gitHubRepositoryForURL(baseRemote.FetchURL)
	if err != nil {
		return fmt.Errorf("base remote: %v", err)
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

		plan := planPullRequests(baseRepoPath, baseRef.Name, placeholderGitHubRepository(headRepoPath), stack)
		plan[0].IsDraft = githubv4.Boolean(c.Draft)
		sb := new(strings.Builder)
		for _, pr := range plan {
			pr.writeLogLine(sb, defaultPRNumberWidth, false)
			sb.WriteString("\n")
		}
		io.WriteString(k.Stdout, sb.String())
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

	plan := planPullRequests(baseRepoPath, baseRef.Name, headRepo, stack)
	plan[0].IsDraft = githubv4.Boolean(c.Draft)

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
			pr.Body = githubv4.String(trimStackFooter(string(existingPR.Body)))
			pr.URL = existingPR.URL
		}
	}
	if planError != nil {
		return planError
	}

	isStdoutTerminal := isTerminal(k.Stdout)

	if c.Push && !c.shouldCreatePushBookmarks() {
		err := jjGitPush(ctx, jj, pushOutput, c.DryRun, pushRemoteName, func(yield func(string) bool) {
			for _, pr := range plan {
				if !yield("--bookmark=exact:" + jujutsu.Quote(string(pr.HeadRefName))) {
					return
				}
			}
		})
		if err != nil {
			return err
		}
		io.WriteString(pushOutput, "\n")
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
			pr.writeLogLine(sb, prNumberWidth, isStdoutTerminal)
			if pr.ID == nil {
				sb.WriteString(" (new)")
			}
			sb.WriteString("\n")
		}
		io.WriteString(k.Stdout, sb.String())
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

	// Create all the new pull requests first so we have the numbers.
	// We need the pull request numbers/URLs for the stack footer.
	isNew := make([]bool, len(plan))
	for i, pr := range plan {
		isNew[i] = pr.ID == nil
		if !isNew[i] {
			continue
		}

		if err := createPullRequest(ctx, gitHubClient, baseRepo, &pr.pullRequest); err != nil {
			return err
		}
		prNumberWidth = max(prNumberWidth, maxIntWidth(func(yield func(githubv4.Int) bool) {
			yield(pr.Number)
		}))
		sb := new(strings.Builder)
		pr.writeLogLine(sb, prNumberWidth, isStdoutTerminal)
		sb.WriteString(" (new)\n")
		io.WriteString(k.Stdout, sb.String())
	}

	// Now go through and update all the pull requests in the stack with the footer
	// (and base ref names).
	for i, pr := range plan {
		bodyBuilder := new(strings.Builder)
		bodyBuilder.WriteString(strings.TrimRight(string(pr.Body), "\n"))
		writeStackFooter(bodyBuilder, plan, i)
		pr.Body = githubv4.String(bodyBuilder.String())

		if err := updatePullRequest(ctx, gitHubClient, baseRepoPath, &pr.pullRequest); err != nil {
			return err
		}
		if !isNew[i] {
			if err := updatePullRequestDraftStatus(ctx, gitHubClient, baseRepoPath, &pr.pullRequest); err != nil {
				// Not a fatal error: the pull request still exists.
				log.Print(err)
			}

			sb := new(strings.Builder)
			pr.writeLogLine(sb, prNumberWidth, isStdoutTerminal)
			sb.WriteString("\n")
			io.WriteString(k.Stdout, sb.String())
		}
	}

	return nil
}

func (c *submitCmd) determineStackHead(ctx context.Context, jj *jujutsu.Jujutsu, baseRef jujutsu.RefSymbol, pushRemoteName string, pushOutput io.Writer) (bookmarks []*jujutsu.Bookmark, headBookmark string, err error) {
	if c.Bookmark != "" {
		var err error
		bookmarks, err = jj.ListBookmarks(ctx)
		return bookmarks, c.Bookmark, err
	}

	var head *jujutsu.Commit
	if c.shouldCreatePushBookmarks() {
		var err error
		head, err = c.pushChanges(ctx, jj, baseRef, pushRemoteName, pushOutput)
		if err != nil {
			return nil, "", err
		}
		if c.DryRun {
			// Bookmarks haven't been created, so we can't get the name.
			return nil, "", nil
		}
	} else {
		var err error
		head, err = c.findStackHeadFromRevisions(ctx, jj, baseRef)
		if err != nil {
			return nil, "", err
		}
	}

	// List bookmarks after potentially pushing changes.
	bookmarks, err = jj.ListBookmarks(ctx)
	if err != nil {
		return nil, "", err
	}
	headBookmark, err = nameForCommit(bookmarks, head.ID)
	if err != nil {
		return bookmarks, "", fmt.Errorf("find stack head: %v", err)
	}
	return bookmarks, headBookmark, nil
}

// shouldCreatePushBookmarks reports whether the options indicate whether "jj git push -c" will be run.
func (c *submitCmd) shouldCreatePushBookmarks() bool {
	return c.Bookmark == "" && len(c.Changes) > 0
}

// pushChanges runs "jj git push -c c.Changes"
// and returns the head commit from the revset defined by c.Changes.
func (c *submitCmd) pushChanges(ctx context.Context, jj *jujutsu.Jujutsu, baseRef jujutsu.RefSymbol, pushRemoteName string, pushOutput io.Writer) (*jujutsu.Commit, error) {
	revset := joinRevsets(c.Changes)
	if hasBase, err := isNonEmptyRevset(ctx, jj, revset+" & ::"+baseRef.String()); err != nil {
		return nil, fmt.Errorf("check for overlaps with %v: %v", baseRef, err)
	} else if hasBase {
		return nil, fmt.Errorf("changes overlap with %v", baseRef)
	}
	// Validate once without doing a network call.
	headRevset := "heads(" + revset + ")"
	head, err := singleCommitRevset(ctx, jj, headRevset)
	if err != nil {
		if errors.Is(err, errEmptyRevset) {
			err = errors.New("no changes found")
		}
		return nil, fmt.Errorf("find stack head: %v", err)
	}

	err = jjGitPush(ctx, jj, pushOutput, c.DryRun, pushRemoteName, func(yield func(string) bool) {
		yield("--change=" + revset)
	})
	if err != nil {
		return nil, err
	}
	if c.DryRun {
		return head, nil
	}

	// Add blank line to separate pull request output.
	io.WriteString(pushOutput, "\n")

	// We need to repeat this after the push in case the commit ID changed.
	head, err = singleCommitRevset(ctx, jj, headRevset)
	if err != nil {
		if errors.Is(err, errEmptyRevset) {
			err = errors.New("no changes found")
		}
		return nil, fmt.Errorf("find stack head: %v", err)
	}

	return head, nil
}

// findStackHeadFromRevisions returns the head commit from c.Revisions.
func (c *submitCmd) findStackHeadFromRevisions(ctx context.Context, jj *jujutsu.Jujutsu, baseRef jujutsu.RefSymbol) (*jujutsu.Commit, error) {
	var revset string
	if len(c.Revisions) > 0 {
		revset = joinRevsets(c.Revisions)
	} else {
		revset = "(" + baseRef.String() + "..@)"
	}
	var err error
	head, err := singleCommitRevset(ctx, jj, "heads(bookmarks() & "+revset+")")
	if err != nil {
		if errors.Is(err, errEmptyRevset) {
			err = errors.New("no bookmarks found")
		}
		return nil, fmt.Errorf("find stack head: %v", err)
	}
	return head, nil
}

// singleCommitRevset fetches the single commit the revset matches
// or returns an error if the revset does not match exactly one commit.
func singleCommitRevset(ctx context.Context, jj *jujutsu.Jujutsu, revset string) (*jujutsu.Commit, error) {
	var result *jujutsu.Commit
	multiple := false
	err := jj.Log(ctx, revset, func(c *jujutsu.Commit) bool {
		if result != nil {
			multiple = true
			return false
		}
		result = c
		return true
	})
	if err != nil {
		return nil, err
	}
	if multiple {
		return nil, errors.New("multiple found")
	}
	if result == nil {
		return nil, errEmptyRevset
	}
	return result, nil
}

// isNonEmptyRevset reports whether the revset matches at least one commit.
func isNonEmptyRevset(ctx context.Context, jj *jujutsu.Jujutsu, revset string) (bool, error) {
	nonEmpty := false
	err := jj.Log(ctx, revset, func(c *jujutsu.Commit) bool {
		nonEmpty = true
		return false
	})
	return nonEmpty, err
}

// errEmptyRevset is the error returned by [singleCommitRevset]
// when the revset does not match any commits.
var errEmptyRevset = errors.New("revset empty")

// jjGitPush runs the `jj git push` command.
func jjGitPush(ctx context.Context, jj *jujutsu.Jujutsu, w io.Writer, dryRun bool, pushRemoteName string, extraArgs iter.Seq[string]) error {
	pushArgs := []string{"git", "push"}
	if dryRun {
		pushArgs = append(pushArgs, "--dry-run")
	}
	pushArgs = append(pushArgs, "--remote="+pushRemoteName)
	pushArgs = slices.AppendSeq(pushArgs, extraArgs)
	if dryRun {
		fmt.Fprintf(w, "%% jj %s\n", strings.Join(pushArgs, " "))
	}
	err := jj.RunJJ(ctx, &jujutsu.Invocation{
		Args:   pushArgs,
		Stdout: w,
		Stderr: w,
	})
	if err != nil && !dryRun {
		return fmt.Errorf("jj git push --remote=%s: %v", pushRemoteName, err)
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
			// If we're pulling from a fork, then always use the base ref.
			prBase = baseRefName
		} else {
			prBase = stack[i-1].name
		}
		title, body := cutCommitDescription(bookmark.commit.Description)
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

func (pr *plannedPullRequest) writeLogLine(sb *strings.Builder, prNumberWidth int, link bool) {
	wroteLink := false
	if pr.ID == nil {
		sb.WriteString(prNumberPlaceholder(prNumberWidth))
	} else {
		formatted := formatPRNumber(pr.Number, prNumberWidth)
		if link && pr.URL.URL != nil {
			i := strings.IndexByte(formatted, '#')
			sb.WriteString(formatted[:i])
			sb.WriteString(osc + "8;;")
			sb.WriteString(pr.URL.String())
			sb.WriteString(st)
			sb.WriteString(formatted[i:])
			wroteLink = true
		} else {
			sb.WriteString(formatted)
		}
	}
	sb.WriteString(": ")
	if pr.IsDraft {
		sb.WriteString("[DRAFT] ")
	}
	sb.WriteString(string(pr.Title))
	if wroteLink {
		sb.WriteString(endLink)
	}

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

const (
	stackFooterMarker   = "<!-- jj-domino -->"
	stackFooterPreamble = "" +
		"\n\n" + stackFooterMarker + "\n" +
		"<!-- Do not remove this comment! Everything in this section will be rewritten by jj-domino. -->\n\n" +
		"## Related Pull Requests\n\n" +
		"This pull request is part of a stack managed by [jj-domino](https://github.com/zombiezen/jj-domino):\n\n"
)

// writeStackFooter writes a Markdown blurb to sb
// intended for the body of stack[i]
// with links to the other pull requests in the stack.
func writeStackFooter(sb *strings.Builder, stack []*plannedPullRequest, i int) {
	if len(stack) <= 1 {
		return
	}

	sb.WriteString(stackFooterPreamble)
	for j, pr := range stack {
		if j == i {
			fmt.Fprintf(sb, "% 2d. *→ this pull request ←*\n", j+1)
		} else {
			fmt.Fprintf(sb, "% 2d. #%d\n", j+1, pr.Number)
		}
	}
}

// trimStackFooter removes a blurb previously written by [writeStackFooter] from a string
// if present.
func trimStackFooter(s string) string {
	for lineStart := 0; lineStart < len(s); {
		var lineEnd int
		if i := strings.IndexByte(s[lineStart:], '\n'); i < 0 {
			lineEnd = len(s)
		} else {
			lineEnd = lineStart + i + 1
		}
		line := s[lineStart:lineEnd]

		if strings.TrimSpace(line) == stackFooterMarker {
			return s[:lineStart]
		}

		lineStart = lineEnd
	}
	return s
}

type localCommitRef struct {
	name   string
	commit *jujutsu.Commit
}

func stackForBookmark(ctx context.Context, jj *jujutsu.Jujutsu, bookmarks []*jujutsu.Bookmark, baseRef jujutsu.RefSymbol, bookmark string) ([]localCommitRef, error) {
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

	revset := baseRef.String() + ".." + headCommitID.String()
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
		return nil, fmt.Errorf("compute stack for %q: commit %v is ancestor of %v", bookmark, headCommitID, baseRef)
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
				// Base ref or base ref ancestor.
				continue
			}
			if name, err := nameForCommit(bookmarks, id); err != nil && !isNoBookmarksError(err) {
				resultError = errors.Join(resultError, err)
			} else if err == nil {
				stack = append(stack, localCommitRef{
					name:   name,
					commit: c,
				})
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

// nameForCommit finds a single local bookmark name for the given commit ID
// or returns an error if the commit does not resolve to exactly one bookmark.
func nameForCommit(bookmarks []*jujutsu.Bookmark, id jujutsu.CommitID) (string, error) {
	var names []string
	for _, b := range bookmarks {
		if target, ok := b.TargetMerge.Resolved(); b.Remote == "" && ok && target.Equal(id) {
			names = append(names, b.Name)
		}
	}
	switch len(names) {
	case 0:
		return "", noBookmarksError{id: id}
	case 1:
		return names[0], nil
	default:
		return "", fmt.Errorf("commit %v has multiple bookmarks (%s)", id, strings.Join(names, "|"))
	}
}

// noBookmarksError is returned if [nameForCommit] did not find any matching bookmarks.
type noBookmarksError struct {
	id jujutsu.CommitID
}

// isNoBookmarksError reports whether err is or wraps a [noBookmarksError].
func isNoBookmarksError(err error) bool {
	_, ok := errors.AsType[noBookmarksError](err)
	return ok
}

// Error implements [error].
func (err noBookmarksError) Error() string {
	return fmt.Sprintf("commit %v has no bookmarks", err.id)
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

// joinRevsets returns a revset that represents a union of the given revsets.
func joinRevsets(revsets []string) string {
	if len(revsets) == 0 {
		return "none()"
	}

	sb := new(strings.Builder)
	sb.WriteString("(")
	sb.WriteString(revsets[0])
	for _, r := range revsets[1:] {
		sb.WriteString(")|(")
		sb.WriteString(r)
	}
	sb.WriteString(")")
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
