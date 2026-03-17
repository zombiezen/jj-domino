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
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"zombiezen.com/go/jj-domino/internal/jujutsu"
	"zombiezen.com/go/jj-domino/internal/sigterm"
)

type editor struct {
	command  *jujutsu.CommandNameAndArgs
	workDir  string
	tempRoot string
	stdin    io.Reader
	stdout   io.Writer
	stderr   io.Writer

	logError func(context.Context, error)
}

// open opens the editor on a temporary file with the given initial content,
// waits for it to finish,
// then returns the content of the file.
func (e *editor) open(ctx context.Context, basename string, initial []byte) (edited []byte, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("open editor: %v", err)
		}
	}()

	editDir, err := os.MkdirTemp(e.tempRoot, "jj-domino-editor")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := os.RemoveAll(editDir); err != nil {
			if e.logError != nil {
				e.logError(ctx, err)
			}
		}
	}()
	path := filepath.Join(editDir, basename)
	if err := os.WriteFile(path, initial, 0o600); err != nil {
		return nil, err
	}
	argv := e.command.Argv()
	argv = append(argv, path)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Cancel = func() error { return sigterm.CancelProcess(cmd.Process) }
	cmd.Dir = e.workDir
	cmd.Stdin = e.stdin
	cmd.Stdout = e.stdout
	cmd.Stderr = e.stderr
	cmd.Env = []string{}
	for k, v := range e.command.Environ() {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	edited, err = os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read result: %v", err)
	}
	return edited, nil
}
