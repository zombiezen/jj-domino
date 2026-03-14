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
	"os/exec"
	"testing"
)

func TestGitHubToken(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
		err  bool
	}{
		{
			name: "EmptyEnviron",
			env:  map[string]string{},
			err:  true,
		},
		{
			name: "EnvVar",
			env: map[string]string{
				"GITHUB_TOKEN": "foo123456",
			},
			want: "foo123456",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lookupEnv := lookupEnvFunc(func(key string) (string, bool) {
				v, ok := test.env[key]
				return v, ok
			})
			lookPath := lookPathFunc(func(file string) (string, error) {
				return "", &exec.Error{
					Name: file,
					Err:  exec.ErrNotFound,
				}
			})

			got, err := gitHubToken(context.Background(), lookupEnv, lookPath)
			if test.err && err == nil {
				t.Errorf("gitHubToken(...) = %q, <nil>; want _, <error>", got)
			} else if !test.err && (got != test.want || err != nil) {
				t.Errorf("gitHubToken(...) = %q, %v; want %q, <nil>", got, err, test.want)
			}
		})
	}
}
