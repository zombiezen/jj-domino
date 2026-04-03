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

// Package commitgraph provides commit graph inspection.
package commitgraph

import (
	"context"
	"fmt"
	"iter"
	"maps"
	"slices"

	"zombiezen.com/go/jj-domino/internal/jujutsu"
)

// Graph is a collection of [*jujutsu.Commit] values.
// The zero value or nil is an empty graph.
type Graph struct {
	commits map[string]*jujutsu.Commit
}

// New creates a new commit graph from the given commits.
// If multiple commits have the same [jujutsu.CommitID],
// then only the last commit with the ID is retained in the graph.
func New(seq iter.Seq[*jujutsu.Commit]) *Graph {
	var g *Graph
	for c := range seq {
		if c == nil {
			continue
		}
		if g == nil {
			g = &Graph{commits: make(map[string]*jujutsu.Commit)}
		}
		g.commits[string(c.ID)] = c
	}
	return g
}

// FromRevset reads the commit metadata from Jujutsu specified by the given revset.
func FromRevset(ctx context.Context, jj *jujutsu.Jujutsu, revset string) (*Graph, error) {
	var err error
	var commits iter.Seq[*jujutsu.Commit] = func(yield func(*jujutsu.Commit) bool) {
		err = jj.Log(ctx, jujutsu.LogOptions{Revset: revset}, yield)
	}
	g := New(commits)
	if err != nil {
		return nil, err
	}
	return g, nil
}

// Get returns the commit in the graph with the given ID.
func (g *Graph) Get(id jujutsu.CommitID) (_ *jujutsu.Commit, ok bool) {
	if g == nil {
		return nil, false
	}
	c, ok := g.commits[string(id)]
	return c, ok
}

// Len returns the number of commits in g.
func (g *Graph) Len() int {
	if g == nil {
		return 0
	}
	return len(g.commits)
}

// All returns an iterator over All the commits in the graph.
// The iteration order is undefined.
func (g *Graph) All() iter.Seq[*jujutsu.Commit] {
	if g == nil {
		return func(yield func(*jujutsu.Commit) bool) {}
	}
	return maps.Values(g.commits)
}

// Children returns an iterator over the direct descendants of the commit with the given ID.
// The iteration order is undefined.
func (g *Graph) Children(id jujutsu.CommitID) iter.Seq[*jujutsu.Commit] {
	return func(yield func(*jujutsu.Commit) bool) {
		for c := range g.All() {
			if slices.ContainsFunc(c.Parents, id.Equal) {
				if !yield(c) {
					return
				}
			}
		}
	}
}

// Cursor is the position element yielded by [*Graph.BFS].
type Cursor struct {
	id     jujutsu.CommitID
	commit *jujutsu.Commit
	skip   bool
}

// CommitID returns the current commit ID.
func (c *Cursor) CommitID() jujutsu.CommitID {
	return c.id
}

// Commit returns the current commit.
func (c *Cursor) Commit() (_ *jujutsu.Commit, ok bool) {
	return c.commit, c.commit != nil
}

// SkipAncestors prevents the iterator from descending into the ancestors of the current commit.
// Ancestors may still be visited if they are ancestors of another commit
// that is not skipped.
func (c *Cursor) SkipAncestors() {
	c.skip = true
}

// BFS returns an iterator over x followed by all its ancestors in breadth-first order.
func (g *Graph) BFS(x *jujutsu.Commit) iter.Seq[*Cursor] {
	return func(yield func(*Cursor) bool) {
		if x == nil {
			return
		}
		cursor := &Cursor{
			id:     x.ID,
			commit: x,
		}
		if !yield(cursor) || cursor.skip {
			return
		}
		next := x.Parents
		var buf []jujutsu.CommitID
		for len(next) > 0 {
			if len(next) == 1 {
				id := next[0]
				cursor := &Cursor{id: id}
				cursor.commit, _ = g.Get(id)
				if !yield(cursor) || cursor.skip || cursor.commit == nil {
					return
				}
				next = cursor.commit.Parents
				continue
			}

			n := 0
			for _, id := range next {
				if c, ok := g.Get(id); ok {
					n += len(c.Parents)
				}
			}
			buf = slices.Grow(buf[:0], n)
			for _, id := range next {
				cursor := &Cursor{id: id}
				cursor.commit, _ = g.Get(id)
				if !yield(cursor) {
					return
				}
				if cursor.commit != nil && !cursor.skip {
					buf = append(buf, cursor.commit.Parents...)
				}
			}
			next = buf
		}
	}
}

// IsAncestor reports whether x is an ancestor of y.
func (g *Graph) IsAncestor(x jujutsu.CommitID, y *jujutsu.Commit) bool {
	for cursor := range g.BFS(y) {
		if cursor.CommitID().Equal(x) {
			return true
		}
	}
	return false
}

