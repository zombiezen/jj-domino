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

package commitgraph

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"zombiezen.com/go/jj-domino/internal/jujutsu"
)

func TestCommitGraphHeads(t *testing.T) {
	tests := []struct {
		name string
		g    []*jujutsu.Commit
		x    []*jujutsu.Commit
		want []*jujutsu.Commit
	}{
		{
			name: "Empty",
		},
		{
			name: "SingleCommit",
			x: []*jujutsu.Commit{
				{ID: jujutsu.CommitID{0x01, 0x23}},
			},
			want: []*jujutsu.Commit{
				{ID: jujutsu.CommitID{0x01, 0x23}},
			},
		},
		{
			name: "DistinctCommits",
			x: []*jujutsu.Commit{
				{ID: jujutsu.CommitID{0x01, 0x23}},
				{ID: jujutsu.CommitID{0x45, 0x67}},
			},
			want: []*jujutsu.Commit{
				{ID: jujutsu.CommitID{0x01, 0x23}},
				{ID: jujutsu.CommitID{0x45, 0x67}},
			},
		},
		{
			name: "TwoChain",
			x: []*jujutsu.Commit{
				{ID: jujutsu.CommitID{0x01, 0x23}},
				{ID: jujutsu.CommitID{0x45, 0x67}, Parents: []jujutsu.CommitID{{0x01, 0x23}}},
			},
			want: []*jujutsu.Commit{
				{ID: jujutsu.CommitID{0x45, 0x67}, Parents: []jujutsu.CommitID{{0x01, 0x23}}},
			},
		},
		{
			name: "OnTrunk",
			x: []*jujutsu.Commit{
				{ID: jujutsu.CommitID{0x01, 0x23}, Parents: []jujutsu.CommitID{trunkPlaceholderCommitID()}},
				{ID: jujutsu.CommitID{0x45, 0x67}, Parents: []jujutsu.CommitID{{0x01, 0x23}}},
			},
			want: []*jujutsu.Commit{
				{ID: jujutsu.CommitID{0x45, 0x67}, Parents: []jujutsu.CommitID{{0x01, 0x23}}},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := New(slices.Values(test.g)).Heads(slices.Values(test.x))
			want := New(slices.Values(test.want))
			if diff := cmp.Diff(want, got, commitGraphOption()); diff != "" {
				t.Errorf("-want +got:\n%s", diff)
			}
		})
	}
}

func TestCommitGraphRoots(t *testing.T) {
	tests := []struct {
		name string
		g    []*jujutsu.Commit
		x    []*jujutsu.Commit
		want []*jujutsu.Commit
	}{
		{
			name: "Empty",
		},
		{
			name: "SingleCommit",
			x: []*jujutsu.Commit{
				{ID: jujutsu.CommitID{0x01, 0x23}},
			},
			want: []*jujutsu.Commit{
				{ID: jujutsu.CommitID{0x01, 0x23}},
			},
		},
		{
			name: "DistinctCommits",
			x: []*jujutsu.Commit{
				{ID: jujutsu.CommitID{0x01, 0x23}},
				{ID: jujutsu.CommitID{0x45, 0x67}},
			},
			want: []*jujutsu.Commit{
				{ID: jujutsu.CommitID{0x01, 0x23}},
				{ID: jujutsu.CommitID{0x45, 0x67}},
			},
		},
		{
			name: "TwoChain",
			x: []*jujutsu.Commit{
				{ID: jujutsu.CommitID{0x01, 0x23}},
				{ID: jujutsu.CommitID{0x45, 0x67}, Parents: []jujutsu.CommitID{{0x01, 0x23}}},
			},
			want: []*jujutsu.Commit{
				{ID: jujutsu.CommitID{0x01, 0x23}},
			},
		},
		{
			name: "OnTrunk",
			x: []*jujutsu.Commit{
				{ID: jujutsu.CommitID{0x01, 0x23}, Parents: []jujutsu.CommitID{trunkPlaceholderCommitID()}},
				{ID: jujutsu.CommitID{0x45, 0x67}, Parents: []jujutsu.CommitID{{0x01, 0x23}}},
			},
			want: []*jujutsu.Commit{
				{ID: jujutsu.CommitID{0x01, 0x23}, Parents: []jujutsu.CommitID{trunkPlaceholderCommitID()}},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := New(slices.Values(test.g)).Roots(slices.Values(test.x))
			want := New(slices.Values(test.want))
			if diff := cmp.Diff(want, got, commitGraphOption()); diff != "" {
				t.Errorf("-want +got:\n%s", diff)
			}
		})
	}
}

