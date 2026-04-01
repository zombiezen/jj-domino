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

	jsonv2 "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
	"zombiezen.com/go/jj-domino/internal/jujutsu"
	"zombiezen.com/go/log"
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
	opts := jujutsu.LogOptions{Revset: revset}
	err := jj.Log(ctx, opts, func(c *jujutsu.Commit) bool {
		if result != nil {
			log.Debugf(ctx, "Found %v and %v for %s", c, result, revset)
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
	opts := jujutsu.LogOptions{Revset: revset}
	err := jj.Log(ctx, opts, func(c *jujutsu.Commit) bool {
		log.Debugf(ctx, "Found %v is in %s", c.ID, revset)
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

// resolveTrunk attempts to determine the [jujutsu.RefSymbol] that the trunk() revset uses.
// This isn't a well-defined operation in Jujutsu because trunk() is a revset,
// so resolveTrunk makes a best effort to resolve most common scenarios.
func resolveTrunk(settings map[string]jsontext.Value, bookmarks []*jujutsu.Bookmark) (jujutsu.RefSymbol, error) {
	const key = `revset-aliases."trunk()"`
	if definition := settings[key]; len(definition) > 0 {
		var revset string
		if err := jsonv2.Unmarshal(definition, &revset); err != nil {
			return jujutsu.RefSymbol{}, fmt.Errorf("parse jj config get %s: %v", key, err)
		}
		refSymbol, err := jujutsu.ParseRefSymbol(revset)
		if err != nil {
			return jujutsu.RefSymbol{}, fmt.Errorf("parse jj config get %s: %s: %v", key, revset, err)
		}
		return refSymbol, nil
	}

	// Built-in trunk() is defined here:
	// https://github.com/jj-vcs/jj/blob/v0.39.0/cli/src/config/revsets.toml#L21-L31
	possible := make([]*jujutsu.Bookmark, 0, 6)
	for _, b := range bookmarks {
		if _, ok := b.TargetMerge.Resolved(); !ok {
			continue
		}
		if (b.Name == "main" || b.Name == "master" || b.Name == "trunk") &&
			(b.Remote == "origin" || b.Remote == "upstream") {
			possible = append(possible, b)
		}
	}
	switch {
	case len(possible) == 0:
		return jujutsu.RefSymbol{}, fmt.Errorf("resolve trunk(): no suitable bookmarks found")
	case len(possible) > 1:
		// TODO(maybe): Jujutsu uses the latest(...) revset function
		// to disambiguate.
		// This requires a `jj log` for us, and it's unclear whether this is desirable.
		// For now, just return an error, and make the user set a trunk().
		sb := new(strings.Builder)
		sb.WriteString("resolve trunk(): multiple found (")
		for i, b := range bookmarks {
			if i > 0 {
				sb.WriteString("|")
			}
			sb.WriteString(b.RefSymbol().String())
		}
		sb.WriteString(")")
		return jujutsu.RefSymbol{}, errors.New(sb.String())
	}
	return possible[0].RefSymbol(), nil
}

// bookmarkForCommit finds a single local bookmark for the given commit ID
// or returns an error if the commit does not resolve to exactly one bookmark.
// If there are multiple bookmarks that match,
// then preferredBookmarkNames is used to attempt to select a single one.
func bookmarkForCommit(bookmarks []*jujutsu.Bookmark, id jujutsu.CommitID, preferredBookmarkNames iter.Seq[string]) (*jujutsu.Bookmark, error) {
	possible := make([]*jujutsu.Bookmark, 0, len(bookmarks))
	for _, b := range bookmarks {
		if target, ok := b.TargetMerge.Resolved(); b.Remote == "" && ok && target.Equal(id) {
			possible = append(possible, b)
		}
	}
	switch len(possible) {
	case 0:
		return nil, noBookmarksError{id: id}
	case 1:
		return possible[0], nil
	default:
		var selected *jujutsu.Bookmark
		for preferredName := range preferredBookmarkNames {
			i := slices.IndexFunc(bookmarks, func(b *jujutsu.Bookmark) bool {
				return b.Name == preferredName
			})
			if i >= 0 {
				if selected != nil {
					selected = nil
					break
				}
				selected = bookmarks[i]
			}
		}
		if selected == nil {
			revset := joinRevsets(func(yield func(string) bool) {
				for _, b := range possible {
					if !yield(b.RefSymbol().String()) {
						return
					}
				}
			})
			// revset is always parenthesized.
			return nil, fmt.Errorf("commit %v has multiple bookmarks %s", id, revset)
		}
		return selected, nil
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
func joinRevsets(revsets iter.Seq[string]) string {
	next, stop := iter.Pull(revsets)
	defer stop()

	r0, ok := next()
	if !ok {
		return "none()"
	}
	r1, ok := next()
	if !ok {
		return "(" + r0 + ")"
	}

	sb := new(strings.Builder)
	sb.WriteString("((")
	sb.WriteString(r0)
	sb.WriteString(")|(")
	sb.WriteString(r1)
	for {
		r, ok := next()
		if !ok {
			break
		}
		sb.WriteString(")|(")
		sb.WriteString(r)
	}
	sb.WriteString("))")
	return sb.String()
}
