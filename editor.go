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
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	jsonv2 "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
	"github.com/shurcooL/githubv4"
	"zombiezen.com/go/jj-domino/internal/jujutsu"
	"zombiezen.com/go/jj-domino/internal/sigterm"
)

const (
	editorCommentPrefix   = "JJ:"
	editorSeparatorPrefix = editorCommentPrefix + " domino "
	editorSeparatorSuffix = " -------\n"

	editorPreamble = "" +
		editorCommentPrefix + " Enter or edit pull request descriptions after the `" + editorCommentPrefix + " domino` lines.\n" +
		editorCommentPrefix + " Warning:\n" +
		editorCommentPrefix + " - The text you enter will be lost on a syntax error.\n" +
		editorCommentPrefix + " - The syntax of the separator lines may change in the future.\n"
	editorPostscript = "" +
		editorCommentPrefix + " Lines starting with `" + editorCommentPrefix + "` (like this one) will be removed.\n"
)

func editPullRequestMessages(prs []*pullRequest, edit func(initialContent []byte) ([]byte, error)) (err error) {
	if len(prs) == 0 {
		return errors.New("edit pull request messages: no pull requests to edit")
	}
	var initialContent []byte
	if len(prs) > 1 {
		initialContent = []byte(editorPreamble)
	}
	for _, pr := range prs {
		if len(prs) > 1 {
			initialContent = append(initialContent, editorSeparatorPrefix...)
			initialContent = append(initialContent, pr.HeadRefName...)
			initialContent = append(initialContent, editorSeparatorSuffix...)
		}
		initialContent = append(initialContent, pr.Title...)
		if pr.Body != "" {
			initialContent = append(initialContent, "\n\n"...)
			initialContent = append(initialContent, strings.TrimRight(string(pr.Body), "\n")...)
		}
		initialContent = append(initialContent, "\n\n"...)
	}
	initialContent = append(initialContent, editorPostscript...)
	newContent, err := edit(initialContent)
	if err != nil {
		return fmt.Errorf("edit pull request messages: %v", err)
	}
	if bytes.Equal(initialContent, newContent) {
		// Optimization: File untouched. Use as-is.
		return nil
	}

	if len(prs) == 1 {
		err = parseSinglePREditor(prs[0], newContent)
	} else {
		err = parseMultiPREditor(prs, newContent)
	}
	if err != nil {
		return fmt.Errorf("edit pull request messages: %w", err)
	}
	return nil
}

func parseSinglePREditor(pr *pullRequest, editorContent []byte) (err error) {
	defer func() {
		if err != nil {
			err = &editorParseError{
				err:     err,
				content: editorContent,
			}
		}
	}()

	nextLine, stopLines := iter.Pull(bytes.SplitSeq(editorContent, []byte("\n")))
	defer stopLines()

	// Read title.
	for {
		line, ok := nextLine()
		if !ok {
			return errors.New("missing title")
		}
		if bytes.HasPrefix(line, []byte(editorCommentPrefix)) {
			continue
		}
		line = bytes.TrimSpace(line)
		if len(line) > 0 {
			pr.Title = githubv4.String(line)
			break
		}
	}

	// Read blank line.
	for {
		line, ok := nextLine()
		if !ok {
			pr.Body = ""
			break
		}
		if !bytes.HasPrefix(line, []byte(editorCommentPrefix)) {
			if len(bytes.TrimSpace(line)) > 0 {
				return fmt.Errorf("missing blank line after %s", pr.Title)
			}
			break
		}
	}

	// Read body.
	body := new(strings.Builder)
	for {
		line, ok := nextLine()
		if !ok {
			break
		}
		if bytes.HasPrefix(line, []byte(editorCommentPrefix)) {
			continue
		}
		body.Write(line)
		body.WriteByte('\n')
	}
	pr.Body = githubv4.String(strings.Trim(body.String(), "\r\n"))

	return nil
}

