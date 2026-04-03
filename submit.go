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
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"

	"gg-scm.io/pkg/git"
	"github.com/alecthomas/kong"
	jsonv2 "github.com/go-json-experiment/json"
	"github.com/shurcooL/githubv4"
	"zombiezen.com/go/jj-domino/internal/jujutsu"
	"zombiezen.com/go/log"
)

type submitCmd struct {
	Bookmarks []string `kong:"name=bookmark,short=b,help=Push a stack with the bookmark as the head,placeholder=NAME,xor=revisions"`
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
	log.Debugf(ctx, "Remote: %s", pushRemoteName)
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
	log.Debugf(ctx, "Base ref: %v", baseRef)

	bookmarks, selectedBookmarkNames, err := c.determineBookmarkNames(ctx, jj, baseRef, pushRemoteName, pushOutput)
	if err != nil {
		return err
	}
	if len(selectedBookmarkNames) == 0 {
		// We don't have a head bookmark (jj-domino submit --change REVSET --dry-run),
		// so exit now.
		return nil
	}
	log.Debugf(ctx, "Bookmark: %v", selectedBookmarkNames)

	graph, err := graphFromRepository(ctx, jj, bookmarks, baseRef, selectedBookmarkNames)
	if err != nil {
		return err
	}

	gitRoot, err := jj.GitRoot(ctx)
	if err != nil {
		return err
	}
	log.Debugf(ctx, "Git repository at %s", gitRoot)
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
	log.Debugf(ctx, "remote.%s.url = %s", baseRef.Remote, baseRemote.FetchURL)
	baseRepoPath, err := gitHubRepositoryForURL(baseRemote.FetchURL)
	if err != nil {
		return fmt.Errorf("base remote: %v", err)
	}
	pushRemote := remotes[pushRemoteName]
	if pushRemote == nil {
		return fmt.Errorf("unknown remote %s from git.push", pushRemoteName)
	}
	log.Debugf(ctx, "remote.%s.pushurl = %s", pushRemoteName, pushRemote.PushURL)
	headRepoPath, err := gitHubRepositoryForURL(pushRemote.PushURL)
	if err != nil {
		return fmt.Errorf("push remote: %v", err)
	}
	templateRevision := "latest(" + joinRevsets(func(yield func(string) bool) {
		for head := range graph.heads() {
			if !yield(head.commit.ID.String()) {
				return
			}
		}
	}) + ")"
	pullRequestTemplate := readGitHubPullRequestTemplate(ctx, jj, templateRevision)

	token, err := gitHubToken(ctx, global.environ, global.lookPath)
	if err != nil {
		if !c.DryRun {
			return err
		}
		// If we're doing a dry run, don't worry about GitHub API.
		// We can still display the proposed PRs.
		log.Warnf(ctx, "Unable to authenticate to GitHub: %v", err)

		sb := new(strings.Builder)
		headRepo := placeholderGitHubRepository(headRepoPath)
		for diff := range graph.walk() {
			pr := pullRequestFromStackedDiff(githubv4.String(baseRef.Name), headRepo, diff)
			if slices.Contains(graph.roots, diff) {
				pr.IsDraft = githubv4.Boolean(c.Draft)
			}
			writeLogLine(sb, pr, logLineOptions{
				baseRepositoryPath: baseRepoPath,
				prNumberWidth:      defaultPRNumberWidth,
			})
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

	var baseRepo *gitHubRepository
	newDiffs := make(map[*stackedDiff]struct{})
	for diff := range graph.walk() {
		newBaseRepo, existingPR, err := findOpenPullRequestForHead(ctx, gitHubClient, baseRepoPath, headRepoPath, diff.name)
		baseRepo = cmp.Or(newBaseRepo, baseRepo)
		if err != nil && !errors.Is(err, errPullRequestNotFound) {
			planError = errors.Join(planError, err)
			continue
		}
		if existingPR == nil {
			newDiffs[diff] = struct{}{}
			diff.pullRequest = pullRequestFromStackedDiff(githubv4.String(baseRef.Name), headRepo, diff)
			if pullRequestTemplate != "" {
				diff.pullRequest.Body += githubv4.String("\n\n" + pullRequestTemplate)
			}
			log.Debugf(ctx, "Will create new pull request for %s", diff.name)
		} else {
			existingPR.Body = githubv4.String(trimStackFooter(string(existingPR.Body)))
			diff.pullRequest = existingPR
			log.Debugf(ctx, "Will reuse pull request %v#%d for %s",
				baseRepo.path(), existingPR.Number, diff.name)
		}
	}
	if planError != nil {
		return planError
	}
	for diff := range graph.walk() {
		diff.pullRequest.IsDraft = githubv4.Boolean(c.Draft || len(diff.parents) > 0)
	}

	// Edit pull request titles/bodies.
	if c.DryRun {
		// In a dry run, we should surface to the user that an editor might fail
		// and have unexpected consequences.
		if _, err := jujutsuEditor(jjSettings); err != nil {
			log.Warnf(ctx, "Unable to determine editor: %v", err)
		}
	} else if c.Editor != nil && *c.Editor || c.Editor == nil && len(newDiffs) > 0 {
		if editorCommand, err := jujutsuEditor(jjSettings); err != nil {
			if c.Editor != nil {
				// User explicitly requested editor. Abort.
				return err
			}
			log.Warnf(ctx, "Unable to determine editor (%v). Sending without editing...", err)
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
					log.Errorf(ctx, "%v", err)
				},
			}
			var prsToEdit []*pullRequest
			for diff := range graph.walk() {
				// If the user didn't explicitly request an editor,
				// only surface the new PRs.
				if _, isNew := newDiffs[diff]; isNew || c.Editor != nil {
					prsToEdit = append(prsToEdit, diff.pullRequest)
				}
			}
			editError := editPullRequestMessages(prsToEdit, func(initialContent []byte) ([]byte, error) {
				log.Infof(ctx, "Opening editor for pull request descriptions...")
				return e.open(ctx, "domino.jjdescription", initialContent)
			})
			if editError != nil {
				if content, ok := failedEditorContent(editError); ok {
					tempPath, err := writeTempFile("", "jj-domino-*.jjdescription", content)
					if err != nil {
						log.Warnf(ctx, "Tried to save editor content: %v", err)
					} else {
						editError = fmt.Errorf("%v (wrote content to %s)", editError, tempPath)
					}
				}
				return editError
			}
		}
	}

	// If we haven't already run push, then do so (in dry-run mode if requested).
	if c.Push && !c.shouldCreatePushBookmarks() {
		err := jjGitPush(ctx, jj, pushOutput, c.DryRun, pushRemoteName, func(yield func(string) bool) {
			for diff := range graph.walk() {
				if !yield("--bookmark=exact:" + jujutsu.Quote(string(diff.name))) {
					return
				}
			}
		})
		if err != nil {
			return err
		}
		io.WriteString(pushOutput, "\n")
	}

	logOptions := logLineOptions{
		baseRepositoryPath: baseRepoPath,
		link:               useANSIEscapes(k.Stdout, lookupEnvMapFunc(global.environ)),
		prNumberWidth: cmp.Or(maxIntWidth(func(yield func(githubv4.Int) bool) {
			for diff := range graph.walk() {
				if _, isNew := newDiffs[diff]; !isNew {
					if !yield(diff.pullRequest.Number) {
						return
					}
				}
			}
		}), defaultPRNumberWidth),
	}
	if c.DryRun {
		sb := new(strings.Builder)
		for diff := range graph.walk() {
			writeLogLine(sb, diff.pullRequest, logOptions)
			if _, isNew := newDiffs[diff]; isNew {
				sb.WriteString(" (new)")
			}
			sb.WriteString("\n")
		}
		io.WriteString(k.Stdout, sb.String())
		return nil
	}

	// Create all the new pull requests first so we have the numbers.
	// We need the pull request numbers/URLs for the stack footer.
	for diff := range graph.walk() {
		if _, isNew := newDiffs[diff]; !isNew {
			continue
		}

		if err := createPullRequest(ctx, gitHubClient, baseRepo, diff.pullRequest); err != nil {
			return err
		}
		logOptions.prNumberWidth = max(logOptions.prNumberWidth, maxIntWidth(func(yield func(githubv4.Int) bool) {
			yield(diff.pullRequest.Number)
		}))
		sb := new(strings.Builder)
		writeLogLine(sb, diff.pullRequest, logOptions)
		sb.WriteString(" (new)\n")
		io.WriteString(k.Stdout, sb.String())
	}

	// Now go through and update all the pull requests in the stack with the footer
	// (and base ref names).
	for diff := range graph.walk() {
		bodyBuilder := new(strings.Builder)
		bodyBuilder.WriteString(strings.TrimRight(string(diff.pullRequest.Body), "\n"))
		writeStackFooter(bodyBuilder, diff)
		diff.pullRequest.Body = githubv4.String(bodyBuilder.String())

		if err := updatePullRequest(ctx, gitHubClient, baseRepoPath, diff.pullRequest); err != nil {
			return err
		}
		if _, isNew := newDiffs[diff]; !isNew {
			if err := updatePullRequestDraftStatus(ctx, gitHubClient, baseRepoPath, diff.pullRequest); err != nil {
				// Not a fatal error: the pull request still exists.
				log.Warnf(ctx, "%v", err)
			}

			sb := new(strings.Builder)
			writeLogLine(sb, diff.pullRequest, logOptions)
			sb.WriteString("\n")
			io.WriteString(k.Stdout, sb.String())
		}
	}

	return nil
}