// Union returns a [*Graph] that contains all the commits in both g and x.
// If x has commits that have the same [jujutsu.CommitID] as in g
// but reference a different [jujutsu.Commit],
// then x's commits take precedence.
// If multiple commits in x have the same ID,
// then only the last commit with the ID is retained in the graph.
// If x's commits are already contained in g, then g is returned as-is.
func (g *Graph) Union(x iter.Seq[*jujutsu.Commit]) *Graph {
	next, stop := iter.Pull(x)
	defer stop()

	for {
		xc, ok := next()
		if !ok {
			return g
		}
		if c, _ := g.Get(xc.ID); c != xc {
			if g == nil {
				g = &Graph{commits: make(map[string]*jujutsu.Commit)}
			} else {
				g = new(*g)
				if len(g.commits) == 0 {
					g.commits = make(map[string]*jujutsu.Commit)
				} else {
					g.commits = maps.Clone(g.commits)
				}
			}
			g.commits[string(xc.ID)] = xc
			for {
				xc, ok := next()
				if !ok {
					break
				}
				g.commits[string(xc.ID)] = xc
			}
			return g
		}
	}
}

// Heads returns the commits in x that are not ancestors of other commits in x.
// g is used for additional ancestry information.
func (g *Graph) Heads(x iter.Seq[*jujutsu.Commit]) *Graph {
	xg := New(x)
	var xgCommits map[string]*jujutsu.Commit
	if xg != nil {
		xgCommits = xg.commits
	}

	g = g.Union(xg.All())
	found := make(map[string]struct{}, xg.Len())
	for k, c := range xgCommits {
		if _, canSkip := found[k]; canSkip {
			continue
		}
		for cursor := range g.BFS(c) {
			ancestor := cursor.CommitID()
			if ancestor.Equal(c.ID) {
				continue
			}
			if xg.commits[string(ancestor)] != nil {
				found[string(ancestor)] = struct{}{}
			}
		}
	}
	for k := range found {
		delete(xg.commits, k)
	}
	return xg
}

// Roots returns the commits in x that are not descendants of other commits in x.
// g is used for additional ancestry information.
func (g *Graph) Roots(x iter.Seq[*jujutsu.Commit]) *Graph {
	xg := New(x)
	var xgCommits map[string]*jujutsu.Commit
	if xg != nil {
		xgCommits = xg.commits
	}

	g = g.Union(xg.All())
	for k, c := range xgCommits {
		for cursor := range g.BFS(c) {
			ancestor := cursor.CommitID()
			if ancestor.Equal(c.ID) {
				continue
			}
			if xg.commits[string(ancestor)] != nil {
				delete(xg.commits, k)
				break
			}
		}
	}
	return xg
}

// Sort returns the graph in a topologically sorted order,
// where commits come before their ancestors.
// An error is returned if and only if the graph contains cycles.
func (g *Graph) Sort() ([]*jujutsu.Commit, error) {
	if g == nil || len(g.commits) == 0 {
		return nil, nil
	}

	// Broadly, we try to follow Jujutsu's algorithm
	// described here: https://github.com/jj-vcs/jj/blob/v0.39.0/lib/src/graph.rs#L118-L125
	// In summary:
	//
	//  - Depth-first search from heads.
	//  - At fork points, descend into other branches.
	//  - At merge points, visit ancestor branches in reverse order.

	children := make(map[*jujutsu.Commit][]*jujutsu.Commit, len(g.commits))
	for _, c := range g.commits {
		for _, parentID := range c.Parents {
			if parent, ok := g.Get(parentID); ok {
				children[parent] = append(children[parent], c)
			}
		}
	}
	stack := make([]*jujutsu.Commit, 0, len(g.commits))
	for _, c := range g.commits {
		if childList := children[c]; len(childList) == 0 {
			stack = append(stack, c)
		} else {
			slices.SortFunc(childList, compareCommitTimes)
		}
	}
	if len(stack) == 0 {
		return nil, fmt.Errorf("graph is cyclic")
	}
	slices.SortFunc(stack, compareCommitTimes)

	l := make([]*jujutsu.Commit, 0, len(g.commits))
	// A commit's mark is true if the commit has been added to l;
	// a commit's mark is false if the commit's children are still being visited.
	marks := make(map[*jujutsu.Commit]bool, len(g.commits))
popStack:
	for len(stack) > 0 {
		curr := stack[len(stack)-1]
		stack = slices.Delete(stack, len(stack)-1, len(stack))
		if mark, hasMark := marks[curr]; !hasMark {
			marks[curr] = false
			stack = append(stack, curr)
			stack = append(stack, children[curr]...)
			continue popStack
		} else if mark {
			continue popStack
		}
		for _, child := range children[curr] {
			if !marks[child] {
				return nil, fmt.Errorf("commit %v is cyclic", curr.ID)
			}
		}

		l = append(l, curr)
		marks[curr] = true

		// Append to stack in order of parents so they are visited in reverse order.
		for _, parentID := range curr.Parents {
			if parent, ok := g.Get(parentID); ok {
				stack = append(stack, parent)
			}
		}
	}
	return l, nil
}

func compareCommitTimes(a, b *jujutsu.Commit) int {
	return a.Committer.Timestamp.Compare(b.Committer.Timestamp)
}
