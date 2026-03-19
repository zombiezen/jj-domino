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
	"maps"
	"slices"
	"strconv"
	"strings"

	"gg-scm.io/pkg/git"
	"github.com/alecthomas/kong"
	jsonv2 "github.com/go-json-experiment/json"
	"github.com/shurcooL/githubv4"
	"zombiezen.com/go/jj-domino/internal/jujutsu"
)

type submitCmd struct {
	Bookmark  string   `kong:"short=b,help=Push a stack with the bookmark as the head,placeholder=NAME,xor=revisions"`
	Revisions []string `kong:"short=r,sep=none,help=Push stacks pointing to these commits (can be repeated) (default: trunk()..@),placeholder=REVSETS,xor=revisions"`
	Changes   []string `kong:"name=change,short=c,sep=none,help=Push stacks by creating bookmarks (can be repeated),placeholder=REVSETS,xor=revisions"`
	Editor    *bool    `kong:"negatable,help=Whether to open an editor on the pull request descriptions (defaults to only new PRs)"`
	Draft     bool     `kong:"short=d,help=Mark first pull request in stack as draft"`
	DryRun    bool     `kong:"short=n,help=Don\\'t send to GitHub"`
	Remote    string   `kong:"help=The remote to push to,placeholder=REMOTE"`
	Base      string   `kong:"help=Base remote bookmark to open pull requests against (default: trunk()),placeholder=BOOKMARK@REMOTE"`
	Push      bool     `kong:"negatable,help=Push commits to GitHub (on by default),default=true"`
}

func (c *submitCmd) Validate() error {
	if c.shouldCreatePushBookmarks() && !c.Push {
		return errors.New("cannot combine --no-push with --change")
	}
	if c.DryRun && c.Editor != nil && *c.Editor {
		return errors.New("cannot combine --editor with --dry-run")
	}
	return nil
}

