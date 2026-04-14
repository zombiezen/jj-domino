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

package fakegithub

import (
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/shurcooL/githubv4"
	"zombiezen.com/go/jj-domino/internal/github"
)

func TestAuthenticatedUser(t *testing.T) {
	ctx := t.Context()
	srv, client := newTestServer(t)
	const want = "octocat"
	srv.SetViewer(want)
	got, err := github.AuthenticatedUser(ctx, client)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("viewer.login = %q; want %q", got, want)
	}
}

func TestFetchRepository(t *testing.T) {
	t.Run("Found", func(t *testing.T) {
		ctx := t.Context()
		srv, client := newTestServer(t)
		const owner = "octocat"
		const repoName = "hello-world"
		srv.SetViewer(owner)
		path := github.RepositoryPath{
			Owner: owner,
			Repo:  repoName,
		}
		err := srv.CreateRepository(path)
		if err != nil {
			t.Fatal(err)
		}

		got, err := github.FetchRepository(ctx, client, path)
		if err != nil {
			t.Fatal(err)
		}
		want := &github.Repository{
			Owner: &github.RepositoryOwner{
				Login: owner,
			},
			Name: repoName,
		}
		ignoreIDOption := cmp.FilterPath(func(p cmp.Path) bool {
			return p.Index(-2).Type() == reflect.TypeFor[github.Repository]() &&
				p.Last().(cmp.StructField).Name() == "ID"
		}, cmp.Ignore())
		if diff := cmp.Diff(want, got, ignoreIDOption); diff != "" {
			t.Errorf("repository (-want +got):\n%s", diff)
		}
		if got.ID == nil {
			t.Error("repository.ID is nil")
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		ctx := t.Context()
		srv, client := newTestServer(t)
		const owner = "octocat"
		srv.SetViewer(owner)
		path := github.RepositoryPath{
			Owner: owner,
			Repo:  "hello-world",
		}

		_, err := github.FetchRepository(ctx, client, path)
		if err == nil {
			t.Error("github.FetchRepository did not return an error")
		} else {
			t.Log("github.FetchRepository:", err)
		}
	})
}

func TestCreatePullRequest(t *testing.T) {
	ctx := t.Context()
	srv, client := newTestServer(t)
	const owner = "octocat"
	const repoName = "hello-world"
	srv.SetViewer(owner)
	path := github.RepositoryPath{
		Owner: owner,
		Repo:  repoName,
	}
	err := srv.CreateRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := github.FetchRepository(ctx, client, path)
	if err != nil {
		t.Fatal(err)
	}
	if repo.ID == nil {
		t.Fatal("repository missing an ID")
	}

	pr := &github.PullRequest{
		Title:          "Fix ALL the things!",
		Body:           "This is great code. Ship it.",
		BaseRefName:    "main",
		HeadRefName:    "foo",
		HeadRepository: repo,
	}
	if err := github.CreatePullRequest(ctx, client, repo, pr); err != nil {
		t.Error(err)
	}
	if pr.ID == nil {
		t.Error("pr.ID not set")
	}
	if pr.Number == 0 {
		t.Error("pr.Number not set")
	}
	if pr.URL.URL == nil {
		t.Error("pr.URL not set")
	}
}

func newTestServer(tb testing.TB) (*Server, *githubv4.Client) {
	srv := new(Server)
	httpServer := httptest.NewServer(srv)
	tb.Cleanup(httpServer.Close)
	client := githubv4.NewEnterpriseClient(httpServer.URL+"/graphql", httpServer.Client())
	return srv, client
}
