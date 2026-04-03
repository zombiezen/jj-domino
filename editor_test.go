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

	"github.com/google/go-cmp/cmp"
	"zombiezen.com/go/jj-domino/internal/jujutsu"
)

func TestEditPullRequestMessages(t *testing.T) {
	tests := []struct {
		name          string
		pullRequests  []*pullRequest
		editedContent string

		want               []*pullRequest
		wantInitialContent string
		err                bool
	}{
		{
			name: "SinglePullRequest",
			pullRequests: []*pullRequest{
				{
					HeadRefName: "zombiezen/foo",
					Title:       "Hello World",
					Body:        "This changes things.",
				},
			},
			wantInitialContent: "" +
				"Hello World\n\n" +
				"This changes things.\n\n" +
				editorPostscript,
			editedContent: "" +
				"Hello Universe\n\n" +
				"This changes *EVERYTHING*.\n" +
				editorPostscript,
			want: []*pullRequest{
				{
					HeadRefName: "zombiezen/foo",
					Title:       "Hello Universe",
					Body:        "This changes *EVERYTHING*.",
				},
			},
		},
		{
			name: "CommentsInBody",
			pullRequests: []*pullRequest{
				{
					HeadRefName: "zombiezen/foo",
					Title:       "Hello World",
					Body:        "This changes things.",
				},
			},
			wantInitialContent: "" +
				"Hello World\n\n" +
				"This changes things.\n\n" +
				editorPostscript,
			editedContent: "" +
				"Hello Universe\n\n" +
				"This changes *EVERYTHING*.\n" +
				editorCommentPrefix + " Some choice commentary here.\n" +
				"I can't believe it.\n" +
				editorPostscript,
			want: []*pullRequest{
				{
					HeadRefName: "zombiezen/foo",
					Title:       "Hello Universe",
					Body:        "This changes *EVERYTHING*.\nI can't believe it.",
				},
			},
		},
		{
			name: "TrailingNewlinesInBody",
			pullRequests: []*pullRequest{
				{
					HeadRefName: "zombiezen/foo",
					Title:       "Hello World",
					Body:        "This changes things.\n\n\n\n\n",
				},
			},
			wantInitialContent: "" +
				"Hello World\n\n" +
				"This changes things.\n\n" +
				editorPostscript,
			editedContent: "" +
				"Hello Universe\n\n" +
				"This changes *EVERYTHING*.\n" +
				editorPostscript,
			want: []*pullRequest{
				{
					HeadRefName: "zombiezen/foo",
					Title:       "Hello Universe",
					Body:        "This changes *EVERYTHING*.",
				},
			},
		},
		{
			name: "MissingMessage",
			pullRequests: []*pullRequest{
				{
					HeadRefName: "zombiezen/foo",
					Title:       "Hello World",
					Body:        "This changes things.",
				},
			},
			wantInitialContent: "" +
				"Hello World\n\n" +
				"This changes things.\n\n" +
				editorPostscript,
			editedContent: "" +
				"\n \t \n\n" +
				editorPostscript,
			err: true,
		},
		{
			name: "RemoveBody",
			pullRequests: []*pullRequest{
				{
					HeadRefName: "zombiezen/foo",
					Title:       "Hello World",
					Body:        "This changes things.",
				},
			},
			wantInitialContent: "" +
				"Hello World\n\n" +
				"This changes things.\n\n" +
				editorPostscript,
			editedContent: "" +
				"Hello Nothing\n\n" +
				editorPostscript,
			want: []*pullRequest{
				{
					HeadRefName: "zombiezen/foo",
					Title:       "Hello Nothing",
					Body:        "",
				},
			},
		},
		{
			name: "AddBody",
			pullRequests: []*pullRequest{
				{
					HeadRefName: "zombiezen/foo",
					Title:       "Hello World",
				},
			},
			wantInitialContent: "" +
				"Hello World\n\n" +
				editorPostscript,
			editedContent: "" +
				"Hello new body\n\n" +
				"wo\n" +
				editorPostscript,
			want: []*pullRequest{
				{
					HeadRefName: "zombiezen/foo",
					Title:       "Hello new body",
					Body:        "wo",
				},
			},
		},
		{
			name: "MultiplePullRequests",
			pullRequests: []*pullRequest{
				{
					HeadRefName: "zombiezen/foo",
					Title:       "Hello World",
					Body:        "This changes things.",
				},
				{
					HeadRefName: "zombiezen/bar",
					Title:       "Endgame",
					Body:        "Checkmate!",
				},
			},
			wantInitialContent: editorPreamble +
				editorSeparatorPrefix + "zombiezen/foo" + editorSeparatorSuffix +
				"Hello World\n\n" +
				"This changes things.\n\n" +
				editorSeparatorPrefix + "zombiezen/bar" + editorSeparatorSuffix +
				"Endgame\n\n" +
				"Checkmate!\n\n" +
				editorPostscript,
			editedContent: editorPreamble +
				editorSeparatorPrefix + "zombiezen/foo" + editorSeparatorSuffix +
				"Hello Universe\n\n" +
				"This changes *EVERYTHING*.\n" +
				editorSeparatorPrefix + "zombiezen/bar" + editorSeparatorSuffix +
				"Close to Endgame\n\n" +
				"Check\n\n" +
				editorPostscript,
			want: []*pullRequest{
				{
					HeadRefName: "zombiezen/foo",
					Title:       "Hello Universe",
					Body:        "This changes *EVERYTHING*.",
				},
				{
					HeadRefName: "zombiezen/bar",
					Title:       "Close to Endgame",
					Body:        "Check",
				},
			},
		},
		{
			name: "MultiplePullRequestsReordered",
			pullRequests: []*pullRequest{
				{
					HeadRefName: "zombiezen/foo",
					Title:       "Hello World",
					Body:        "This changes things.",
				},
				{
					HeadRefName: "zombiezen/bar",
					Title:       "Endgame",
					Body:        "Checkmate!",
				},
			},
			wantInitialContent: editorPreamble +
				editorSeparatorPrefix + "zombiezen/foo" + editorSeparatorSuffix +
				"Hello World\n\n" +
				"This changes things.\n\n" +
				editorSeparatorPrefix + "zombiezen/bar" + editorSeparatorSuffix +
				"Endgame\n\n" +
				"Checkmate!\n\n" +
				editorPostscript,
			editedContent: editorPreamble +
				editorSeparatorPrefix + "zombiezen/bar" + editorSeparatorSuffix +
				"Close to Endgame\n\n" +
				"Check\n\n" +
				editorSeparatorPrefix + "zombiezen/foo" + editorSeparatorSuffix +
				"Hello Universe\n\n" +
				"This changes *EVERYTHING*.\n" +
				editorPostscript,
			want: []*pullRequest{
				{
					HeadRefName: "zombiezen/foo",
					Title:       "Hello Universe",
					Body:        "This changes *EVERYTHING*.",
				},
				{
					HeadRefName: "zombiezen/bar",
					Title:       "Close to Endgame",
					Body:        "Check",
				},
			},
		},
		{
			name: "DuplicateMarkers",
			pullRequests: []*pullRequest{
				{
					HeadRefName: "zombiezen/foo",
					Title:       "Hello World",
					Body:        "This changes things.",
				},
				{
					HeadRefName: "zombiezen/bar",
					Title:       "Endgame",
					Body:        "Checkmate!",
				},
			},
			wantInitialContent: editorPreamble +
				editorSeparatorPrefix + "zombiezen/foo" + editorSeparatorSuffix +
				"Hello World\n\n" +
				"This changes things.\n\n" +
				editorSeparatorPrefix + "zombiezen/bar" + editorSeparatorSuffix +
				"Endgame\n\n" +
				"Checkmate!\n\n" +
				editorPostscript,
			editedContent: editorPreamble +
				editorSeparatorPrefix + "zombiezen/foo" + editorSeparatorSuffix +
				"Hello Universe\n\n" +
				"This changes *EVERYTHING*.\n" +
				editorSeparatorPrefix + "zombiezen/bar" + editorSeparatorSuffix +
				"Close to Endgame\n\n" +
				"Check\n\n" +
				editorSeparatorPrefix + "zombiezen/bar" + editorSeparatorSuffix +
				"Deja vu\n\n" +
				editorPostscript,
			err: true,
		},
		{
			name: "ExtraMarker",
			pullRequests: []*pullRequest{
				{
					HeadRefName: "zombiezen/foo",
					Title:       "Hello World",
					Body:        "This changes things.",
				},
				{
					HeadRefName: "zombiezen/bar",
					Title:       "Endgame",
					Body:        "Checkmate!",
				},
			},
			wantInitialContent: editorPreamble +
				editorSeparatorPrefix + "zombiezen/foo" + editorSeparatorSuffix +
				"Hello World\n\n" +
				"This changes things.\n\n" +
				editorSeparatorPrefix + "zombiezen/bar" + editorSeparatorSuffix +
				"Endgame\n\n" +
				"Checkmate!\n\n" +
				editorPostscript,
			editedContent: editorPreamble +
				editorSeparatorPrefix + "zombiezen/foo" + editorSeparatorSuffix +
				"Hello Universe\n\n" +
				"This changes *EVERYTHING*.\n" +
				editorSeparatorPrefix + "zombiezen/bar" + editorSeparatorSuffix +
				"Close to Endgame\n\n" +
				"Check\n\n" +
				editorSeparatorPrefix + "zombiezen/baz" + editorSeparatorSuffix +
				"zombiezen, what is this?\n\n" +
				editorPostscript,
			err: true,
		},
		{
			name: "MissingMarker",
			pullRequests: []*pullRequest{
				{
					HeadRefName: "zombiezen/foo",
					Title:       "Hello World",
					Body:        "This changes things.",
				},
				{
					HeadRefName: "zombiezen/bar",
					Title:       "Endgame",
					Body:        "Checkmate!",
				},
			},
			wantInitialContent: editorPreamble +
				editorSeparatorPrefix + "zombiezen/foo" + editorSeparatorSuffix +
				"Hello World\n\n" +
				"This changes things.\n\n" +
				editorSeparatorPrefix + "zombiezen/bar" + editorSeparatorSuffix +
				"Endgame\n\n" +
				"Checkmate!\n\n" +
				editorPostscript,
			editedContent: editorPreamble +
				editorSeparatorPrefix + "zombiezen/bar" + editorSeparatorSuffix +
				"Close to Endgame\n\n" +
				"Check\n\n" +
				editorPostscript,
			err: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			prs := make([]*pullRequest, len(test.pullRequests))
			for i, pr := range test.pullRequests {
				prs[i] = new(*pr)
			}
			err := editPullRequestMessages(prs, func(initialContent []byte) ([]byte, error) {
				if diff := cmp.Diff(test.wantInitialContent, string(initialContent)); diff != "" {
					t.Errorf("initial editor content (-want +got):\n%s", diff)
				}
				return []byte(test.editedContent), nil
			})
			if err != nil {
				t.Log("editPullRequestMessages:", err)
				if !test.err {
					t.Fail()
				}
			}
			if test.err {
				if err == nil {
					t.Error("editPullRequestMessages did not return an error")
				}
				return
			}
			if diff := cmp.Diff(test.want, prs, cmp.AllowUnexported(pullRequest{})); diff != "" {
				t.Errorf("pull requests (-want +got):\n%s", diff)
			}
		})
	}
}

func TestEditor(t *testing.T) {
	ctx := testContext(t)

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
	ctx := testContext(t)

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
