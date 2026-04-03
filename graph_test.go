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
	"fmt"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"zombiezen.com/go/jj-domino/internal/jujutsu"
)

func TestStackForBookmark(t *testing.T) {
	tests := []struct {
		name            string
		skip            string
		changes         []changeDescription
		deleteBookmarks []string
		bookmark        string
		want            func(commits map[string]*jujutsu.Commit) []stackedDiff
		wantError       bool
	}{
		{
			name: "SinglePR",
			changes: []changeDescription{
				{bookmark: "foo"},
			},
			bookmark: "foo",
			want: func(commits map[string]*jujutsu.Commit) []stackedDiff {
				return []stackedDiff{
					{localCommitRef: localCommitRef{name: "foo", commit: commits["foo"]}},
				}
			},
		},
		{
			name: "LinearChain",
			changes: []changeDescription{
				{bookmark: "foo"},
				{bookmark: "bar"},
			},
			bookmark: "bar",
			want: func(commits map[string]*jujutsu.Commit) []stackedDiff {
				return []stackedDiff{
					{localCommitRef: localCommitRef{name: "foo", commit: commits["foo"]}},
					{localCommitRef: localCommitRef{name: "bar", commit: commits["bar"]}},
				}
			},
		},
		{
			name: "LinearChainWithExtraCommits",
			changes: []changeDescription{
				{bookmark: "f"},
				{bookmark: "e"},
				{bookmark: "d"},
				{bookmark: "c"},
				{bookmark: "b"},
				{bookmark: "a"},
			},
			deleteBookmarks: []string{"b", "c", "e", "f"},
			bookmark:        "a",
			want: func(commits map[string]*jujutsu.Commit) []stackedDiff {
				return []stackedDiff{
					{
						localCommitRef: localCommitRef{
							name:   "d",
							commit: commits["d"],
						},
						uniqueAncestors: []*jujutsu.Commit{
							commits["f"],
							commits["e"],
						},
					},
					{
						localCommitRef: localCommitRef{
							name:   "a",
							commit: commits["a"],
						},
						uniqueAncestors: []*jujutsu.Commit{
							commits["c"],
							commits["b"],
						},
					},
				}
			},
		},
		{
			name: "ValidMerge",
			changes: []changeDescription{
				{bookmark: "d"},
				{bookmark: "b", parents: []string{"d"}},
				{bookmark: "c", parents: []string{"d"}},
				{bookmark: "a", parents: []string{"b", "c"}},
			},
			deleteBookmarks: []string{"b", "c", "d"},
			bookmark:        "a",
			want: func(commits map[string]*jujutsu.Commit) []stackedDiff {
				return []stackedDiff{
					{
						localCommitRef: localCommitRef{
							name:   "a",
							commit: commits["a"],
						},
						uniqueAncestors: []*jujutsu.Commit{
							commits["d"],
							commits["c"],
							commits["b"],
						},
					},
				}
			},
		},
		{
			name: "InvalidMerge",
			skip: "TODO(#1)",
			changes: []changeDescription{
				{bookmark: "d"},
				{bookmark: "b", parents: []string{"d"}},
				{bookmark: "c", parents: []string{"d"}},
				{bookmark: "a", parents: []string{"b", "c"}},
			},
			deleteBookmarks: []string{"c", "d"},
			bookmark:        "a",
			wantError:       true,
		},
	}

	jjExe := findJJExecutable(t)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.skip != "" {
				t.Skip(test.skip)
			}

			ctx := testContext(t)

			repoDir := t.TempDir()
			jj, err := jujutsu.New(jujutsu.Options{
				Dir:   repoDir,
				JJExe: jjExe,
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := createRepository(ctx, jj); err != nil {
				t.Fatal(err)
			}
			commits, err := newChanges(ctx, jj, test.changes)
			if err != nil {
				t.Fatal(err)
			}
			if err := jj.DeleteBookmarks(ctx, test.deleteBookmarks); err != nil {
				t.Fatal(err)
			}
			bookmarks, err := jj.ListBookmarks(ctx)
			if err != nil {
				t.Fatal(err)
			}

			got, err := stackForBookmark(ctx, jj, bookmarks, jujutsu.RefSymbol{Name: "main"}, test.bookmark)
			switch {
			case test.wantError && err == nil:
				names := make([]string, 0, len(got))
				for _, pr := range got {
					names = append(names, pr.name)
				}
				t.Error("stackForBookmark did not return an error. Stack: main ←", strings.Join(names, " ← "))
			case !test.wantError && err == nil:
				want := test.want(commits)
				if diff := cmp.Diff(want, got, stackedDiffOption()); diff != "" {
					t.Errorf("stack (-want +got):\n%s", diff)
				}
			default:
				t.Log(err)
				if !test.wantError {
					t.Fail()
				}
			}
		})
	}
}

func stackedDiffOption() cmp.Option {
	return cmp.Options{
		cmp.AllowUnexported(stackedDiff{}, localCommitRef{}),
		cmp.FilterPath(func(p cmp.Path) bool {
			return p.Index(-2).Type() == reflect.TypeFor[stackedDiff]() &&
				p.Last().(cmp.StructField).Name() == "uniqueAncestors"
		}, cmpopts.EquateEmpty()),
	}
}

func createRepository(ctx context.Context, jj *jujutsu.Jujutsu) error {
	if err := jj.GitInit(ctx, jujutsu.GitInitOptions{}); err != nil {
		return err
	}
	if err := jj.SetBookmark(ctx, []string{"main"}, jujutsu.SetBookmarkOptions{}); err != nil {
		return err
	}
	return nil
}

type changeDescription struct {
	bookmark string
	parents  []string
}

func newChanges(ctx context.Context, jj *jujutsu.Jujutsu, changes []changeDescription) (map[string]*jujutsu.Commit, error) {
	m := make(map[string]*jujutsu.Commit)
	for _, desc := range changes {
		if err := jj.New(ctx, desc.parents...); err != nil {
			return m, fmt.Errorf("create %s: %v", desc.bookmark, err)
		}
		if err := jj.SetBookmark(ctx, []string{desc.bookmark}, jujutsu.SetBookmarkOptions{}); err != nil {
			return m, err
		}
		opts := jujutsu.LogOptions{Revset: "@"}
		err := jj.Log(ctx, opts, func(c *jujutsu.Commit) bool {
			m[desc.bookmark] = c
			return false
		})
		if err != nil {
			return nil, fmt.Errorf("create %s: %v", desc.bookmark, err)
		}
	}
	return m, nil
}

func trunkPlaceholderCommitID() jujutsu.CommitID {
	return jujutsu.CommitID{0xde, 0xad, 0xbe, 0xef, 0xca, 0xfe}
}

var jjExePath = sync.OnceValues(func() (string, error) {
	return exec.LookPath("jj")
})

func findJJExecutable(tb testing.TB) string {
	tb.Helper()
	exe, err := jjExePath()
	if err != nil {
		tb.Skip("Cannot find Jujutsu:", err)
	}
	return exe
}
