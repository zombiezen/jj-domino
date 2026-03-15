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

package jujutsu

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

func TestShowFile(t *testing.T) {
	ctx := t.Context()
	jjExe := findJJExecutable(t)

	t.Run("Exists", func(t *testing.T) {
		repoDir := t.TempDir()
		jj, err := New(Options{
			Dir:   repoDir,
			JJExe: jjExe,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := jj.GitInit(ctx, GitInitOptions{}); err != nil {
			t.Fatal(err)
		}
		const filename = "hello.txt"
		const want = "Hello, World!\n"
		if err := os.WriteFile(filepath.Join(repoDir, filename), []byte(want), 0o666); err != nil {
			t.Fatal(err)
		}

		rc, err := jj.ShowFile(ctx, "@", filename)
		if err != nil {
			t.Fatal("ShowFile:", err)
		}
		got, err := io.ReadAll(rc)
		if err != nil {
			t.Error("ReadAll:", err)
		}
		if err := rc.Close(); err != nil {
			t.Error("Close:", err)
		}
		if string(got) != want {
			t.Errorf("content = %q; want %q", got, want)
		}
	})

	t.Run("DoesNotExist", func(t *testing.T) {
		repoDir := t.TempDir()
		jj, err := New(Options{
			Dir:   repoDir,
			JJExe: jjExe,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := jj.GitInit(ctx, GitInitOptions{}); err != nil {
			t.Fatal(err)
		}

		rc, err := jj.ShowFile(ctx, "@", "foo.txt")
		if rc != nil {
			rc.Close()
			t.Error("ShowFile returned a non-nil io.ReadCloser")
		}
		if err == nil {
			t.Error("ShowFile did not return an error")
		} else {
			t.Log("ShowFile:", err)
		}
	})
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