func (c *submitCmd) Run(ctx context.Context, k *kong.Kong, global *cli) error {
	const defaultPRNumberWidth = 3

	jj, err := global.newJujutsu()
	if err != nil {
		return err
	}
	jjSettings, err := jj.ReadSettings(ctx)
	if err != nil {
		return err
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
		// TODO(someday): Deduplicate bookmark listing.
		bookmarks, err := jj.ListBookmarks(ctx)
		if err != nil {
			return err
		}
		baseRef, err = resolveTrunk(jjSettings, bookmarks)
		if err != nil {
			return err
		}
		if baseRef.Remote == "" {
			return fmt.Errorf("trunk() (%v) does not have an associated remote", baseRef)
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
	pullRequestTemplate := readGitHubPullRequestTemplate(ctx, jj, stack[len(stack)-1].commit.ID.String())

	token, err := gitHubToken(ctx, global.environ, global.lookPath)
	if err != nil {
		if !c.DryRun {
			return err
		}
		// If we're doing a dry run, don't worry about GitHub API.
		// We can still display the proposed PRs.
		log.Printf("Unable to authenticate to GitHub: %v", err)

		plan := planPullRequests(baseRepoPath, baseRef.Name, pullRequestTemplate, placeholderGitHubRepository(headRepoPath), stack)
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

	plan := planPullRequests(baseRepoPath, baseRef.Name, pullRequestTemplate, headRepo, stack)
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

	// Edit pull request titles/bodies.
	isNew := make([]bool, len(plan))
	for i, pr := range plan {
		isNew[i] = pr.ID == nil
	}
	if c.DryRun {
		// In a dry run, we should surface to the user that an editor might fail
		// and have unexpected consequences.
		if _, err := jujutsuEditor(jjSettings); err != nil {
			log.Printf("Warning: unable to determine editor: %v", err)
		}
	} else if c.Editor != nil && *c.Editor || c.Editor == nil && slices.Contains(isNew, true) {
		if editorCommand, err := jujutsuEditor(jjSettings); err != nil {
			if c.Editor != nil {
				// User explicitly requested editor. Abort.
				return err
			}
			log.Printf("Unable to determine editor (%v). Sending without editing...", err)
		} else {
			argv := editorCommand.Argv()
			editorEnviron := maps.Clone(global.environ)
			maps.Insert(editorEnviron, editorCommand.Environ())
			e := &editor{
				command: jujutsu.CommandArgv(argv[0], argv[1:], editorEnviron),
				stdin:   global.stdin,
				stdout:  k.Stdout,
				stderr:  k.Stderr,
				logError: func(ctx context.Context, err error) {
					log.Println(err)
				},
			}
			prsToEdit := plan
			if c.Editor == nil {
				// If the user didn't explicitly request an editor,
				// only surface the new PRs.
				prsToEdit = make([]*plannedPullRequest, 0, len(plan))
				for i, pr := range plan {
					if isNew[i] {
						prsToEdit = append(prsToEdit, pr)
					}
				}
			}
			err := editPullRequestMessages(prsToEdit, func(initialContent []byte) ([]byte, error) {
				log.Println("Opening editor for pull request descriptions...")
				return e.open(ctx, "domino.jjdescription", initialContent)
			})
			if err != nil {
				return err
			}
		}
	}

	// If we haven't already run push, then do so (in dry-run mode if requested).
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

	isStdoutTerminal := isTerminal(k.Stdout)
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
	for i, pr := range plan {
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

type plannedPullRequest struct {
	pullRequest
	baseRepositoryPath gitHubRepositoryPath
}

func planPullRequests(baseRepoPath gitHubRepositoryPath, baseRefName string, template string, headRepo *gitHubRepository, stack []stackedDiff) []*plannedPullRequest {
	plan := make([]*plannedPullRequest, 0, len(stack))
	isFork := headRepo.path() != baseRepoPath
	for i, diff := range stack {
		var prBase string
		if i == 0 || isFork {
			// A pull request's base ref must be in the pull request's repository.
			// If we're pulling from a fork, then always use the base ref.
			prBase = baseRefName
		} else {
			prBase = stack[i-1].name
		}
		title, body := inferPullRequestMessage(func(yield func(string) bool) {
			for c := range diff.commitsBackward() {
				if !yield(c.Description) {
					return
				}
			}
		})
		if template != "" {
			body += "\n\n" + template
		}
		plan = append(plan, &plannedPullRequest{
			baseRepositoryPath: baseRepoPath,
			pullRequest: pullRequest{
				BaseRefName:    githubv4.String(prBase),
				HeadRepository: headRepo,
				HeadRefName:    githubv4.String(diff.name),

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

// stackedDiff represents a single local bookmark to be pushed
// as part of a pull request stack.
type stackedDiff struct {
	localCommitRef

	// uniqueAncestors is the set of commits beyond that referenced by the bookmark
	// that would be merged by the pull request.
	// Parents appear before children.
	uniqueAncestors []*jujutsu.Commit
}

func stackForBookmark(ctx context.Context, jj *jujutsu.Jujutsu, bookmarks []*jujutsu.Bookmark, baseRef jujutsu.RefSymbol, bookmark string) ([]stackedDiff, error) {
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

	stack := []stackedDiff{{
		localCommitRef: localCommitRef{
			name:   bookmark,
			commit: headCommit,
		},
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
			if name, err := nameForCommit(bookmarks, id); isNoBookmarksError(err) {
				top := &stack[len(stack)-1]
				top.uniqueAncestors = append(top.uniqueAncestors, c)
			} else if err != nil {
				resultError = errors.Join(resultError, err)
			} else {
				stack = append(stack, stackedDiff{
					localCommitRef: localCommitRef{
						name:   name,
						commit: c,
					},
				})
			}
			next = append(next, c.Parents...)
		}
		curr = next
	}

	slices.Reverse(stack)
	for i := range stack {
		slices.Reverse(stack[i].uniqueAncestors)
	}
	if resultError != nil {
		resultError = fmt.Errorf("compute stack for %q: %w", bookmark, resultError)
	}
	return stack, resultError
}

// commitsBackward returns an iterator over all the commits in the diff.
// The iterator will yield child commits before their parents.
func (diff *stackedDiff) commitsBackward() iter.Seq[*jujutsu.Commit] {
	return func(yield func(*jujutsu.Commit) bool) {
		if !yield(diff.commit) {
			return
		}
		for _, c := range slices.Backward(diff.uniqueAncestors) {
			if !yield(c) {
				return
			}
		}
	}
}

func inferPullRequestMessage(descriptions iter.Seq[string]) (title, body string) {
	bodyBuilder := new(strings.Builder)
	i := 0
	for msg := range descriptions {
		if i == 0 {
			// First line of first commit message is the title.
			if j := strings.IndexByte(msg, '\n'); j != -1 {
				title = strings.TrimSpace(msg[:j])
				bodyBuilder.WriteString(strings.TrimSpace(msg[j+1:]))
			} else {
				title = strings.TrimSpace(msg)
			}
			i++
			continue
		}
		// Join rest of messages by bullets into body.
		bodyBuilder.WriteString("\n\n* ")
		bodyBuilder.WriteString(strings.TrimSpace(msg))
		i++
	}
	body = strings.TrimSpace(bodyBuilder.String())
	return title, body
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