func parseMultiPREditor(prs []*pullRequest, editorContent []byte) (err error) {
	defer func() {
		if err != nil {
			err = &editorParseError{
				err:     err,
				content: editorContent,
			}
		}
	}()

	nextLine, stopLines := iter.Pull(bytes.SplitSeq(editorContent, []byte("\n")))
	defer stopLines()

	// Read to first marker.
	var headRefName string
	for {
		line, ok := nextLine()
		if !ok {
			return errors.New("no markers found")
		}
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if bytes.HasPrefix(line, []byte(editorSeparatorPrefix)) {
			var err error
			headRefName, err = parseEditorSeparator(line)
			if err != nil {
				return err
			}
			break
		}
		if !bytes.HasPrefix(line, []byte(editorCommentPrefix)) {
			return errors.New("extra text before first marker")
		}
	}
	// Read pull request content.
	found := make([]bool, len(prs))
	for headRefName != "" {
		i := slices.IndexFunc(prs, func(pr *pullRequest) bool {
			return pr.HeadRefName == githubv4.String(headRefName)
		})
		if i == -1 {
			return fmt.Errorf("unknown bookmark name %s", headRefName)
		}
		if found[i] {
			return fmt.Errorf("duplicate marker for %s", headRefName)
		}
		currPR := prs[i]
		found[i] = true

		// Read title.
		for {
			line, ok := nextLine()
			if !ok {
				return fmt.Errorf("missing title for %s", headRefName)
			}
			if bytes.HasPrefix(line, []byte(editorCommentPrefix)) {
				continue
			}
			line = bytes.TrimSpace(line)
			if len(line) > 0 {
				currPR.Title = githubv4.String(line)
				break
			}
		}

		// Read blank line.
		for {
			line, ok := nextLine()
			if !ok {
				currPR.Body = ""
				break
			}
			if !bytes.HasPrefix(line, []byte(editorCommentPrefix)) {
				if len(bytes.TrimSpace(line)) > 0 {
					return fmt.Errorf("missing blank line after %s", currPR.Title)
				}
				break
			}
		}

		// Read body.
		body := new(strings.Builder)
		for {
			line, ok := nextLine()
			if !ok {
				headRefName = ""
				break
			}
			if bytes.HasPrefix(line, []byte(editorSeparatorPrefix)) {
				var err error
				headRefName, err = parseEditorSeparator(line)
				if err != nil {
					return err
				}
				break
			}
			if bytes.HasPrefix(line, []byte(editorCommentPrefix)) {
				continue
			}
			body.Write(line)
			body.WriteByte('\n')
		}
		currPR.Body = githubv4.String(strings.Trim(body.String(), "\r\n"))
	}

	for i := range found {
		if !found[i] {
			return fmt.Errorf("missing description for %s", prs[i].HeadRefName)
		}
	}

	return nil
}

func parseEditorSeparator(line []byte) (string, error) {
	line = bytes.TrimSuffix(line, []byte("\n"))
	origLine := line
	line, ok := bytes.CutPrefix(line, []byte(editorSeparatorPrefix))
	if !ok {
		return "", fmt.Errorf("invalid separator line %q: does not start with %q", origLine, editorSeparatorPrefix)
	}
	line = bytes.TrimSuffix(line, []byte(editorSeparatorSuffix[len(" "):len(editorSeparatorSuffix)-len("\n")]))
	ref := string(bytes.TrimSpace(line))
	if ref == "" {
		return "", fmt.Errorf("invalid separator line %q: missing ref name", origLine)
	}
	return ref, nil
}

type editorParseError struct {
	err     error
	content []byte
}

func (e *editorParseError) Error() string {
	return e.err.Error()
}

func (e *editorParseError) Unwrap() error {
	return e.err
}

// failedEditorContent returns the content from an error returned by [editPullRequestMessages].
func failedEditorContent(err error) (content []byte, ok bool) {
	e, ok := errors.AsType[*editorParseError](err)
	if !ok {
		return nil, false
	}
	return e.content, true
}

type editor struct {
	command  *jujutsu.CommandNameAndArgs
	workDir  string
	tempRoot string
	stdin    io.Reader
	stdout   io.Writer
	stderr   io.Writer

	logError func(context.Context, error)
}

func jujutsuEditor(settings map[string]jsontext.Value) (*jujutsu.CommandNameAndArgs, error) {
	v := settings["ui.editor"]
	if len(v) == 0 || v.Kind() == jsontext.KindNull {
		return nil, fmt.Errorf("jj config get ui.editor: not set")
	}
	c := new(jujutsu.CommandNameAndArgs)
	if err := jsonv2.Unmarshal(v, c); err != nil {
		return nil, fmt.Errorf("jj config get ui.editor: %v", err)
	}
	return c, nil
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
