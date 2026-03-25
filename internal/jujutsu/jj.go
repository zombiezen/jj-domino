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
	"strconv"
	"strings"
	"time"

	jsonv2 "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
	"zombiezen.com/go/jj-domino/internal/cmderror"
	"zombiezen.com/go/jj-domino/internal/sigterm"
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
	cmd.Cancel = func() error { return sigterm.CancelProcess(cmd.Process) }
	return cmd
}

// Invocation holds the parameters for a Jujutsu process.
type Invocation struct {
	// Args is the list of arguments to Jujutsu.
	// It does not include a leading "jj" argument.
	Args []string

	// Stdout and Stderr specify the Jujutsu process's standard output and error.
	//
	// If either is nil, RunJJ connects the corresponding file descriptor
	// to the null device.
	//
	// If Stdout and Stderr are the same writer,
	// and have a type that can be compared with ==,
	// at most one goroutine at a time will call Write.
	Stdout io.Writer
	Stderr io.Writer
}

// RunJJ executes a Jujutsu process.
func (jj *Jujutsu) RunJJ(ctx context.Context, invocation *Invocation) error {
	cmd := jj.command(ctx, invocation.Args...)
	cmd.Stdout = invocation.Stdout
	cmd.Stderr = invocation.Stderr
	return cmd.Run()
}

// GitInitOptions is the set of optional parameters to [*Jujutsu.GitInit].
type GitInitOptions struct {
	Destination string
	Colocate    bool
}

// GitInit creates a new Git-backed repository.
func (jj *Jujutsu) GitInit(ctx context.Context, opts GitInitOptions) error {
	cmd := jj.command(ctx, "git", "init", "--quiet")
	if opts.Colocate {
		cmd.Args = append(cmd.Args, "--colocate")
	} else {
		cmd.Args = append(cmd.Args, "--no-colocate")
	}
	if opts.Destination != "" {
		cmd.Args = append(cmd.Args, "--", opts.Destination)
	}
	return runCommand("jj git init", cmd)
}

// New creates a new change and edits it.
func (jj *Jujutsu) New(ctx context.Context, parents ...string) error {
	args := append([]string{"new", "--"}, parents...)
	cmd := jj.command(ctx, args...)
	return runCommand("jj new", cmd)
}

// SetBookmarkOptions is the set of optional parameters to [*Jujutsu.SetBookmark].
type SetBookmarkOptions struct {
	Revision       string
	AllowBackwards bool
}

// SetBookmark creates a new bookmark or updates an existing one by name.
func (jj *Jujutsu) SetBookmark(ctx context.Context, names []string, opts SetBookmarkOptions) error {
	if len(names) == 0 {
		return fmt.Errorf("jj bookmark set: names empty")
	}
	cmd := jj.command(ctx, "bookmark", "set", "--ignore-working-copy")
	if opts.Revision != "" {
		cmd.Args = append(cmd.Args, "--revision="+opts.Revision)
	}
	if opts.AllowBackwards {
		cmd.Args = append(cmd.Args, "--allow-backwards")
	}
	cmd.Args = append(cmd.Args, "--")
	cmd.Args = append(cmd.Args, names...)

	return runCommand("jj bookmark set", cmd)
}

// DeleteBookmarks deletes bookmarks and propagates the deletion on the next push.
// If names is empty, then DeleteBookmarks returns nil without doing anything.
func (jj *Jujutsu) DeleteBookmarks(ctx context.Context, names []string) error {
	if len(names) == 0 {
		return nil
	}
	args := append([]string{"bookmark", "delete", "--"}, names...)
	cmd := jj.command(ctx, args...)
	return runCommand("jj bookmark delete "+strings.Join(names, " "), cmd)
}

// ReadSettings reads all the active Jujutsu configuration settings.
func (jj *Jujutsu) ReadSettings(ctx context.Context) (map[string]jsontext.Value, error) {
	const template = `json(name) ++ ":" ++ json(value) ++ ","`
	cmd := jj.command(ctx, "config", "list", "--template="+template)
	stdout := new(bytes.Buffer)
	stdout.WriteByte('{')
	cmd.Stdout = stdout

	if err := runCommand("jj config list", cmd); err != nil {
		return nil, err
	}
	jsonData := stdout.Bytes()
	jsonData = bytes.TrimSuffix(jsonData, []byte("\n"))
	jsonData = bytes.TrimSuffix(jsonData, []byte(","))
	jsonData = append(jsonData, '}')
	result := make(map[string]jsontext.Value)
	if err := jsonv2.Unmarshal(jsonData, &result); err != nil {
		return result, fmt.Errorf("jj config list: %v", err)
	}
	return result, nil
}

// SetRepositorySetting sets a configuration setting on the repository.
func (jj *Jujutsu) SetRepositorySetting(ctx context.Context, key, value string) error {
	cmd := jj.command(ctx, "config", "set", "--ignore-working-copy", "--repo", "--", key, value)
	return runCommand(fmt.Sprintf("jj config set --repo %s %s", key, value), cmd)
}

type Bookmark struct {
	Name        string          `json:"name"`
	Remote      string          `json:"remote"`
	TargetMerge Merge[CommitID] `json:"target"`
}

