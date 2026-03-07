package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/shurcooL/githubv4"
	"zombiezen.com/go/jj-domino/internal/jujutsu"
)

func TestStackForBookmark(t *testing.T) {
	ctx := t.Context()
	jjExe := findJJExecutable(t)

	t.Run("SinglePR", func(t *testing.T) {
		repoDir := t.TempDir()
		jj, err := jujutsu.New(jujutsu.Options{
			Dir:   repoDir,
			JJExe: jjExe,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := createRepository(ctx, jj); err != nil {
			t.Fatal(err)
		}
		commits, err := newChanges(ctx, jj, []changeDescription{
			{bookmark: "foo"},
		})
		if err != nil {
			t.Fatal(err)
		}
		bookmarks, err := jj.ListBookmarks(ctx)
		if err != nil {
			t.Fatal(err)
		}

		got, err := stackForBookmark(ctx, jj, bookmarks, "foo")
		if err != nil {
			t.Fatal(err)
		}
		want := []localCommitRef{
			{name: "foo", commit: commits["foo"]},
		}
		if diff := cmp.Diff(want, got, cmp.AllowUnexported(localCommitRef{})); diff != "" {
			t.Errorf("stack (-want +got):\n%s", diff)
		}
	})

	t.Run("LinearChain", func(t *testing.T) {
		repoDir := t.TempDir()
		jj, err := jujutsu.New(jujutsu.Options{
			Dir:   repoDir,
			JJExe: jjExe,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := createRepository(ctx, jj); err != nil {
			t.Fatal(err)
		}
		commits, err := newChanges(ctx, jj, []changeDescription{
			{bookmark: "foo"},
			{bookmark: "bar"},
		})
		if err != nil {
			t.Fatal(err)
		}
		bookmarks, err := jj.ListBookmarks(ctx)
		if err != nil {
			t.Fatal(err)
		}

		got, err := stackForBookmark(ctx, jj, bookmarks, "bar")
		if err != nil {
			t.Fatal(err)
		}
		want := []localCommitRef{
			{name: "foo", commit: commits["foo"]},
			{name: "bar", commit: commits["bar"]},
		}
		if diff := cmp.Diff(want, got, cmp.AllowUnexported(localCommitRef{})); diff != "" {
			t.Errorf("stack (-want +got):\n%s", diff)
		}
	})

	t.Run("ValidMerge", func(t *testing.T) {
		repoDir := t.TempDir()
		jj, err := jujutsu.New(jujutsu.Options{
			Dir:   repoDir,
			JJExe: jjExe,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := createRepository(ctx, jj); err != nil {
			t.Fatal(err)
		}
		commits, err := newChanges(ctx, jj, []changeDescription{
			{bookmark: "d"},
			{bookmark: "b", parents: []string{"d"}},
			{bookmark: "c", parents: []string{"d"}},
			{bookmark: "a", parents: []string{"b", "c"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := jj.DeleteBookmarks(ctx, []string{"b", "c", "d"}); err != nil {
			t.Fatal(err)
		}
		bookmarks, err := jj.ListBookmarks(ctx)
		if err != nil {
			t.Fatal(err)
		}

		got, err := stackForBookmark(ctx, jj, bookmarks, "a")
		if err != nil {
			t.Fatal(err)
		}
		want := []localCommitRef{
			{name: "a", commit: commits["a"]},
		}
		if diff := cmp.Diff(want, got, cmp.AllowUnexported(localCommitRef{})); diff != "" {
			t.Errorf("stack (-want +got):\n%s", diff)
		}
	})

	t.Run("InvalidMerge", func(t *testing.T) {
		t.Skip("TODO(#1)")

		repoDir := t.TempDir()
		jj, err := jujutsu.New(jujutsu.Options{
			Dir:   repoDir,
			JJExe: jjExe,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := createRepository(ctx, jj); err != nil {
			t.Fatal(err)
		}
		_, err = newChanges(ctx, jj, []changeDescription{
			{bookmark: "d"},
			{bookmark: "b", parents: []string{"d"}},
			{bookmark: "c", parents: []string{"d"}},
			{bookmark: "a", parents: []string{"b", "c"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := jj.DeleteBookmarks(ctx, []string{"c", "d"}); err != nil {
			t.Fatal(err)
		}
		bookmarks, err := jj.ListBookmarks(ctx)
		if err != nil {
			t.Fatal(err)
		}

		got, err := stackForBookmark(ctx, jj, bookmarks, "a")
		if err == nil {
			names := make([]string, 0, len(got))
			for _, pr := range got {
				names = append(names, pr.name)
			}
			t.Error("stackForBookmark did not return an error. Stack: main ←", strings.Join(names, " ← "))
		}
	})
}

func createRepository(ctx context.Context, jj *jujutsu.Jujutsu) error {
	if err := jj.GitInit(ctx, jujutsu.GitInitOptions{}); err != nil {
		return err
	}
	if err := jj.SetBookmark(ctx, []string{"main"}, jujutsu.SetBookmarkOptions{}); err != nil {
		return err
	}
	if err := jj.SetRepositorySetting(ctx, `revset-aliases."trunk()"`, "main"); err != nil {
		return err
	}
	return nil
}

type changeDescription struct {
	bookmark string
	parents  []string
}

func newChanges(ctx context.Context, jj *jujutsu.Jujutsu, changes []changeDescription) (map[string]*jujutsu.Commit, error) {
	m := make(map[string]*jujutsu.Commit)
	for _, desc := range changes {
		if err := jj.New(ctx, desc.parents...); err != nil {
			return m, fmt.Errorf("create %s: %v", desc.bookmark, err)
		}
		if err := jj.SetBookmark(ctx, []string{desc.bookmark}, jujutsu.SetBookmarkOptions{}); err != nil {
			return m, err
		}
		err := jj.Log(ctx, "@", func(c *jujutsu.Commit) bool {
			m[desc.bookmark] = c
			return false
		})
		if err != nil {
			return nil, fmt.Errorf("create %s: %v", desc.bookmark, err)
		}
	}
	return m, nil
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

func TestFormatPRNumber(t *testing.T) {
	tests := []struct {
		n     githubv4.Int
		width int
		want  string
	}{
		{1, 1, "#1"},
		{2, 1, "#2"},
		{1, 2, " #1"},
		{123, 3, "#123"},
		{123, 1, "#123"},
		{123, 5, "  #123"},
	}

	for _, test := range tests {
		if got := formatPRNumber(test.n, test.width); got != test.want {
			t.Errorf("formatPRNumber(%d, %d) = %q; want %q", test.n, test.width, got, test.want)
		}
	}
}

func TestPRNumberPlaceholder(t *testing.T) {
	tests := []struct {
		width int
		want  string
	}{
		{1, "#X"},
		{3, "#XXX"},
	}

	for _, test := range tests {
		if got := prNumberPlaceholder(test.width); got != test.want {
			t.Errorf("prNumberPlaceholder(%d) = %q; want %q", test.width, got, test.want)
		}
	}
}
