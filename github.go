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
	"errors"
	"os"
	"os/exec"
	"strings"

	"github.com/google/go-github/v81/github"
)

func githubToken() (string, error) {
	// Prefer `gh`, fall back to env vars if not available
	cmd := exec.Command("gh", "auth", "status")
	if err := cmd.Run(); err == nil {
		cmd = exec.Command("gh", "auth", "token")
		var raw []byte
		if raw, err = cmd.Output(); err == nil {
			return strings.TrimSpace(string(raw)), nil
		}
	}
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token, nil
	} else if token := os.Getenv("GH_TOKEN"); token != "" {
		return token, nil
	}
	return "", errors.New("no token found")
}

func getClient() (*github.Client, error) {
	token, err := githubToken()
	if err != nil {
		return nil, err
	}
	return github.NewClient(nil).WithAuthToken(token), nil
}
