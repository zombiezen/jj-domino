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
	"errors"
	"fmt"
	"log"
	"slices"

	"github.com/alecthomas/kong"
	"zombiezen.com/go/jj-domino/internal/jujutsu"
)

type cli struct {
	Submit submitCmd `cmd:"" help:"Submit a review stack"`
	Doctor doctorCmd `cmd:"" help:"Verify auth and config settings"`
}

type submitCmd struct {
	Draft        bool    `short:"d" help:"Submit PR as draft"`
	TemplatePath *string `short:"t" help:"Template path"`
	Bookmark     string  `short:"b" help:"Bookmark to send"`
	Root         *string `short:"R" help:"Optional repository root (defaults to \"jj root\")"`
}

type doctorCmd struct{}

func (c *submitCmd) Run(ctx context.Context) error {
	opts := jujutsu.Options{}
	if c.Root != nil {
		opts.Dir = *c.Root
	}
	jj, err := jujutsu.New(opts)
	if err != nil {
		return err
	}
	root, err := jj.WorkspaceRoot(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("root: %#v\n", root)

	stack, err := stackForBookmark(ctx, jj, c.Bookmark)
	if err != nil {
		return err
	}
	for _, pr := range stack {
		fmt.Printf("%s\t%s\n", pr.commit.ChangeID.Short(), pr.bookmarkName)
	}

	return nil
}

type intendedPR struct {
	bookmarkName string
	commit       *jujutsu.Commit
}

func stackForBookmark(ctx context.Context, jj *jujutsu.Jujutsu, bookmark string) ([]intendedPR, error) {
	type stackFrame struct {
		curr  *jujutsu.Commit
		trail []intendedPR
	}

	bookmarks, err := jj.ListBookmarks(ctx)
	if err != nil {
		return nil, fmt.Errorf("compute stack for %q: %v", bookmark, err)
	}
	i := slices.IndexFunc(bookmarks, func(b *jujutsu.Bookmark) bool {
		return b.Name == bookmark && b.Remote == ""
	})
	if i < 0 {
		return nil, fmt.Errorf("compute stack for %q: no such bookmark", bookmark)
	}
	b := bookmarks[i]
	headCommitID, ok := b.TargetMerge.Resolved()
	if !ok {
		return nil, fmt.Errorf("compute stack for %q: unresolved bookmark", bookmark)
	}

	revset := "trunk().." + headCommitID.String()
	changes := make(map[string]*jujutsu.Commit)
	err = jj.Log(ctx, revset, func(c *jujutsu.Commit) bool {
		changes[string(c.ID)] = c
		return true
	})
	if err != nil {
		return nil, fmt.Errorf("compute stack for %q: %v", bookmark, err)
	}
	headCommit := changes[string(headCommitID)]
	if headCommit == nil {
		return nil, fmt.Errorf("compute stack for %q: commit %v is ancestor of trunk()", bookmark, headCommitID)
	}

	stack := []intendedPR{{
		bookmarkName: bookmark,
		commit:       headCommit,
	}}
	visited := make(map[string]struct{})
	var resultError error
	for curr := headCommit.Parents; len(curr) > 0; {
		var next []jujutsu.CommitID
		for _, id := range curr {
			if _, done := visited[string(id)]; done {
				continue
			}
			visited[string(id)] = struct{}{}

			c := changes[string(id)]
			if c == nil {
				// Trunk or trunk ancestor.
				continue
			}
			var names []string
			for _, b := range bookmarks {
				if target, ok := b.TargetMerge.Resolved(); b.Remote == "" && ok && target.Equal(id) {
					names = append(names, b.Name)
				}
			}
			switch {
			case len(names) == 1:
				stack = append(stack, intendedPR{
					bookmarkName: names[0],
					commit:       c,
				})
			case len(names) > 1:
				resultError = errors.Join(resultError, fmt.Errorf("commit %v has multiple bookmarks", id))
			}
			next = append(next, c.Parents...)
		}
		curr = next
	}

	slices.Reverse(stack)
	if resultError != nil {
		resultError = fmt.Errorf("compute stack for %q: %w", bookmark, resultError)
	}
	return stack, resultError
}

func (c *doctorCmd) Run(ctx context.Context) error {
	client, err := getClient()
	if err != nil {
		return err
	}
	user, _, err := client.Users.Get(ctx, "")
	if err != nil {
		return err
	}
	fmt.Printf("Authenticated as: %s\n", user.GetLogin())
	return nil
}

func main() {
	ctx := kong.Parse(&cli{}, kong.UsageOnError())
	ctx.BindTo(context.Background(), (*context.Context)(nil))
	if err := ctx.Run(); err != nil {
		log.Fatal(err)
	}
}
