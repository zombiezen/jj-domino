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
	"iter"
	"slices"
	"strings"

	"zombiezen.com/go/jj-domino/internal/jujutsu"
)

// localCommitRef represents a local bookmark.
type localCommitRef struct {
	name   string
	commit *jujutsu.Commit
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