func (c *submitCmd) determineBookmarkNames(ctx context.Context, jj *jujutsu.Jujutsu, baseRef jujutsu.RefSymbol, pushRemoteName string, pushOutput io.Writer) (allBookmarks []*jujutsu.Bookmark, selectedBookmarkNames []string, err error) {
	if len(c.Bookmarks) > 0 {
		var err error
		allBookmarks, err = jj.ListBookmarks(ctx)
		return allBookmarks, c.Bookmarks, err
	}

	var revset string
	if c.shouldCreatePushBookmarks() {
		if err := c.pushChanges(ctx, jj, baseRef, pushRemoteName, pushOutput); err != nil {
			return nil, nil, err
		}
		if c.DryRun {
			// Bookmarks haven't been created, so we can't get the name.
			return nil, nil, nil
		}
		revset = joinRevsets(slices.Values(c.Changes))
	} else if len(c.Revisions) > 0 {
		revset = joinRevsets(slices.Values(c.Revisions))
	} else {
		revset = "(" + baseRef.String() + "..@)"
	}

	// List bookmarks *after* potentially pushing changes.
	allBookmarks, err = jj.ListBookmarks(ctx)
	if err != nil {
		return allBookmarks, nil, err
	}

	logOptions := jujutsu.LogOptions{
		Revset: revset,
	}
	var resultError error
	err = jj.Log(ctx, logOptions, func(c *jujutsu.Commit) bool {
		b, err := bookmarkForCommit(allBookmarks, c.ID, nil)
		if err != nil {
			if isNoBookmarksError(err) {
				return true
			} else {
				resultError = errors.Join(resultError, err)
				return false
			}
		}
		selectedBookmarkNames = append(selectedBookmarkNames, b.Name)
		return true
	})
	resultError = errors.Join(resultError, err)
	if resultError == nil && len(selectedBookmarkNames) == 0 {
		resultError = fmt.Errorf("bookmarks() & (%s) did not match any changes", joinRevsets(slices.Values(selectedBookmarkNames)))
	}

	return allBookmarks, selectedBookmarkNames, resultError
}

