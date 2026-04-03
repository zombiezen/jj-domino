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
	"net/url"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/shurcooL/githubv4"
	"zombiezen.com/go/jj-domino/internal/jujutsu"
)

func TestStackForBookmark(t *testing.T) {
	ctx := testContext(t)
	jjExe := findJJExecutable(t)

	t.Run("SinglePR", func(t *testing.T) {
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
		commits, err := newChanges(ctx, jj, []changeDescription{
			{bookmark: "foo"},
		})
		if err != nil {
			t.Fatal(err)
		}
		bookmarks, err := jj.ListBookmarks(ctx)
		if err != nil {
			t.Fatal(err)
		}

		got, err := stackForBookmark(ctx, jj, bookmarks, jujutsu.RefSymbol{Name: "main"}, "foo")
		if err != nil {
			t.Fatal(err)
		}
		want := []stackedDiff{
			{localCommitRef: localCommitRef{name: "foo", commit: commits["foo"]}},
		}
		if diff := cmp.Diff(want, got, stackedDiffOption()); diff != "" {
			t.Errorf("stack (-want +got):\n%s", diff)
		}
	})

	t.Run("LinearChain", func(t *testing.T) {
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
		commits, err := newChanges(ctx, jj, []changeDescription{
			{bookmark: "foo"},
			{bookmark: "bar"},
		})
		if err != nil {
			t.Fatal(err)
		}
		bookmarks, err := jj.ListBookmarks(ctx)
		if err != nil {
			t.Fatal(err)
		}

		got, err := stackForBookmark(ctx, jj, bookmarks, jujutsu.RefSymbol{Name: "main"}, "bar")
		if err != nil {
			t.Fatal(err)
		}
		want := []stackedDiff{
			{localCommitRef: localCommitRef{name: "foo", commit: commits["foo"]}},
			{localCommitRef: localCommitRef{name: "bar", commit: commits["bar"]}},
		}
		if diff := cmp.Diff(want, got, stackedDiffOption()); diff != "" {
			t.Errorf("stack (-want +got):\n%s", diff)
		}
	})

	t.Run("LinearChainWithExtraCommits", func(t *testing.T) {
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
		commits, err := newChanges(ctx, jj, []changeDescription{
			{bookmark: "f"},
			{bookmark: "e"},
			{bookmark: "d"},
			{bookmark: "c"},
			{bookmark: "b"},
			{bookmark: "a"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := jj.DeleteBookmarks(ctx, []string{"b", "c", "e", "f"}); err != nil {
			t.Fatal(err)
		}
		bookmarks, err := jj.ListBookmarks(ctx)
		if err != nil {
			t.Fatal(err)
		}

		got, err := stackForBookmark(ctx, jj, bookmarks, jujutsu.RefSymbol{Name: "main"}, "a")
		if err != nil {
			t.Fatal(err)
		}
		want := []stackedDiff{
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
		if diff := cmp.Diff(want, got, stackedDiffOption()); diff != "" {
			t.Errorf("stack (-want +got):\n%s", diff)
		}
	})

	t.Run("ValidMerge", func(t *testing.T) {
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
		commits, err := newChanges(ctx, jj, []changeDescription{
			{bookmark: "d"},
			{bookmark: "b", parents: []string{"d"}},
			{bookmark: "c", parents: []string{"d"}},
			{bookmark: "a", parents: []string{"b", "c"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := jj.DeleteBookmarks(ctx, []string{"b", "c", "d"}); err != nil {
			t.Fatal(err)
		}
		bookmarks, err := jj.ListBookmarks(ctx)
		if err != nil {
			t.Fatal(err)
		}

		got, err := stackForBookmark(ctx, jj, bookmarks, jujutsu.RefSymbol{Name: "main"}, "a")
		if err != nil {
			t.Fatal(err)
		}
		want := []stackedDiff{
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
		if diff := cmp.Diff(want, got, stackedDiffOption()); diff != "" {
			t.Errorf("stack (-want +got):\n%s", diff)
		}
	})

	t.Run("InvalidMerge", func(t *testing.T) {
		t.Skip("TODO(#1)")

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
		_, err = newChanges(ctx, jj, []changeDescription{
			{bookmark: "d"},
			{bookmark: "b", parents: []string{"d"}},
			{bookmark: "c", parents: []string{"d"}},
			{bookmark: "a", parents: []string{"b", "c"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := jj.DeleteBookmarks(ctx, []string{"c", "d"}); err != nil {
			t.Fatal(err)
		}
		bookmarks, err := jj.ListBookmarks(ctx)
		if err != nil {
			t.Fatal(err)
		}

		got, err := stackForBookmark(ctx, jj, bookmarks, jujutsu.RefSymbol{Name: "main"}, "a")
		if err == nil {
			names := make([]string, 0, len(got))
			for _, pr := range got {
				names = append(names, pr.name)
			}
			t.Error("stackForBookmark did not return an error. Stack: main ←", strings.Join(names, " ← "))
		}
	})
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
		err := jj.Log(ctx, "@", func(c *jujutsu.Commit) bool {
			m[desc.bookmark] = c
			return false
		})
		if err != nil {
			return nil, fmt.Errorf("create %s: %v", desc.bookmark, err)
		}
	}
	return m, nil
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

func TestWriteStackFooter(t *testing.T) {
	repository := &gitHubRepository{
		Owner: &gitHubRepositoryOwner{Login: "zombiezen"},
		Name:  "jj-domino",
	}

	tests := []struct {
		name  string
		stack []stackedDiff
		plan  []*plannedPullRequest
		want  []string
	}{
		{
			name: "Single",
			stack: []stackedDiff{
				{
					localCommitRef: localCommitRef{
						name:   "foo",
						commit: &jujutsu.Commit{ID: jujutsu.CommitID{0x01, 0x23}},
					},
				},
			},
			plan: []*plannedPullRequest{
				{
					baseRepositoryPath: repository.path(),
					pullRequest: pullRequest{
						Number:         123,
						BaseRefName:    "main",
						HeadRefName:    "foo",
						HeadRepository: repository,
					},
				},
			},
			want: []string{
				"",
			},
		},
		{
			name: "TwoStack",
			stack: []stackedDiff{
				{
					localCommitRef: localCommitRef{
						name:   "foo",
						commit: &jujutsu.Commit{ID: jujutsu.CommitID{0x01, 0x23}},
					},
				},
				{
					localCommitRef: localCommitRef{
						name:   "bar",
						commit: &jujutsu.Commit{ID: jujutsu.CommitID{0x45, 0x67}},
					},
				},
			},
			plan: []*plannedPullRequest{
				{
					baseRepositoryPath: repository.path(),
					pullRequest: pullRequest{
						Number:         123,
						BaseRefName:    "main",
						HeadRefName:    "foo",
						HeadRepository: repository,
						URL: githubv4.URI{URL: &url.URL{
							Scheme: "https",
							Host:   "github.com",
							Path:   "/" + repository.path().String() + "/pull/123",
						}},
					},
				},
				{
					baseRepositoryPath: repository.path(),
					pullRequest: pullRequest{
						Number:         456,
						BaseRefName:    "main",
						HeadRefName:    "bar",
						HeadRepository: repository,
						URL: githubv4.URI{URL: &url.URL{
							Scheme: "https",
							Host:   "github.com",
							Path:   "/" + repository.path().String() + "/pull/456",
						}},
					},
				},
			},
			want: []string{
				stackFooterPreamble +
					stackFooterStackIntro +
					" 1. *→ this pull request ←*\n" +
					" 2. #456\n",
				stackFooterPreamble +
					fmt.Sprintf(stackFooterChangesSection, "#123", "commit", "https://github.com/"+repository.path().String()+"/pull/456/changes/4567") +
					stackFooterStackIntro +
					" 1. #123\n" +
					" 2. *→ this pull request ←*\n",
			},
		},
		{
			name: "TwoStackWithMultipleCommits",
			stack: []stackedDiff{
				{
					localCommitRef: localCommitRef{
						name:   "foo",
						commit: &jujutsu.Commit{ID: jujutsu.CommitID{0x01, 0x23}},
					},
				},
				{
					localCommitRef: localCommitRef{
						name:   "bar",
						commit: &jujutsu.Commit{ID: jujutsu.CommitID{0x89, 0xab}},
					},
					uniqueAncestors: []*jujutsu.Commit{
						{ID: jujutsu.CommitID{0x45, 0x67}},
					},
				},
			},
			plan: []*plannedPullRequest{
				{
					baseRepositoryPath: repository.path(),
					pullRequest: pullRequest{
						Number:         123,
						BaseRefName:    "main",
						HeadRefName:    "foo",
						HeadRepository: repository,
						URL: githubv4.URI{URL: &url.URL{
							Scheme: "https",
							Host:   "github.com",
							Path:   "/" + repository.path().String() + "/pull/123",
						}},
					},
				},
				{
					baseRepositoryPath: repository.path(),
					pullRequest: pullRequest{
						Number:         456,
						BaseRefName:    "main",
						HeadRefName:    "bar",
						HeadRepository: repository,
						URL: githubv4.URI{URL: &url.URL{
							Scheme: "https",
							Host:   "github.com",
							Path:   "/" + repository.path().String() + "/pull/456",
						}},
					},
				},
			},
			want: []string{
				stackFooterPreamble +
					stackFooterStackIntro +
					" 1. *→ this pull request ←*\n" +
					" 2. #456\n",
				stackFooterPreamble +
					fmt.Sprintf(stackFooterChangesSection, "#123", "2 commits", "https://github.com/"+repository.path().String()+"/pull/456/changes/4567..89ab") +
					stackFooterStackIntro +
					" 1. #123\n" +
					" 2. *→ this pull request ←*\n",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for i, want := range test.want {
				sb := new(strings.Builder)
				writeStackFooter(sb, new(test.stack[i]), test.plan, i)
				got := sb.String()
				if diff := cmp.Diff(want, got); diff != "" {
					t.Errorf("footer for stack[%d] (-want +got):\n%s", i, diff)
				}
			}
		})
	}
}

func TestTrimStackFooter(t *testing.T) {
	tests := []struct {
		s    string
		want string
	}{
		{"", ""},
		{"Hello, World!", "Hello, World!"},
		{"foo\nbar", "foo\nbar"},
		{
			"I can mention " + stackFooterMarker + " in the middle of a line.",
			"I can mention " + stackFooterMarker + " in the middle of a line.",
		},
		{
			"foo\n" + stackFooterMarker + "\nbar",
			"foo\n",
		},
		{
			"foo\r\n" + stackFooterMarker + "\r\nbar",
			"foo\r\n",
		},
		{
			"foo\n  " + stackFooterMarker + " \nbar",
			"foo\n",
		},
	}

	for _, test := range tests {
		got := trimStackFooter(test.s)
		if diff := cmp.Diff(test.want, got); diff != "" {
			t.Errorf("trimStackFooter(%q) (-want +got:)\n%s", test.s, diff)
		}
	}
}

func TestFormatPRNumber(t *testing.T) {
	tests := []struct {
		n     githubv4.Int
		width int
		want  string
	}{
		{1, 1, "#1"},
		{2, 1, "#2"},
		{1, 2, " #1"},
		{123, 3, "#123"},
		{123, 1, "#123"},
		{123, 5, "  #123"},
	}

	for _, test := range tests {
		if got := formatPRNumber(test.n, test.width); got != test.want {
			t.Errorf("formatPRNumber(%d, %d) = %q; want %q", test.n, test.width, got, test.want)
		}
	}
}

func TestPRNumberPlaceholder(t *testing.T) {
	tests := []struct {
		width int
		want  string
	}{
		{1, "#X"},
		{3, "#XXX"},
	}

	for _, test := range tests {
		if got := prNumberPlaceholder(test.width); got != test.want {
			t.Errorf("prNumberPlaceholder(%d) = %q; want %q", test.width, got, test.want)
		}
	}
}
