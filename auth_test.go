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
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
)

func TestAuthGitHubLoginCmd(t *testing.T) {
	const fakeToken = "foo123456"
	configHome := t.TempDir()
	c := &cli{
		stdin: strings.NewReader(fakeToken + "\n"),
		environ: map[string]string{
			"XDG_CONFIG_HOME": configHome,
			"AppData":         configHome,
		},
		lookPath: stubLookPath,

		Auth: authCmd{
			GitHubLogin: authGitHubLoginCmd{
				Verify: false,
			},
		},
	}

	ctx := testContext(t)
	stdout := new(strings.Builder)
	stderr := new(strings.Builder)
	k := &kong.Kong{
		Stdout: stdout,
		Stderr: stderr,
	}
	if err := c.Auth.GitHubLogin.Run(ctx, k, c); err != nil {
		t.Error("Run:", err)
	}

	path := filepath.Join(configHome, configSubdirName, "github-token")
	got, err := os.ReadFile(path)
	if want := fakeToken + "\n"; string(got) != want || err != nil {
		t.Errorf("os.ReadFile(%q) = %q, %v; want %q, <nil>", path, got, err, want)
	}
}

func TestGitHubToken(t *testing.T) {
	configHome := t.TempDir()
	appConfigDir := filepath.Join(configHome, configSubdirName)
	if err := os.Mkdir(appConfigDir, 0o777); err != nil {
		t.Fatal(err)
	}
	const fileToken = "__filetoken__"
	fileTokenPath := filepath.Join(appConfigDir, "github-token")
	if err := os.WriteFile(fileTokenPath, []byte(fileToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	emptyDir := t.TempDir()

	configHomeEnvVar := "XDG_CONFIG_HOME"
	configDirsEnvVar := "XDG_CONFIG_DIRS"
	if runtime.GOOS == "windows" {
		configHomeEnvVar = "AppData"
	}

	tests := []struct {
		name string
		env  map[string]string
		want string
		err  bool

		skipOnWindows bool
	}{
		{
			name: "EmptyEnviron",
			env: map[string]string{
				configDirsEnvVar: emptyDir,
			},
			err: true,
		},
		{
			name: "EnvVar",
			env: map[string]string{
				"GITHUB_TOKEN":   "foo123456",
				configDirsEnvVar: emptyDir,
			},
			want: "foo123456",
		},
		{
			name: "FileInHome",
			env: map[string]string{
				configHomeEnvVar: configHome,
				configDirsEnvVar: emptyDir,
			},
			want: fileToken,
		},
		{
			name: "FileInFallbackDir",
			env: map[string]string{
				configHomeEnvVar: emptyDir,
				configDirsEnvVar: configHome,
			},
			want:          fileToken,
			skipOnWindows: true,
		},
		{
			name: "EnvVarOverride",
			env: map[string]string{
				"GITHUB_TOKEN":   "foo123456",
				configHomeEnvVar: configHome,
				configDirsEnvVar: emptyDir,
			},
			want: "foo123456",
		},
		{
			name: "FileMissing",
			env: map[string]string{
				configHomeEnvVar: emptyDir,
				configDirsEnvVar: emptyDir,
			},
			err: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.skipOnWindows && runtime.GOOS == "windows" {
				t.Skip("Skip on Windows")
			}

			ctx := testContext(t)
			lookPath := lookPathFunc(stubLookPath)
			got, err := gitHubToken(ctx, test.env, lookPath)
			if test.err && err == nil {
				t.Errorf("gitHubToken(...) = %q, <nil>; want _, <error>", got)
			} else if !test.err && (got != test.want || err != nil) {
				t.Errorf("gitHubToken(...) = %q, %v; want %q, <nil>", got, err, test.want)
			}
		})
	}
}

func stubLookPath(file string) (string, error) {
	return "", &exec.Error{
		Name: file,
		Err:  exec.ErrNotFound,
	}
}