// shouldCreatePushBookmarks reports whether the options indicate whether "jj git push -c" will be run.
func (c *submitCmd) shouldCreatePushBookmarks() bool {
	return len(c.Bookmarks) == 0 && len(c.Changes) > 0
}

// pushChanges runs "jj git push -c c.Changes"
// and returns the commits from the revset defined by c.Changes.
func (c *submitCmd) pushChanges(ctx context.Context, jj *jujutsu.Jujutsu, baseRef jujutsu.RefSymbol, pushRemoteName string, pushOutput io.Writer) error {
	revset := joinRevsets(slices.Values(c.Changes))
	if hasBase, err := isNonEmptyRevset(ctx, jj, revset+" & ::"+baseRef.String()); err != nil {
		return fmt.Errorf("check for overlaps with %v: %v", baseRef, err)
	} else if hasBase {
		return fmt.Errorf("changes overlap with %v", baseRef)
	}

	err := jjGitPush(ctx, jj, pushOutput, c.DryRun, pushRemoteName, func(yield func(string) bool) {
		yield("--change=" + revset)
	})
	if err != nil {
		return err
	}

	if !c.DryRun {
		// Add blank line to separate pull request output.
		io.WriteString(pushOutput, "\n")
	}
	return nil
}

func pullRequestFromStackedDiff(baseRefName githubv4.String, headRepository *gitHubRepository, diff *stackedDiff) *pullRequest {
	title, body := inferPullRequestMessage(func(yield func(string) bool) {
		for c := range diff.commitsBackward() {
			if !yield(c.Description) {
				return
			}
		}
	})
	return &pullRequest{
		BaseRefName:    baseRefName,
		HeadRepository: headRepository,
		HeadRefName:    githubv4.String(diff.name),

		Title:   githubv4.String(title),
		Body:    githubv4.String(body),
		IsDraft: len(diff.parents) > 0,
	}
}

