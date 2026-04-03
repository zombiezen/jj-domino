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
	"strings"

	"zombiezen.com/go/jj-domino/internal/commitgraph"
	"zombiezen.com/go/jj-domino/internal/jujutsu"
)

// diffGraph is a collection of [*stackedDiff] nodes.
type diffGraph struct {
	roots []*stackedDiff
}

func (g diffGraph) byName(name string) *stackedDiff {
	for diff := range g.walk() {
		if diff.name == name {
			return diff
		}
	}
	return nil
}

func (g diffGraph) walk() iter.Seq[*stackedDiff] {
	return walkGraph(g.roots)
}

func (g diffGraph) heads() iter.Seq[*stackedDiff] {
	return heads(g.roots)
}

// stackedDiff represents a single local bookmark to be pushed
// as part of a [diffGraph].
type stackedDiff struct {
	localCommitRef

	// uniqueAncestors is the set of commits beyond that referenced by the bookmark
	// that would be merged by the pull request.
	// Parents appear before children.
	uniqueAncestors []*jujutsu.Commit

	pullRequest *pullRequest

	parents  []*stackedDiff
	children []*stackedDiff
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

func graphFromRepository(ctx context.Context, jj *jujutsu.Jujutsu, allBookmarks []*jujutsu.Bookmark, baseRef jujutsu.RefSymbol, selectedBookmarkNames []string) (_ diffGraph, err error) {
	selectedBookmarks, err := selectBookmarks(allBookmarks, selectedBookmarkNames)
	if err != nil {
		return diffGraph{}, err
	}

	revset := new(strings.Builder)
	revset.WriteString(baseRef.String())
	revset.WriteString("..(")
	for i, b := range selectedBookmarks {
		if i > 0 {
			revset.WriteString("|")
		}
		revset.WriteString(b.RefSymbol().String())
	}
	revset.WriteString(")")

	commits, err := commitgraph.FromRevset(ctx, jj, revset.String())
	if err != nil {
		return diffGraph{}, err
	}
	return newDiffGraph(commits, allBookmarks, selectedBookmarks)
}

// newDiffGraph returns a new [diffGraph] from the given non-trunk() commits and bookmarks.
func newDiffGraph(commits *commitgraph.Graph, allBookmarks, selectedBookmarks []*jujutsu.Bookmark) (_ diffGraph, err error) {
	commitToDiff := make(map[string]*stackedDiff, commits.Len())
	diffForName := func(name string, commit *jujutsu.Commit) (*stackedDiff, error) {
		if diff := commitToDiff[string(commit.ID)]; diff != nil {
			if diff.name != name {
				return nil, fmt.Errorf("%s and %s refer to the same commit (%v)", name, diff.name, commit.ID)
			}
			return diff, nil
		}
		diff := &stackedDiff{
			localCommitRef: localCommitRef{
				name:   name,
				commit: commit,
			},
		}
		commitToDiff[string(commit.ID)] = diff
		return diff, nil
	}

	// Sort commits early: we don't want to infinite loop.
	sortedCommits, err := commits.Sort()
	if err != nil {
		return diffGraph{}, err
	}

	bookmarksStack := slices.Clone(selectedBookmarks)
	slices.Reverse(bookmarksStack) // Visit bookmarks in given order.
	var resultError error
	for len(bookmarksStack) > 0 {
		b := bookmarksStack[len(bookmarksStack)-1]
		bookmarksStack = slices.Delete(bookmarksStack, len(bookmarksStack)-1, len(bookmarksStack))

		headCommitID, ok := b.TargetMerge.Resolved()
		if !ok {
			resultError = errors.Join(resultError, fmt.Errorf("unresolved bookmark %v", b.RefSymbol()))
			continue
		}
		headCommit, ok := commits.Get(headCommitID)
		if !ok {
			resultError = errors.Join(resultError, fmt.Errorf("%v (%v) is ancestor of base", b.RefSymbol(), headCommitID))
			continue
		}
		diff, err := diffForName(b.Name, headCommit)
		if err != nil {
			resultError = errors.Join(resultError, err)
			continue
		}
		if len(diff.uniqueAncestors) > 0 || len(diff.parents) > 0 {
			// Already visited.
			continue
		}

		for cursor := range commits.BFS(headCommit) {
			if cursor.CommitID().Equal(headCommitID) {
				continue
			}
			commit, ok := cursor.Commit()
			if !ok {
				// Ancestor of base.
				cursor.SkipAncestors()
				continue
			}

			b, err := bookmarkForCommit(allBookmarks, cursor.CommitID(), bookmarkNames(slices.Values(selectedBookmarks)))
			switch {
			case isNoBookmarksError(err):
				// No bookmarks? Add to diff's unique ancestors.
				if sharedWith := commitToDiff[string(commit.ID)]; sharedWith != nil && sharedWith != diff {
					resultError = errors.Join(resultError, fmt.Errorf("%v is an ancestor of both %s and %s", commit.ID, diff.name, sharedWith.name))
				} else if sharedWith == nil {
					diff.uniqueAncestors = append(diff.uniqueAncestors, commit)
					commitToDiff[string(commit.ID)] = diff
				}
			case err == nil:
				// Stop traversing at bookmark.
				// We'll add to the queue so we visit later.
				cursor.SkipAncestors()
				parentDiff, err := diffForName(b.Name, commit)
				if err != nil {
					resultError = errors.Join(resultError, err)
					continue
				}
				diff.parents = append(diff.parents, parentDiff)
				parentDiff.children = append(parentDiff.children, diff)
				bookmarksStack = append(bookmarksStack, b)
			default:
				resultError = errors.Join(resultError, err)
			}
		}

		slices.Reverse(diff.uniqueAncestors)
		roots := new(commitgraph.Graph).Roots(diff.commitsBackward())
		if roots.Len() != 1 {
			resultError = errors.Join(resultError, fmt.Errorf("%s has multiple roots", diff.name))
		}
	}
	if resultError != nil {
		return diffGraph{}, resultError
	}

	g := diffGraph{}
	for _, c := range slices.Backward(sortedCommits) {
		diff := commitToDiff[string(c.ID)]
		if len(diff.parents) == 0 && !slices.Contains(g.roots, diff) {
			g.roots = append(g.roots, diff)
		}
	}
	return g, nil
}

// selectBookmarks returns the bookmarks with the given names.
// selectBookmarks returns an error if any of the bookmarks refer to the same commit.
func selectBookmarks(bookmarks []*jujutsu.Bookmark, names []string) ([]*jujutsu.Bookmark, error) {
	result := make([]*jujutsu.Bookmark, 0, len(names))
	for _, want := range names {
		i := slices.IndexFunc(bookmarks, func(b *jujutsu.Bookmark) bool {
			return b.Name == want && b.Remote == ""
		})
		if i < 0 {
			return nil, fmt.Errorf("no such bookmark %v", jujutsu.RefSymbol{Name: want})
		}
		b := bookmarks[i]
		headCommitID, ok := b.TargetMerge.Resolved()
		if !ok {
			return nil, fmt.Errorf("unresolved bookmark %v", b.RefSymbol())
		}
		j := slices.IndexFunc(bookmarks, func(b2 *jujutsu.Bookmark) bool {
			if b2 == b || b2.Remote != "" {
				return false
			}
			c, ok := b2.TargetMerge.Resolved()
			return !ok || c.Equal(headCommitID)
		})
		if j >= 0 {
			return nil, fmt.Errorf("%v and %v refer to the same commit (%v)",
				b.RefSymbol(), bookmarks[j].RefSymbol(), headCommitID)
		}
		result = append(result, b)
	}
	return result, nil
}

func bookmarkNames(bookmarks iter.Seq[*jujutsu.Bookmark]) iter.Seq[string] {
	return func(yield func(string) bool) {
		for b := range bookmarks {
			if !yield(b.Name) {
				return
			}
		}
	}
}

func walkGraph(start []*stackedDiff) iter.Seq[*stackedDiff] {
	return func(yield func(*stackedDiff) bool) {
		visited := make(map[*stackedDiff]struct{})
		queue := slices.Clone(start)
		for len(queue) > 0 {
			curr := queue[0]
			queue = queue[:copy(queue, queue[1:])]

			if _, ok := visited[curr]; ok {
				continue
			}
			needsParents := slices.ContainsFunc(curr.parents, func(parent *stackedDiff) bool {
				_, ok := visited[parent]
				return !ok
			})
			if needsParents {
				queue = append(queue, curr.parents...)
				queue = append(queue, curr)
				continue
			}
			visited[curr] = struct{}{}
			queue = slices.Insert(queue, 0, curr.children...)
			if !yield(curr) {
				return
			}
		}
	}
}

func heads(start []*stackedDiff) iter.Seq[*stackedDiff] {
	return func(yield func(*stackedDiff) bool) {
		visited := make(map[*stackedDiff]struct{})
		queue := slices.Clone(start)
		for len(queue) > 0 {
			curr := queue[0]
			queue = queue[:copy(queue, queue[1:])]

			if _, ok := visited[curr]; ok {
				continue
			}
			visited[curr] = struct{}{}
			if len(curr.children) == 0 {
				if !yield(curr) {
					return
				}
			}
			queue = append(queue, curr.children...)
		}
	}
}
