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
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"zombiezen.com/go/jj-domino/internal/jujutsu"
)

func TestEditor(t *testing.T) {
	ctx := t.Context()

	const want = "I edited it!\n"
	command, err := editorCommand(t, []byte(want))
	if err != nil {
		t.Fatal(err)
	}
	argv := command.Argv()
	command = jujutsu.CommandArgv(argv[0], argv[1:], environMap())

	e := &editor{
		command:  command,
		workDir:  t.TempDir(),
		tempRoot: t.TempDir(),
		stderr:   t.Output(),

		logError: func(ctx context.Context, err error) {
			t.Error(err)
		},
	}
	got, err := e.open(ctx, "foo.txt", []byte("This is the initial content.\n"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("(&editor{...}).open(...) = %q; want %q", got, want)
	}
}

func TestEditorDirectory(t *testing.T) {
	ctx := t.Context()

	var command *jujutsu.CommandNameAndArgs
	if runtime.GOOS == "windows" {
		pwshPath, err := exec.LookPath("pwsh")
		if err != nil {
			t.Skip("PowerShell Core missing:", err)
		}
		command = jujutsu.CommandArgv(pwshPath, []string{
			"-CommandWithArgs",
			`(Get-Location).ToString() | Out-File -NoNewline -FilePath $args[0]`,
		}, environMap())
	} else {
		command = jujutsu.CommandArgv("sh", []string{"-c", `pwd | tee "$@"`, "myscript"}, environMap())
	}

	e := &editor{
		command:  command,
		workDir:  t.TempDir(),
		tempRoot: t.TempDir(),
		stderr:   t.Output(),

		logError: func(ctx context.Context, err error) {
			t.Error(err)
		},
	}
	got, err := e.open(ctx, "foo.txt", []byte("This is the initial content.\n"))
	if err != nil {
		t.Fatal(err)
	}
	gotPath := strings.TrimSuffix(string(got), "\n")
	gotPath, err = filepath.EvalSymlinks(gotPath)
	if err != nil {
		t.Fatal(err)
	}
	wantPath, err := filepath.EvalSymlinks(e.workDir)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != wantPath {
		t.Errorf("working directory = %q; want %q", gotPath, wantPath)
	}
}

func editorCommand(tb testing.TB, content []byte) (*jujutsu.CommandNameAndArgs, error) {
	cpPath, err := findCopyProgram()
	if err != nil {
		return nil, fmt.Errorf("editor command: cp not found: %v", err)
	}
	fname := filepath.Join(tb.TempDir(), "msg")
	if err := os.WriteFile(fname, content, 0o600); err != nil {
		return nil, fmt.Errorf("editor command: %v", err)
	}
	return jujutsu.CommandArgv(cpPath, []string{fname}, environMap()), nil
}

var findCopyProgram = sync.OnceValues(func() (string, error) {
	if runtime.GOOS == "windows" {
		return "cp", nil
	}
	return exec.LookPath("cp")
})
