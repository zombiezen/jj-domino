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
	"iter"
	"slices"

	"zombiezen.com/go/jj-domino/internal/jujutsu"
)

// stackedDiff represents a single local bookmark to be pushed
// as part of a pull request stack.
type stackedDiff struct {
	localCommitRef

	// uniqueAncestors is the set of commits beyond that referenced by the bookmark
	// that would be merged by the pull request.
	// Parents appear before children.
	uniqueAncestors []*jujutsu.Commit
}

// root returns the commit in the diff that is not a descendant of other commits in the diff.
func (diff *stackedDiff) root() *jujutsu.Commit {
	if len(diff.uniqueAncestors) > 0 {
		return diff.uniqueAncestors[0]
	}
	return diff.commit
}

// len returns the number of commits in the diff.
func (diff *stackedDiff) len() int {
	return len(diff.uniqueAncestors) + 1
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
