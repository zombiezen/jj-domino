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

// Package jujutsu provides a high-level interface for interacting with a [Jujutsu] subprocess.
package jujutsu

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	jsonv2 "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
)

// Jujutsu is a context for performing Jujutsu version control operations.
type Jujutsu struct {
	exe string
	env []string
	dir string
}

// Options holds the parameters for [New].
type Options struct {
	// Dir is the working directory to run the Jujutsu subprocess from.
	// If empty, uses this process's working directory.
	Dir string

	// Env specifies the environment of the subprocess.
	// If Env == nil, then the process's environment will be used.
	Env []string

	// JJExe is the name of or a path to a Jujutsu executable.
	// It is treated in the same manner as the argument to exec.LookPath.
	// An empty string is treated the same as "jj".
	JJExe string
}

// New returns a new Jujutsu context with the given [Options].
func New(opts Options) (*Jujutsu, error) {
	jj := &Jujutsu{
		exe: cmp.Or(opts.JJExe, "jj"),
		env: opts.Env,
		dir: opts.Dir,
	}
	var err error
	jj.exe, err = exec.LookPath(jj.exe)
	if err != nil {
		return nil, fmt.Errorf("init jj: %v", err)
	}
	if jj.env == nil {
		jj.env = os.Environ()
	}
	if jj.dir != "" {
		var err error
		jj.dir, err = filepath.Abs(jj.dir)
		if err != nil {
			return nil, fmt.Errorf("init jj: %v", err)
		}
	}
	return jj, nil
}

func (jj *Jujutsu) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, jj.exe, args...)
	cmd.Dir = jj.dir
	cmd.Env = jj.env
	cmd.Stderr = os.Stderr
	return cmd
}

type Bookmark struct {
	Name        string          `json:"name"`
	Remote      string          `json:"remote"`
	TargetMerge Merge[CommitID] `json:"target"`
}

func (jj *Jujutsu) ListBookmarks(ctx context.Context) ([]*Bookmark, error) {
	out, err := jj.command(ctx, "bookmark", "list", "--all", "--ignore-working-copy", "--template", "json(self)").Output()
	if err != nil {
		return nil, fmt.Errorf("jj bookmark list: %v", err)
	}
	decoder := jsontext.NewDecoder(bytes.NewReader(out))
	var bookmarks []*Bookmark
	for decoder.PeekKind() != jsontext.KindInvalid {
		b := new(Bookmark)
		if err := jsonv2.UnmarshalDecode(decoder, b); err != nil {
			return nil, err
		}
		bookmarks = append(bookmarks, b)
	}
	if _, err := decoder.ReadToken(); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("jj bookmark list: %v", err)
	}
	return bookmarks, nil
}

// Commit represents a snapshot of the repository at a given point in time
// with some metadata.
//
// https://docs.jj-vcs.dev/latest/glossary/#commit
type Commit struct {
	ID          CommitID   `json:"commit_id"`
	ChangeID    ChangeID   `json:"change_id"`
	Description string     `json:"description"`
	Parents     []CommitID `json:"parents"`
}

func (jj *Jujutsu) Log(ctx context.Context, revset string, yield func(*Commit) bool) error {
	cmd := jj.command(ctx, "log", "--ignore-working-copy", "--no-graph", "--template", "json(self)", "--revisions", revset)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("jj log: %v", err)
	}
	defer stdout.Close()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("jj log: %v", err)
	}

	decoder := jsontext.NewDecoder(stdout)
	for decoder.PeekKind() != jsontext.KindInvalid {
		c := new(Commit)
		if err := jsonv2.UnmarshalDecode(decoder, c); err != nil {
			stdout.Close()
			cmd.Wait()
			return fmt.Errorf("jj log: %v", err)
		}
		if !yield(c) {
			stdout.Close()
			cmd.Wait()
			return nil
		}
	}
	if _, err := decoder.ReadToken(); err != nil && !errors.Is(err, io.EOF) {
		stdout.Close()
		cmd.Wait()
		return fmt.Errorf("jj log: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("jj log: %v", err)
	}
	return nil
}

func (jj *Jujutsu) WorkspaceRoot(ctx context.Context) (string, error) {
	out, err := jj.command(ctx, "workspace", "root", "--ignore-working-copy").Output()
	if err != nil {
		return "", fmt.Errorf("jj workspace root: %v", err)
	}
	return string(bytes.TrimSuffix(out, []byte("\n"))), nil
}