func TestCommitGraphSort(t *testing.T) {
	tests := []struct {
		name    string
		commits []*jujutsu.Commit
		err     bool
	}{
		{
			name: "Empty",
		},
		{
			name: "SingleCommit",
			commits: []*jujutsu.Commit{
				{ID: jujutsu.CommitID{0x01, 0x02}},
			},
		},
		{
			name: "UnknownParent",
			commits: []*jujutsu.Commit{
				{ID: jujutsu.CommitID{0x01, 0x02}, Parents: []jujutsu.CommitID{trunkPlaceholderCommitID()}},
			},
		},
		{
			name: "LinearCommits",
			commits: []*jujutsu.Commit{
				{ID: jujutsu.CommitID{0x45, 0x67}, Parents: []jujutsu.CommitID{{0x01, 0x23}}},
				{ID: jujutsu.CommitID{0x01, 0x23}},
				{ID: jujutsu.CommitID{0x89, 0xab}, Parents: []jujutsu.CommitID{{0x45, 0x67}}},
			},
		},
		{
			name: "LinearCommits",
			commits: []*jujutsu.Commit{
				{ID: jujutsu.CommitID{0x45, 0x67}, Parents: []jujutsu.CommitID{{0x01, 0x23}}},
				{ID: jujutsu.CommitID{0x01, 0x23}},
				{ID: jujutsu.CommitID{0x89, 0xab}, Parents: []jujutsu.CommitID{{0x45, 0x67}}},
			},
		},
		{
			name: "Diamond",
			commits: []*jujutsu.Commit{
				{ID: jujutsu.CommitID{0x45, 0x67}, Parents: []jujutsu.CommitID{{0x01, 0x23}}},
				{ID: jujutsu.CommitID{0x01, 0x23}},
				{ID: jujutsu.CommitID{0x89, 0xab}, Parents: []jujutsu.CommitID{{0x01, 0x23}}},
				{ID: jujutsu.CommitID{0xcd, 0xef}, Parents: []jujutsu.CommitID{{0x45, 0x67}, {0x89, 0xab}}},
			},
		},
		{
			name: "RedundantMerge",
			commits: []*jujutsu.Commit{
				{ID: jujutsu.CommitID{0xcd, 0xef}, Parents: []jujutsu.CommitID{{0x89, 0xab}, {0x01, 0x23}}},
				{ID: jujutsu.CommitID{0x89, 0xab}, Parents: []jujutsu.CommitID{{0x45, 0x67}}},
				{ID: jujutsu.CommitID{0x45, 0x67}, Parents: []jujutsu.CommitID{{0x01, 0x23}}},
				{ID: jujutsu.CommitID{0x01, 0x23}},
			},
		},
		{
			name: "RedundantMergeFlipped",
			commits: []*jujutsu.Commit{
				{ID: jujutsu.CommitID{0xcd, 0xef}, Parents: []jujutsu.CommitID{{0x01, 0x23}, {0x89, 0xab}}},
				{ID: jujutsu.CommitID{0x89, 0xab}, Parents: []jujutsu.CommitID{{0x45, 0x67}}},
				{ID: jujutsu.CommitID{0x45, 0x67}, Parents: []jujutsu.CommitID{{0x01, 0x23}}},
				{ID: jujutsu.CommitID{0x01, 0x23}},
			},
		},
		{
			name: "SelfCycle",
			commits: []*jujutsu.Commit{
				{ID: jujutsu.CommitID{0x01, 0x23}, Parents: []jujutsu.CommitID{{0x01, 0x23}}},
			},
			err: true,
		},
		{
			name: "TwoCycle",
			commits: []*jujutsu.Commit{
				{ID: jujutsu.CommitID{0x45, 0x67}, Parents: []jujutsu.CommitID{{0x01, 0x23}}},
				{ID: jujutsu.CommitID{0x01, 0x23}, Parents: []jujutsu.CommitID{{0x45, 0x67}}},
			},
			err: true,
		},
		{
			name: "CycleWithHead",
			commits: []*jujutsu.Commit{
				{ID: jujutsu.CommitID{0x45, 0x67}, Parents: []jujutsu.CommitID{{0x01, 0x23}}},
				{ID: jujutsu.CommitID{0x01, 0x23}, Parents: []jujutsu.CommitID{{0x45, 0x67}}},
				{ID: jujutsu.CommitID{0x89, 0xab}, Parents: []jujutsu.CommitID{{0x45, 0x67}}},
			},
			err: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := New(slices.Values(test.commits))
			got, err := g.Sort()
			if err != nil {
				t.Log("g.sort():", err)
				if !test.err {
					t.Fail()
				}
				return
			}
			if test.err {
				t.Fatal("g.sort() did not return an error")
			}
			for i, c1 := range got {
				for _, c2 := range got[i+1:] {
					if slices.ContainsFunc(c2.Parents, c1.ID.Equal) {
						if !t.Failed() {
							sb := new(strings.Builder)
							sb.WriteString("Ordering: [")
							for i, c := range got {
								if i > 0 {
									sb.WriteString(" ")
								}
								sb.WriteString(c.ID.String())
							}
							sb.WriteString("]")
							t.Log(sb.String())
						}
						t.Errorf("%v showed up after %v", c2.ID, c1.ID)
					}
				}
			}
		})
	}
}

func commitGraphOption() cmp.Option {
	return cmp.Options{
		cmp.AllowUnexported(Graph{}),
		cmp.FilterValues(
			func(a, b *Graph) bool {
				return a.Len() == 0 || b.Len() == 0
			},
			cmp.Comparer(func(a, b *Graph) bool {
				return a.Len() == b.Len()
			}),
		),
		cmp.FilterPath(func(p cmp.Path) bool {
			return p.Index(-2).Type() == reflect.TypeFor[Graph]() &&
				p.Last().(cmp.StructField).Name() == "commits"
		}, cmpopts.EquateEmpty()),
	}
}

func trunkPlaceholderCommitID() jujutsu.CommitID {
	return jujutsu.CommitID{0xde, 0xad, 0xbe, 0xef, 0xca, 0xfe}
}