// RefSymbol returns the ref symbol that represents the bookmark.
func (b *Bookmark) RefSymbol() RefSymbol {
	return RefSymbol{Name: b.Name, Remote: b.Remote}
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
	Parents     []CommitID `json:"parents"`
	ChangeID    ChangeID   `json:"change_id"`
	Author      Signature  `json:"author"`
	Committer   Signature  `json:"committer"`
	Description string     `json:"description"`
}

// Signature represents a person/entity
// and a timestamp for when they authored or committed a [Commit].
//
// https://docs.jj-vcs.dev/latest/templates/#signature-type
type Signature struct {
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Timestamp time.Time `json:"timestamp,format:RFC3339"`
}

// String formats the signature in Git commit format.
func (sig *Signature) String() string {
	sb := new(strings.Builder)
	sb.WriteString(sig.Name)
	if sig.Email != "" {
		sb.WriteString(" <")
		sb.WriteString(sig.Email)
		sb.WriteString(">")
	}
	sb.WriteString(" ")
	sb.WriteString(strconv.FormatInt(sig.Timestamp.Unix(), 10))
	sb.WriteString(" ")
	sb.WriteString(sig.Timestamp.Format("-0700"))
	return sb.String()
}

// LogOptions is the set of optional parameters to [*Jujutsu.Log].
type LogOptions struct {
	// Revset is the set of revisions to yield.
	// If empty, all() is used.
	Revset string
	// If Reversed is true, then the commits are yielded in order of older revisions first.
	Reversed bool
}

// Log calls yield for every revision in opts.Revset.
func (jj *Jujutsu) Log(ctx context.Context, opts LogOptions, yield func(*Commit) bool) error {
	args := []string{
		"log",
		"--ignore-working-copy",
		"--no-graph",
		"--template=json(self)",
		"--revisions=" + cmp.Or(opts.Revset, "all()"),
	}
	if opts.Reversed {
		args = append(args, "--reversed")
	}
	cmd := jj.command(ctx, args...)
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

func (jj *Jujutsu) ShowFile(ctx context.Context, revset string, path string) (io.ReadCloser, error) {
	errPrefix := fmt.Sprintf("jj file show -r %s %s", revset, path)
	cmd := jj.command(ctx, "file", "show", "--revision="+revset, "--", path)
	stderr := new(bytes.Buffer)
	cmd.Stderr = stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("%s: %v", errPrefix, err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%s: %v", errPrefix, err)
	}

	// If jj reports an error, stdout will be empty and stderr will contain the error message.
	first := make([]byte, 2048)
	readLen, readErr := io.ReadAtLeast(stdout, first, 1)
	if readErr != nil {
		// Empty stdout, check for error.
		var err error
		err = errors.Join(err, stdout.Close())
		err = errors.Join(err, cmd.Wait())
		if err != nil {
			return nil, cmderror.New(errPrefix, err, stderr.Bytes())
		}
		if !errors.Is(readErr, io.EOF) {
			return nil, cmderror.New(errPrefix, readErr, stderr.Bytes())
		}
		return io.NopCloser(bytes.NewReader(nil)), nil
	}
	return &fileReader{
		errPrefix: errPrefix,
		first:     first[:readLen],
		pipe:      stdout,
		wait:      cmd.Wait,
		stderr:    stderr,
	}, nil
}

type fileReader struct {
	errPrefix string
	first     []byte
	pipe      io.ReadCloser
	wait      func() error
	stderr    *bytes.Buffer // can't be read until wait returns
}

func (fr *fileReader) Read(p []byte) (int, error) {
	if len(fr.first) > 0 {
		n := copy(p, fr.first)
		fr.first = fr.first[n:]
		return n, nil
	}
	return fr.pipe.Read(p)
}

func (fr *fileReader) WriteTo(w io.Writer) (int64, error) {
	var n int64
	if len(fr.first) > 0 {
		nn, err := w.Write(fr.first)
		fr.first = fr.first[nn:]
		n += int64(nn)
		if err != nil {
			return n, err
		}
	}
	nn, err := io.Copy(w, fr.pipe)
	n += nn
	return n, err
}

func (fr *fileReader) Close() error {
	closeErr := fr.pipe.Close()
	waitErr := fr.wait()
	// Wait errors are usually more interesting than close errors.
	if err := cmp.Or(waitErr, closeErr); err != nil {
		return cmderror.New("close "+fr.errPrefix, waitErr, fr.stderr.Bytes())
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

// GitRoot returns the path to the underlying Git directory
// of a repository using the Git backend.
func (jj *Jujutsu) GitRoot(ctx context.Context) (string, error) {
	out, err := jj.command(ctx, "git", "root", "--ignore-working-copy").Output()
	if err != nil {
		return "", fmt.Errorf("jj git root: %v", err)
	}
	return string(bytes.TrimSuffix(out, []byte("\n"))), nil
}

func runCommand(errPrefix string, cmd *exec.Cmd) error {
	stderr := new(bytes.Buffer)
	cmd.Stderr = stderr

	if err := cmd.Run(); err != nil {
		return cmderror.New(errPrefix, err, stderr.Bytes())
	}

	return nil
}