type logLineOptions struct {
	baseRepositoryPath gitHubRepositoryPath
	prNumberWidth      int
	link               bool
}

func writeLogLine(sb *strings.Builder, pr *pullRequest, opts logLineOptions) {
	wroteLink := false
	if pr.ID == nil {
		sb.WriteString(prNumberPlaceholder(opts.prNumberWidth))
	} else {
		formatted := formatPRNumber(pr.Number, opts.prNumberWidth)
		if opts.link && pr.URL.URL != nil {
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
	if pr.HeadRepository.path() == opts.baseRepositoryPath {
		sb.WriteString(string(pr.BaseRefName))
		sb.WriteString(" ← ")
		sb.WriteString(string(pr.HeadRefName))
	} else {
		sb.WriteString(opts.baseRepositoryPath.Owner)
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
		"<!-- Do not remove this comment! Everything in this section will be rewritten by jj-domino. -->" +
		"\n\n"
	stackFooterChangesSection = "" +
		"## How to Review\n\n" +
		"This pull request is a draft because it is intended to be merged after %s. " +
		"The new changes are in the [last %s](%s)." +
		"\n\n"
	stackFooterRelatedSectionIntro = "" +
		"## Related Pull Requests\n\n" +
		"This pull request is part of a stack managed by [jj-domino](https://github.com/zombiezen/jj-domino):\n\n"
	stackFooterParentsIntro  = "This pull request comes after:\n\n"
	stackFooterChildrenIntro = "After this pull request:\n\n"
)

// writeStackFooter writes a Markdown blurb to sb
// intended for the body of diff
// with a link to display the unique delta
// and links to the other pull requests in the stack.
func writeStackFooter(sb *strings.Builder, diff *stackedDiff) {
	if len(diff.children) == 0 && len(diff.parents) == 0 {
		return
	}

	sb.WriteString(stackFooterPreamble)
	writeStackFooterChanges(sb, diff)
	sb.WriteString(stackFooterRelatedSectionIntro)

	if len(diff.parents) > 1 {
		writeStackFooterList(sb, stackFooterParentsIntro, func(yield func(*pullRequest) bool) {
			for _, parent := range diff.parents {
				if !yield(parent.pullRequest) {
					return
				}
			}
		})
		if len(diff.children) > 0 {
			sb.WriteString("\n")
		}
		writeStackFooterChildren(sb, diff)
		return
	}

	var list []*stackedDiff
	ancestorsDiverge := false
	if len(diff.parents) == 1 {
		for curr := diff.parents[0]; ; {
			list = append(list, curr)
			if len(curr.parents) != 1 {
				ancestorsDiverge = len(curr.parents) > 1
				break
			}
			curr = curr.parents[0]
		}
		slices.Reverse(list)
	}
	list = append(list, diff)

	descendantsDiverge := len(diff.children) > 1
	if len(diff.children) == 1 {
		for curr := diff.children[0]; ; {
			list = append(list, curr)
			if len(curr.children) != 1 {
				descendantsDiverge = len(curr.children) > 1
				break
			}
			curr = curr.children[0]
		}
	}

	if len(list) > 1 {
		if ancestorsDiverge {
			sb.WriteString("- …multiple pull requests…\n")
		}
		for i, other := range list {
			if ancestorsDiverge {
				sb.WriteString("- ")
			} else {
				fmt.Fprintf(sb, "% 2d. ", i+1)
			}
			if other == diff {
				sb.WriteString("*→ this pull request ←*\n")
			} else {
				fmt.Fprintf(sb, "#%d\n", other.pullRequest.Number)
			}
		}
	}
	if descendantsDiverge {
		if list[len(list)-1] == diff {
			if len(list) > 1 {
				sb.WriteString("\n")
			}
			writeStackFooterChildren(sb, diff)
		} else {
			if ancestorsDiverge {
				sb.WriteString("-")
			} else {
				fmt.Fprintf(sb, "% 2d.", len(list)+1)
			}
			sb.WriteString(" …multiple pull requests…\n")
		}
	}
}

// writeStackFooterChanges writes the "View Changes" section to sb.
func writeStackFooterChanges(sb *strings.Builder, diff *stackedDiff) {
	var parentsString string
	switch len(diff.parents) {
	case 0:
		return
	case 1:
		parentsString = fmt.Sprintf("#%d", diff.parents[0].pullRequest.Number)
	case 2:
		parentsString = fmt.Sprintf("#%d and #%d", diff.parents[0].pullRequest.Number, diff.parents[1].pullRequest.Number)
	default:
		sb := new(strings.Builder)
		for i, parent := range diff.parents {
			if i == len(diff.parents)-1 {
				sb.WriteString(", and ")
			} else if i > 0 {
				sb.WriteString(", ")
			}
			fmt.Fprintf(sb, "#%d", parent.pullRequest.Number)
		}
		parentsString = sb.String()
	}
	var commitsPhrase string
	if n := diff.len(); n == 1 {
		commitsPhrase = "commit"
	} else {
		commitsPhrase = fmt.Sprintf("%d commits", n)
	}
	fmt.Fprintf(sb, stackFooterChangesSection,
		parentsString,
		commitsPhrase,
		diff.pullRequest.changesURL(diff.root().ID, diff.commit.ID),
	)
}

// writeStackFooterChildren writes the list of immediate child pull requests of diff to sb.
func writeStackFooterChildren(sb *strings.Builder, diff *stackedDiff) {
	writeStackFooterList(sb, stackFooterChildrenIntro, func(yield func(*pullRequest) bool) {
		for _, child := range diff.children {
			if !yield(child.pullRequest) {
				return
			}
		}
	})
}

// writeStackFooterList writes a unordered list of links to pull requests
// with an introduction to sb.
// If prs is empty, then nothing is written.
func writeStackFooterList(sb *strings.Builder, intro string, prs iter.Seq[*pullRequest]) {
	nextPR, stop := iter.Pull(prs)
	defer stop()

	pr, ok := nextPR()
	if !ok {
		return
	}
	sb.WriteString(intro)
	for ok {
		fmt.Fprintf(sb, "- #%d\n", pr.Number)
		pr, ok = nextPR()
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

func writeTempFile(dir string, pattern string, content []byte) (string, error) {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	_, err = f.Write(content)
	err = errors.Join(err, f.Close())
	path := f.Name()
	if err != nil {
		err = fmt.Errorf("write %s: %v", path, err)
	}
	return path, err
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
