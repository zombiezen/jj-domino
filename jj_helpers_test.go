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
	"testing"

	"github.com/go-json-experiment/json/jsontext"
	"zombiezen.com/go/jj-domino/internal/jujutsu"
)

func TestResolveTrunk(t *testing.T) {
	fakeCommitID := jujutsu.CommitID{0xde, 0xad, 0xbe, 0xef}

	tests := []struct {
		name      string
		settings  map[string]jsontext.Value
		bookmarks []*jujutsu.Bookmark
		want      jujutsu.RefSymbol
		err       bool
	}{
		{
			name: "Nothing",
			err:  true,
		},
		{
			name: "NoRemote",
			bookmarks: []*jujutsu.Bookmark{
				{Name: "foo", TargetMerge: jujutsu.Resolved(fakeCommitID)},
				{Name: "bar", TargetMerge: jujutsu.Resolved(fakeCommitID)},
			},
			err: true,
		},
		{
			name: "Implicit",
			bookmarks: []*jujutsu.Bookmark{
				{Name: "foo", TargetMerge: jujutsu.Resolved(fakeCommitID)},
				{Name: "bar", TargetMerge: jujutsu.Resolved(fakeCommitID)},
				{Name: "main", Remote: "origin", TargetMerge: jujutsu.Resolved(fakeCommitID)},
			},
			want: jujutsu.RefSymbol{Name: "main", Remote: "origin"},
		},
		{
			name: "ExplicitlySetLocal",
			settings: map[string]jsontext.Value{
				`revset-aliases."trunk()"`: jsontext.Value(`"foo"`),
			},
			bookmarks: []*jujutsu.Bookmark{
				{Name: "foo", TargetMerge: jujutsu.Resolved(fakeCommitID)},
				{Name: "bar", TargetMerge: jujutsu.Resolved(fakeCommitID)},
				{Name: "main", Remote: "origin", TargetMerge: jujutsu.Resolved(fakeCommitID)},
			},
			want: jujutsu.RefSymbol{Name: "foo"},
		},
		{
			name: "ExplicitlySetRemote",
			settings: map[string]jsontext.Value{
				`revset-aliases."trunk()"`: jsontext.Value(`"foo@origin"`),
			},
			bookmarks: []*jujutsu.Bookmark{
				{Name: "bar", TargetMerge: jujutsu.Resolved(fakeCommitID)},
				{Name: "main", Remote: "origin", TargetMerge: jujutsu.Resolved(fakeCommitID)},
				{Name: "foo", Remote: "origin", TargetMerge: jujutsu.Resolved(fakeCommitID)},
			},
			want: jujutsu.RefSymbol{Name: "foo", Remote: "origin"},
		},
		{
			name: "AmbiguousDefault",
			bookmarks: []*jujutsu.Bookmark{
				{Name: "main", Remote: "origin", TargetMerge: jujutsu.Resolved(fakeCommitID)},
				{Name: "trunk", Remote: "origin", TargetMerge: jujutsu.Resolved(fakeCommitID)},
			},
			err: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := resolveTrunk(test.settings, test.bookmarks)
			if !test.err && (got != test.want || err != nil) {
				t.Errorf("resolveTrunk(...) = %v, %v; want %v, <nil>", got, err, test.want)
			} else if test.err && err == nil {
				t.Errorf("resolveTrunk(...) = %v, <nil>; want _, <error>", got)
			}
		})
	}
}
