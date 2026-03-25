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
	"net/url"
	"testing"

	"github.com/shurcooL/githubv4"
	"zombiezen.com/go/jj-domino/internal/jujutsu"
)

func TestPullRequestChangesURL(t *testing.T) {
	tests := []struct {
		pullRequestURL string
		from           jujutsu.CommitID
		to             jujutsu.CommitID
		want           string
	}{
		{
			pullRequestURL: "https://github.com/zombiezen/jj-domino/pull/50",
			want:           "https://github.com/zombiezen/jj-domino/pull/50/changes",
		},
		{
			pullRequestURL: "https://github.com/zombiezen/jj-domino/pull/50",
			from: jujutsu.CommitID{
				0xb2, 0x0b, 0xca, 0x37, 0x03, 0x0c, 0xf5, 0x98, 0x4a, 0x5c,
				0x1b, 0x48, 0xd4, 0xce, 0x09, 0xf9, 0x42, 0x65, 0x1b, 0xe0,
			},
			to: jujutsu.CommitID{
				0x18, 0xc4, 0x2a, 0x85, 0x59, 0x3e, 0xf7, 0x9d, 0x57, 0x6a,
				0x41, 0x85, 0xb4, 0xbc, 0xb3, 0x84, 0x6f, 0x13, 0xc2, 0x16,
			},
			want: "https://github.com/zombiezen/jj-domino/pull/50/changes/b20bca37030cf5984a5c1b48d4ce09f942651be0..18c42a85593ef79d576a4185b4bcb3846f13c216",
		},
		{
			pullRequestURL: "https://github.com/zombiezen/jj-domino/pull/50",
			from: jujutsu.CommitID{
				0x18, 0xc4, 0x2a, 0x85, 0x59, 0x3e, 0xf7, 0x9d, 0x57, 0x6a,
				0x41, 0x85, 0xb4, 0xbc, 0xb3, 0x84, 0x6f, 0x13, 0xc2, 0x16,
			},
			to: jujutsu.CommitID{
				0x18, 0xc4, 0x2a, 0x85, 0x59, 0x3e, 0xf7, 0x9d, 0x57, 0x6a,
				0x41, 0x85, 0xb4, 0xbc, 0xb3, 0x84, 0x6f, 0x13, 0xc2, 0x16,
			},
			want: "https://github.com/zombiezen/jj-domino/pull/50/changes/18c42a85593ef79d576a4185b4bcb3846f13c216",
		},
	}

	for _, test := range tests {
		u, err := url.Parse(test.pullRequestURL)
		if err != nil {
			t.Error(err)
			continue
		}

		pr := &pullRequest{URL: githubv4.URI{URL: u}}
		if got := pr.changesURL(test.from, test.to).String(); got != test.want {
			t.Errorf("(&pullRequest{URL: %s}).changesURL(%v, %v) = %s; want %s",
				test.pullRequestURL, test.from, test.to, got, test.want)
		}
	}
}
