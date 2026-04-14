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
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/shurcooL/githubv4"
	"zombiezen.com/go/jj-domino/internal/github"
	"zombiezen.com/go/jj-domino/internal/jujutsu"
)

func TestWriteStackFooter(t *testing.T) {
	repository := &github.Repository{
		Owner: &github.RepositoryOwner{Login: "zombiezen"},
		Name:  "jj-domino",
	}

	tests := []struct {
		name  string
		graph diffGraph
		prs   map[string]*github.PullRequest
		want  map[string]string
	}{
		{
			name: "Single",
			graph: mustDiffGraph(t,
				[]*jujutsu.Commit{
					{
						ID:      jujutsu.CommitID{0x01, 0x23},
						Parents: []jujutsu.CommitID{trunkPlaceholderCommitID()},
					},
				},
				[]*jujutsu.Bookmark{
					{
						Name:        "foo",
						TargetMerge: jujutsu.Resolved(jujutsu.CommitID{0x01, 0x23}),
					},
				},
				[]string{"foo"},
			),
			prs: map[string]*github.PullRequest{
				"foo": {
					Number:         123,
					BaseRefName:    "main",
					HeadRefName:    "foo",
					HeadRepository: repository,
				},
			},
			want: map[string]string{
				"foo": "",
			},
		},
		{
			name: "TwoStack",
			graph: mustDiffGraph(t,
				[]*jujutsu.Commit{
					{
						ID:      jujutsu.CommitID{0x45, 0x67},
						Parents: []jujutsu.CommitID{{0x01, 0x23}},
					},
					{
						ID:      jujutsu.CommitID{0x01, 0x23},
						Parents: []jujutsu.CommitID{trunkPlaceholderCommitID()},
					},
				},
				[]*jujutsu.Bookmark{
					{
						Name:        "foo",
						TargetMerge: jujutsu.Resolved(jujutsu.CommitID{0x01, 0x23}),
					},
					{
						Name:        "bar",
						TargetMerge: jujutsu.Resolved(jujutsu.CommitID{0x45, 0x67}),
					},
				},
				[]string{"bar"},
			),
			prs: map[string]*github.PullRequest{
				"foo": {
					Number:         123,
					BaseRefName:    "main",
					HeadRefName:    "foo",
					HeadRepository: repository,
					URL: githubv4.URI{URL: &url.URL{
						Scheme: "https",
						Host:   "github.com",
						Path:   "/" + repository.Path().String() + "/pull/123",
					}},
				},
				"bar": {
					Number:         456,
					BaseRefName:    "main",
					HeadRefName:    "bar",
					HeadRepository: repository,
					URL: githubv4.URI{URL: &url.URL{
						Scheme: "https",
						Host:   "github.com",
						Path:   "/" + repository.Path().String() + "/pull/456",
					}},
				},
			},
			want: map[string]string{
				"foo": stackFooterPreamble +
					stackFooterRelatedSectionIntro +
					" 1. *→ this pull request ←*\n" +
					" 2. #456\n",
				"bar": stackFooterPreamble +
					fmt.Sprintf(stackFooterChangesSection, "#123", "commit", "https://github.com/"+repository.Path().String()+"/pull/456/changes/4567") +
					stackFooterRelatedSectionIntro +
					" 1. #123\n" +
					" 2. *→ this pull request ←*\n",
			},
		},
		{
			name: "TwoStackWithMultipleCommits",
			graph: mustDiffGraph(t,
				[]*jujutsu.Commit{
					{
						ID:      jujutsu.CommitID{0x89, 0xab},
						Parents: []jujutsu.CommitID{{0x45, 0x67}},
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
				[]*jujutsu.Bookmark{
					{
						Name:        "foo",
						TargetMerge: jujutsu.Resolved(jujutsu.CommitID{0x01, 0x23}),
					},
					{
						Name:        "bar",
						TargetMerge: jujutsu.Resolved(jujutsu.CommitID{0x89, 0xab}),
					},
				},
				[]string{"bar"},
			),
			prs: map[string]*github.PullRequest{
				"foo": {
					Number:         123,
					BaseRefName:    "main",
					HeadRefName:    "foo",
					HeadRepository: repository,
					URL: githubv4.URI{URL: &url.URL{
						Scheme: "https",
						Host:   "github.com",
						Path:   "/" + repository.Path().String() + "/pull/123",
					}},
				},
				"bar": {
					Number:         456,
					BaseRefName:    "main",
					HeadRefName:    "bar",
					HeadRepository: repository,
					URL: githubv4.URI{URL: &url.URL{
						Scheme: "https",
						Host:   "github.com",
						Path:   "/" + repository.Path().String() + "/pull/456",
					}},
				},
			},
			want: map[string]string{
				"foo": stackFooterPreamble +
					stackFooterRelatedSectionIntro +
					" 1. *→ this pull request ←*\n" +
					" 2. #456\n",
				"bar": stackFooterPreamble +
					fmt.Sprintf(stackFooterChangesSection, "#123", "2 commits", "https://github.com/"+repository.Path().String()+"/pull/456/changes/4567..89ab") +
					stackFooterRelatedSectionIntro +
					" 1. #123\n" +
					" 2. *→ this pull request ←*\n",
			},
		},
		{
			name: "ForkPoint",
			graph: mustDiffGraph(t,
				[]*jujutsu.Commit{
					{
						ID:      jujutsu.CommitID{0xcd, 0xef},
						Parents: []jujutsu.CommitID{{0x45, 0x67}},
					},
					{
						ID:      jujutsu.CommitID{0x89, 0xab},
						Parents: []jujutsu.CommitID{{0x45, 0x67}},
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
				[]*jujutsu.Bookmark{
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
				[]string{"c", "d"},
			),
			prs: map[string]*github.PullRequest{
				"a": {
					Number:         123,
					BaseRefName:    "main",
					HeadRefName:    "a",
					HeadRepository: repository,
					URL: githubv4.URI{URL: &url.URL{
						Scheme: "https",
						Host:   "github.com",
						Path:   "/" + repository.Path().String() + "/pull/123",
					}},
				},
				"b": {
					Number:         4567,
					BaseRefName:    "main",
					HeadRefName:    "b",
					HeadRepository: repository,
					URL: githubv4.URI{URL: &url.URL{
						Scheme: "https",
						Host:   "github.com",
						Path:   "/" + repository.Path().String() + "/pull/4567",
					}},
				},
				"c": {
					Number:         8910,
					BaseRefName:    "main",
					HeadRefName:    "c",
					HeadRepository: repository,
					URL: githubv4.URI{URL: &url.URL{
						Scheme: "https",
						Host:   "github.com",
						Path:   "/" + repository.Path().String() + "/pull/8910",
					}},
				},
				"d": {
					Number:         9999,
					BaseRefName:    "main",
					HeadRefName:    "d",
					HeadRepository: repository,
					URL: githubv4.URI{URL: &url.URL{
						Scheme: "https",
						Host:   "github.com",
						Path:   "/" + repository.Path().String() + "/pull/9999",
					}},
				},
			},
			want: map[string]string{
				"a": stackFooterPreamble +
					stackFooterRelatedSectionIntro +
					" 1. *→ this pull request ←*\n" +
					" 2. #4567\n" +
					" 3. …multiple pull requests…\n",
				"b": stackFooterPreamble +
					fmt.Sprintf(stackFooterChangesSection, "#123", "commit", "https://github.com/"+repository.Path().String()+"/pull/4567/changes/4567") +
					stackFooterRelatedSectionIntro +
					" 1. #123\n" +
					" 2. *→ this pull request ←*\n" +
					"\n" +
					stackFooterChildrenIntro +
					"- #8910\n" +
					"- #9999\n",
				"c": stackFooterPreamble +
					fmt.Sprintf(stackFooterChangesSection, "#4567", "commit", "https://github.com/"+repository.Path().String()+"/pull/8910/changes/89ab") +
					stackFooterRelatedSectionIntro +
					" 1. #123\n" +
					" 2. #4567\n" +
					" 3. *→ this pull request ←*\n",
				"d": stackFooterPreamble +
					fmt.Sprintf(stackFooterChangesSection, "#4567", "commit", "https://github.com/"+repository.Path().String()+"/pull/9999/changes/cdef") +
					stackFooterRelatedSectionIntro +
					" 1. #123\n" +
					" 2. #4567\n" +
					" 3. *→ this pull request ←*\n",
			},
		},
		{
			name: "ImmediateForkPoint",
			graph: mustDiffGraph(t,
				[]*jujutsu.Commit{
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
				[]*jujutsu.Bookmark{
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
				[]string{"b", "c"},
			),
			prs: map[string]*github.PullRequest{
				"a": {
					Number:         123,
					BaseRefName:    "main",
					HeadRefName:    "a",
					HeadRepository: repository,
					URL: githubv4.URI{URL: &url.URL{
						Scheme: "https",
						Host:   "github.com",
						Path:   "/" + repository.Path().String() + "/pull/123",
					}},
				},
				"b": {
					Number:         4567,
					BaseRefName:    "main",
					HeadRefName:    "b",
					HeadRepository: repository,
					URL: githubv4.URI{URL: &url.URL{
						Scheme: "https",
						Host:   "github.com",
						Path:   "/" + repository.Path().String() + "/pull/4567",
					}},
				},
				"c": {
					Number:         8910,
					BaseRefName:    "main",
					HeadRefName:    "c",
					HeadRepository: repository,
					URL: githubv4.URI{URL: &url.URL{
						Scheme: "https",
						Host:   "github.com",
						Path:   "/" + repository.Path().String() + "/pull/8910",
					}},
				},
			},
			want: map[string]string{
				"a": stackFooterPreamble +
					stackFooterRelatedSectionIntro +
					stackFooterChildrenIntro +
					"- #4567\n" +
					"- #8910\n",
				"b": stackFooterPreamble +
					fmt.Sprintf(stackFooterChangesSection, "#123", "commit", "https://github.com/"+repository.Path().String()+"/pull/4567/changes/4567") +
					stackFooterRelatedSectionIntro +
					" 1. #123\n" +
					" 2. *→ this pull request ←*\n",
				"c": stackFooterPreamble +
					fmt.Sprintf(stackFooterChangesSection, "#123", "commit", "https://github.com/"+repository.Path().String()+"/pull/8910/changes/89ab") +
					stackFooterRelatedSectionIntro +
					" 1. #123\n" +
					" 2. *→ this pull request ←*\n",
			},
		},
		{
			name: "MergePoint2",
			graph: mustDiffGraph(t,
				[]*jujutsu.Commit{
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
				[]*jujutsu.Bookmark{
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
				[]string{"c", "d"},
			),
			prs: map[string]*github.PullRequest{
				"a": {
					Number:         123,
					BaseRefName:    "main",
					HeadRefName:    "a",
					HeadRepository: repository,
					URL: githubv4.URI{URL: &url.URL{
						Scheme: "https",
						Host:   "github.com",
						Path:   "/" + repository.Path().String() + "/pull/123",
					}},
				},
				"b": {
					Number:         4567,
					BaseRefName:    "main",
					HeadRefName:    "b",
					HeadRepository: repository,
					URL: githubv4.URI{URL: &url.URL{
						Scheme: "https",
						Host:   "github.com",
						Path:   "/" + repository.Path().String() + "/pull/4567",
					}},
				},
				"c": {
					Number:         8910,
					BaseRefName:    "main",
					HeadRefName:    "c",
					HeadRepository: repository,
					URL: githubv4.URI{URL: &url.URL{
						Scheme: "https",
						Host:   "github.com",
						Path:   "/" + repository.Path().String() + "/pull/8910",
					}},
				},
				"d": {
					Number:         9999,
					BaseRefName:    "main",
					HeadRefName:    "d",
					HeadRepository: repository,
					URL: githubv4.URI{URL: &url.URL{
						Scheme: "https",
						Host:   "github.com",
						Path:   "/" + repository.Path().String() + "/pull/9999",
					}},
				},
			},
			want: map[string]string{
				"a": stackFooterPreamble +
					stackFooterRelatedSectionIntro +
					" 1. *→ this pull request ←*\n" +
					" 2. #8910\n" +
					" 3. #9999\n",
				"b": stackFooterPreamble +
					stackFooterRelatedSectionIntro +
					" 1. *→ this pull request ←*\n" +
					" 2. #8910\n" +
					" 3. #9999\n",
				"c": stackFooterPreamble +
					fmt.Sprintf(stackFooterChangesSection, "#123 and #4567", "commit", "https://github.com/"+repository.Path().String()+"/pull/8910/changes/89ab") +
					stackFooterRelatedSectionIntro +
					stackFooterParentsIntro +
					"- #123\n" +
					"- #4567\n" +
					"\n" +
					stackFooterChildrenIntro +
					"- #9999\n",
				"d": stackFooterPreamble +
					fmt.Sprintf(stackFooterChangesSection, "#8910", "commit", "https://github.com/"+repository.Path().String()+"/pull/9999/changes/cdef") +
					stackFooterRelatedSectionIntro +
					"- …multiple pull requests…\n" +
					"- #8910\n" +
					"- *→ this pull request ←*\n",
			},
		},
		{
			name: "MergePoint3",
			graph: mustDiffGraph(t,
				[]*jujutsu.Commit{
					{
						ID:      jujutsu.CommitID{0xcd, 0xef},
						Parents: []jujutsu.CommitID{{0x01, 0x23}, {0x45, 0x67}, {0x89, 0xab}},
					},
					{
						ID:      jujutsu.CommitID{0x89, 0xab},
						Parents: []jujutsu.CommitID{trunkPlaceholderCommitID()},
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
				[]*jujutsu.Bookmark{
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
				[]string{"d"},
			),
			prs: map[string]*github.PullRequest{
				"a": {
					Number:         123,
					BaseRefName:    "main",
					HeadRefName:    "a",
					HeadRepository: repository,
					URL: githubv4.URI{URL: &url.URL{
						Scheme: "https",
						Host:   "github.com",
						Path:   "/" + repository.Path().String() + "/pull/123",
					}},
				},
				"b": {
					Number:         4567,
					BaseRefName:    "main",
					HeadRefName:    "b",
					HeadRepository: repository,
					URL: githubv4.URI{URL: &url.URL{
						Scheme: "https",
						Host:   "github.com",
						Path:   "/" + repository.Path().String() + "/pull/4567",
					}},
				},
				"c": {
					Number:         8910,
					BaseRefName:    "main",
					HeadRefName:    "c",
					HeadRepository: repository,
					URL: githubv4.URI{URL: &url.URL{
						Scheme: "https",
						Host:   "github.com",
						Path:   "/" + repository.Path().String() + "/pull/8910",
					}},
				},
				"d": {
					Number:         9999,
					BaseRefName:    "main",
					HeadRefName:    "d",
					HeadRepository: repository,
					URL: githubv4.URI{URL: &url.URL{
						Scheme: "https",
						Host:   "github.com",
						Path:   "/" + repository.Path().String() + "/pull/9999",
					}},
				},
			},
			want: map[string]string{
				"a": stackFooterPreamble +
					stackFooterRelatedSectionIntro +
					" 1. *→ this pull request ←*\n" +
					" 2. #9999\n",
				"b": stackFooterPreamble +
					stackFooterRelatedSectionIntro +
					" 1. *→ this pull request ←*\n" +
					" 2. #9999\n",
				"c": stackFooterPreamble +
					stackFooterRelatedSectionIntro +
					" 1. *→ this pull request ←*\n" +
					" 2. #9999\n",
				"d": stackFooterPreamble +
					fmt.Sprintf(stackFooterChangesSection, "#123, #4567, and #8910", "commit", "https://github.com/"+repository.Path().String()+"/pull/9999/changes/cdef") +
					stackFooterRelatedSectionIntro +
					stackFooterParentsIntro +
					"- #123\n" +
					"- #4567\n" +
					"- #8910\n",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for diff := range test.graph.walk() {
				diff.pullRequest = test.prs[diff.name]
			}
			artifactsDir := t.ArtifactDir()
			for name, want := range test.want {
				artifactPath := filepath.Join(artifactsDir, test.name+"_"+name+".md")
				if err := os.WriteFile(artifactPath, []byte(want), 0o666); err != nil {
					t.Log(err)
				}

				sd := test.graph.byName(name)
				if sd == nil {
					t.Fatal("No pull request in graph for", name)
					continue
				}
				sb := new(strings.Builder)
				writeStackFooter(sb, sd)
				got := sb.String()
				if diff := cmp.Diff(want, got); diff != "" {
					t.Errorf("footer for %s (-want +got):\n%s", name, diff)
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
