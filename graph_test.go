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
	"os/exec"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"zombiezen.com/go/jj-domino/internal/commitgraph"
	"zombiezen.com/go/jj-domino/internal/jujutsu"
)

func TestNewDiffGraph(t *testing.T) {
	tests := []struct {
		name string

		commits               []*jujutsu.Commit
		bookmarks             []*jujutsu.Bookmark
		selectedBookmarkNames []string

		want      func(commits []*jujutsu.Commit) diffGraph
		wantError bool
	}{
		{
			name: "SinglePR",
			commits: []*jujutsu.Commit{
				{
					ID:      jujutsu.CommitID{0x12, 0x34},
					Parents: []jujutsu.CommitID{trunkPlaceholderCommitID()},
				},
			},
			bookmarks: []*jujutsu.Bookmark{
				{Name: "foo", TargetMerge: jujutsu.Resolved(jujutsu.CommitID{0x12, 0x34})},
			},
			selectedBookmarkNames: []string{"foo"},
			want: func(commits []*jujutsu.Commit) diffGraph {
				return diffGraph{[]*stackedDiff{
					{
						localCommitRef: localCommitRef{name: "foo", commit: commits[0]},
					},
				}}
			},
		},
		{
			name: "LinearChain",
			commits: []*jujutsu.Commit{
				{
					ID:      jujutsu.CommitID{0x56, 0x78},
					Parents: []jujutsu.CommitID{{0x12, 0x34}},
				},
				{
					ID:      jujutsu.CommitID{0x12, 0x34},
					Parents: []jujutsu.CommitID{trunkPlaceholderCommitID()},
				},
			},
			bookmarks: []*jujutsu.Bookmark{
				{Name: "foo", TargetMerge: jujutsu.Resolved(jujutsu.CommitID{0x12, 0x34})},
				{Name: "bar", TargetMerge: jujutsu.Resolved(jujutsu.CommitID{0x56, 0x78})},
			},
			selectedBookmarkNames: []string{"bar"},
			want: func(commits []*jujutsu.Commit) diffGraph {
				foo := &stackedDiff{
					localCommitRef: localCommitRef{name: "foo", commit: commits[1]},
				}
				bar := &stackedDiff{
					localCommitRef: localCommitRef{name: "bar", commit: commits[0]},
				}
				foo.children = []*stackedDiff{bar}
				bar.parents = []*stackedDiff{foo}
				return diffGraph{
					roots: []*stackedDiff{foo},
				}
			},
		},
		{
			name: "LinearChainWithExtraCommits",
			commits: []*jujutsu.Commit{
				{
					ID:      jujutsu.CommitID{0xaa, 0xaa},
					Parents: []jujutsu.CommitID{{0xbb, 0xbb}},
				},
				{
					ID:      jujutsu.CommitID{0xbb, 0xbb},
					Parents: []jujutsu.CommitID{{0xcc, 0xcc}},
				},
				{
					ID:      jujutsu.CommitID{0xcc, 0xcc},
					Parents: []jujutsu.CommitID{{0xdd, 0xdd}},
				},
				{
					ID:      jujutsu.CommitID{0xdd, 0xdd},
					Parents: []jujutsu.CommitID{{0xee, 0xee}},
				},
				{
					ID:      jujutsu.CommitID{0xee, 0xee},
					Parents: []jujutsu.CommitID{{0xff, 0xff}},
				},
				{
					ID:      jujutsu.CommitID{0xff, 0xff},
					Parents: []jujutsu.CommitID{trunkPlaceholderCommitID()},
				},
			},
			bookmarks: []*jujutsu.Bookmark{
				{Name: "a", TargetMerge: jujutsu.Resolved(jujutsu.CommitID{0xaa, 0xaa})},
				{Name: "d", TargetMerge: jujutsu.Resolved(jujutsu.CommitID{0xdd, 0xdd})},
			},
			selectedBookmarkNames: []string{"a"},
			want: func(commits []*jujutsu.Commit) diffGraph {
				d := &stackedDiff{
					localCommitRef: localCommitRef{
						name:   "d",
						commit: commits[3],
					},
					uniqueAncestors: []*jujutsu.Commit{
						commits[5],
						commits[4],
					},
				}
				a := &stackedDiff{
					localCommitRef: localCommitRef{
						name:   "a",
						commit: commits[0],
					},
					uniqueAncestors: []*jujutsu.Commit{
						commits[2],
						commits[1],
					},
				}
				d.children = []*stackedDiff{a}
				a.parents = []*stackedDiff{d}
				return diffGraph{
					roots: []*stackedDiff{d},
				}
			},
		},
		{
			name: "ValidMerge",
			commits: []*jujutsu.Commit{
				{
					ID:      jujutsu.CommitID{0xaa, 0xaa},
					Parents: []jujutsu.CommitID{{0xbb, 0xbb}, {0xcc, 0xcc}},
				},
				{
					ID:      jujutsu.CommitID{0xbb, 0xbb},
					Parents: []jujutsu.CommitID{{0xdd, 0xdd}},
				},
				{
					ID:      jujutsu.CommitID{0xcc, 0xcc},
					Parents: []jujutsu.CommitID{{0xdd, 0xdd}},
				},
				{
					ID:      jujutsu.CommitID{0xdd, 0xdd},
					Parents: []jujutsu.CommitID{trunkPlaceholderCommitID()},
				},
			},
			bookmarks: []*jujutsu.Bookmark{
				{Name: "a", TargetMerge: jujutsu.Resolved(jujutsu.CommitID{0xaa, 0xaa})},
			},
			selectedBookmarkNames: []string{"a"},
			want: func(commits []*jujutsu.Commit) diffGraph {
				a := &stackedDiff{
					localCommitRef: localCommitRef{
						name:   "a",
						commit: commits[0],
					},
					uniqueAncestors: []*jujutsu.Commit{
						commits[3],
						commits[2],
						commits[1],
					},
				}
				return diffGraph{
					roots: []*stackedDiff{a},
				}
			},
		},
		{
			name: "InvalidMerge",
			commits: []*jujutsu.Commit{
				{
					ID:      jujutsu.CommitID{0xaa, 0xaa},
					Parents: []jujutsu.CommitID{{0xbb, 0xbb}, {0xcc, 0xcc}},
				},
				{
					ID:      jujutsu.CommitID{0xbb, 0xbb},
					Parents: []jujutsu.CommitID{{0xdd, 0xdd}},
				},
				{
					ID:      jujutsu.CommitID{0xcc, 0xcc},
					Parents: []jujutsu.CommitID{{0xdd, 0xdd}},
				},
				{
					ID:      jujutsu.CommitID{0xdd, 0xdd},
					Parents: []jujutsu.CommitID{trunkPlaceholderCommitID()},
				},
			},
			bookmarks: []*jujutsu.Bookmark{
				{Name: "a", TargetMerge: jujutsu.Resolved(jujutsu.CommitID{0xaa, 0xaa})},
				{Name: "b", TargetMerge: jujutsu.Resolved(jujutsu.CommitID{0xbb, 0xbb})},
			},
			selectedBookmarkNames: []string{"a"},
			wantError:             true,
		},
		{
			name: "MultipleRoots",
			commits: []*jujutsu.Commit{
				{
					ID:      jujutsu.CommitID{0xaa, 0xaa},
					Parents: []jujutsu.CommitID{{0xbb, 0xbb}, {0xcc, 0xcc}},
				},
				{
					ID:      jujutsu.CommitID{0xbb, 0xbb},
					Parents: []jujutsu.CommitID{{0xdd, 0xdd}},
				},
				{
					ID:      jujutsu.CommitID{0xcc, 0xcc},
					Parents: []jujutsu.CommitID{{0xdd, 0xdd}},
				},
				{
					ID:      jujutsu.CommitID{0xdd, 0xdd},
					Parents: []jujutsu.CommitID{trunkPlaceholderCommitID()},
				},
			},
			bookmarks: []*jujutsu.Bookmark{
				{Name: "a", TargetMerge: jujutsu.Resolved(jujutsu.CommitID{0xaa, 0xaa})},
				{Name: "d", TargetMerge: jujutsu.Resolved(jujutsu.CommitID{0xdd, 0xdd})},
			},
			selectedBookmarkNames: []string{"a"},
			wantError:             true,
		},
		{
			name: "ForkPoint",
			commits: []*jujutsu.Commit{
				{
					ID:      jujutsu.CommitID{0x89, 0xab},
					Parents: []jujutsu.CommitID{{0x01, 0x23}},
				},
				{
					ID:      jujutsu.CommitID{0x45, 0x67},
					Parents: []jujutsu.CommitID{{0x01, 0x23}},
				},
				{
					ID:      jujutsu.CommitID{0x01, 0x23},
					Parents: []jujutsu.CommitID{trunkPlaceholderCommitID()},
				},
			},
			bookmarks: []*jujutsu.Bookmark{
				{
					Name:        "a",
					TargetMerge: jujutsu.Resolved(jujutsu.CommitID{0x01, 0x23}),
				},
				{
					Name:        "b",
					TargetMerge: jujutsu.Resolved(jujutsu.CommitID{0x45, 0x67}),
				},
				{
					Name:        "c",
					TargetMerge: jujutsu.Resolved(jujutsu.CommitID{0x89, 0xab}),
				},
			},
			selectedBookmarkNames: []string{"b", "c"},
			want: func(commits []*jujutsu.Commit) diffGraph {
				a := &stackedDiff{
					localCommitRef: localCommitRef{
						name:   "a",
						commit: commits[2],
					},
				}
				b := &stackedDiff{
					localCommitRef: localCommitRef{
						name:   "b",
						commit: commits[1],
					},
				}
				c := &stackedDiff{
					localCommitRef: localCommitRef{
						name:   "c",
						commit: commits[0],
					},
				}
				a.children = []*stackedDiff{b, c}
				b.parents = []*stackedDiff{a}
				c.parents = []*stackedDiff{a}
				return diffGraph{
					roots: []*stackedDiff{a},
				}
			},
		},
		{
			name: "MergePoint",
			commits: []*jujutsu.Commit{
				{
					ID:      jujutsu.CommitID{0xcd, 0xef},
					Parents: []jujutsu.CommitID{{0x89, 0xab}},
				},
				{
					ID:      jujutsu.CommitID{0x89, 0xab},
					Parents: []jujutsu.CommitID{{0x01, 0x23}, {0x45, 0x67}},
				},
				{
					ID:      jujutsu.CommitID{0x45, 0x67},
					Parents: []jujutsu.CommitID{trunkPlaceholderCommitID()},
				},
				{
					ID:      jujutsu.CommitID{0x01, 0x23},
					Parents: []jujutsu.CommitID{trunkPlaceholderCommitID()},
				},
			},
			bookmarks: []*jujutsu.Bookmark{
				{
					Name:        "a",
					TargetMerge: jujutsu.Resolved(jujutsu.CommitID{0x01, 0x23}),
				},
				{
					Name:        "b",
					TargetMerge: jujutsu.Resolved(jujutsu.CommitID{0x45, 0x67}),
				},
				{
					Name:        "c",
					TargetMerge: jujutsu.Resolved(jujutsu.CommitID{0x89, 0xab}),
				},
				{
					Name:        "d",
					TargetMerge: jujutsu.Resolved(jujutsu.CommitID{0xcd, 0xef}),
				},
			},
			selectedBookmarkNames: []string{"c", "d"},
			want: func(commits []*jujutsu.Commit) diffGraph {
				a := &stackedDiff{
					localCommitRef: localCommitRef{
						name:   "a",
						commit: commits[3],
					},
				}
				b := &stackedDiff{
					localCommitRef: localCommitRef{
						name:   "b",
						commit: commits[2],
					},
				}
				c := &stackedDiff{
					localCommitRef: localCommitRef{
						name:   "c",
						commit: commits[1],
					},
				}
				c.parents = []*stackedDiff{a, b}
				a.children = []*stackedDiff{c}
				b.children = []*stackedDiff{c}
				d := &stackedDiff{
					localCommitRef: localCommitRef{
						name:   "d",
						commit: commits[0],
					},
				}
				d.parents = []*stackedDiff{c}
				c.children = []*stackedDiff{d}
				return diffGraph{
					roots: []*stackedDiff{a, b},
				}
			},
		},
		{
			name: "MultipleMergePoints",
			commits: []*jujutsu.Commit{
				{
					ID:      jujutsu.CommitID{0xcd, 0xef},
					Parents: []jujutsu.CommitID{{0x89, 0xab}, {0x45, 0x67}},
				},
				{
					ID:      jujutsu.CommitID{0x89, 0xab},
					Parents: []jujutsu.CommitID{{0x01, 0x23}},
				},
				{
					ID:      jujutsu.CommitID{0x45, 0x67},
					Parents: []jujutsu.CommitID{trunkPlaceholderCommitID()},
				},
				{
					ID:      jujutsu.CommitID{0x01, 0x23},
					Parents: []jujutsu.CommitID{trunkPlaceholderCommitID()},
				},
			},
			bookmarks: []*jujutsu.Bookmark{
				{
					Name:        "a",
					TargetMerge: jujutsu.Resolved(jujutsu.CommitID{0x01, 0x23}),
				},
				{
					Name:        "b",
					TargetMerge: jujutsu.Resolved(jujutsu.CommitID{0x45, 0x67}),
				},
				{
					Name:        "c",
					TargetMerge: jujutsu.Resolved(jujutsu.CommitID{0xcd, 0xef}),
				},
			},
			selectedBookmarkNames: []string{"c"},
			want: func(commits []*jujutsu.Commit) diffGraph {
				a := &stackedDiff{
					localCommitRef: localCommitRef{
						name:   "a",
						commit: commits[3],
					},
				}
				b := &stackedDiff{
					localCommitRef: localCommitRef{
						name:   "b",
						commit: commits[2],
					},
				}
				c := &stackedDiff{
					localCommitRef: localCommitRef{
						name:   "c",
						commit: commits[0],
					},
					uniqueAncestors: []*jujutsu.Commit{commits[1]},
				}
				c.parents = []*stackedDiff{b, a}
				a.children = []*stackedDiff{c}
				b.children = []*stackedDiff{c}
				return diffGraph{
					roots: []*stackedDiff{a, b},
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			selectedBookmarks, err := selectBookmarks(test.bookmarks, test.selectedBookmarkNames)
			if err != nil {
				t.Fatal(err)
			}
			got, err := newDiffGraph(commitgraph.New(slices.Values(test.commits)), test.bookmarks, selectedBookmarks)
			switch {
			case test.wantError && err == nil:
				description := new(strings.Builder)
				for diff := range got.walk() {
					if description.Len() > 0 {
						description.WriteString(", ")
					}
					if len(diff.parents) == 0 {
						description.WriteString("trunk()")
					} else {
						for i, parent := range diff.parents {
							if i > 0 {
								description.WriteString("|")
							}
							description.WriteString(parent.name)
						}
					}
					description.WriteString(" ← ")
					description.WriteString(diff.name)
				}
				t.Error("newDiffGraph did not return an error. Graph:", description)
			case !test.wantError && err == nil:
				want := test.want(test.commits)
				if diff := cmp.Diff(want, got, diffGraphOption()); diff != "" {
					t.Errorf("graph (-want +got):\n%s", diff)
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

func diffGraphOption() cmp.Option {
	return cmp.Options{
		cmp.AllowUnexported(diffGraph{}),
		stackedDiffOption(),
		cmp.FilterPath(func(p cmp.Path) bool {
			if p.Index(-2).Type() != reflect.TypeFor[diffGraph]() {
				return false
			}
			fieldName := p.Last().(cmp.StructField).Name()
			return fieldName == "roots"
		}, cmpopts.EquateEmpty()),
	}
}

func stackedDiffOption() cmp.Option {
	return cmp.Options{
		cmp.AllowUnexported(stackedDiff{}, localCommitRef{}),
		cmp.FilterPath(func(p cmp.Path) bool {
			if p.Index(-2).Type() != reflect.TypeFor[stackedDiff]() {
				return false
			}
			fieldName := p.Last().(cmp.StructField).Name()
			return fieldName == "uniqueAncestors" || fieldName == "parents" || fieldName == "children"
		}, cmpopts.EquateEmpty()),
	}
}

// TestGraphFromRepository tests [graphFromRepository] with a single simple linear chain.
// Most of the complexity is tested by [TestNewDiffGraph],
// so this is an integration test to gain confidence that real Jujutsu operations
// translate to our data model.
func TestGraphFromRepository(t *testing.T) {
	ctx := testContext(t)
	jjExe := findJJExecutable(t)

	repoDir := t.TempDir()
	jj, err := jujutsu.New(jujutsu.Options{
		Dir:   repoDir,
		JJExe: jjExe,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := jj.GitInit(ctx, jujutsu.GitInitOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := jj.SetBookmark(ctx, []string{"main"}, jujutsu.SetBookmarkOptions{}); err != nil {
		t.Fatal(err)
	}

	if err := jj.New(ctx); err != nil {
		t.Fatal(err)
	}
	if err := jj.SetBookmark(ctx, []string{"a"}, jujutsu.SetBookmarkOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := jj.New(ctx); err != nil {
		t.Fatal(err)
	}
	if err := jj.SetBookmark(ctx, []string{"b"}, jujutsu.SetBookmarkOptions{}); err != nil {
		t.Fatal(err)
	}
	bookmarks, err := jj.ListBookmarks(ctx)
	if err != nil {
		t.Fatal(err)
	}

	var commits []*jujutsu.Commit
	err = jj.Log(ctx,
		jujutsu.LogOptions{Revset: "main..b", Reversed: true},
		func(c *jujutsu.Commit) bool {
			commits = append(commits, c)
			return true
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 {
		t.Fatal("commits =", commits)
	}
	wantA := &stackedDiff{
		localCommitRef: localCommitRef{
			name:   "a",
			commit: commits[0],
		},
	}
	wantB := &stackedDiff{
		localCommitRef: localCommitRef{
			name:   "b",
			commit: commits[1],
		},
	}
	wantA.children = []*stackedDiff{wantB}
	wantB.parents = []*stackedDiff{wantA}
	want := diffGraph{
		roots: []*stackedDiff{wantA},
	}

	got, err := graphFromRepository(ctx, jj, bookmarks, jujutsu.RefSymbol{Name: "main"}, []string{"b"})
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, got, diffGraphOption()); diff != "" {
		t.Errorf("graph (-want +got):\n%s", diff)
	}
}

func mustDiffGraph(tb testing.TB, commits []*jujutsu.Commit, allBookmarks []*jujutsu.Bookmark, bookmarkNames []string) diffGraph {
	selectedBookmarks, err := selectBookmarks(allBookmarks, bookmarkNames)
	if err != nil {
		tb.Fatal(err)
	}
	g, err := newDiffGraph(commitgraph.New(slices.Values(commits)), allBookmarks, selectedBookmarks)
	if err != nil {
		tb.Fatal(err)
	}
	return g
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
