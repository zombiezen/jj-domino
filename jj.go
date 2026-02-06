package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type Repository struct {
	root string
}

type Changeset struct {
	Id          string   `json:"change_id"` // Jujutsu changeset ID
	Sha         string   `json:"commit_id"` // git commit
	Description string   `json:"description"`
	Bookmarks   []string `json:"-"` // populated separately
	Parents     []string `json:"parents"`
}

func (r *Repository) runJj(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "jj", args...)
	cmd.Dir = r.root
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("jj %s: %w\n%s", args[0], err, exitErr.Stderr)
		}
		return nil, err
	}
	return out, nil
}

type Bookmark struct {
	Name   string
	Remote string
	Target []string
}

func (r *Repository) getBookmarks(ctx context.Context) (map[string][]string, error) {
	out, err := r.runJj(ctx, "bookmark", "list", "-T", "json(self)")
	if err != nil {
		return nil, err
	}
	bookmarksBySha := make(map[string][]string)
	decoder := json.NewDecoder(bytes.NewReader(out))
	for decoder.More() {
		var bookmark Bookmark
		if err := decoder.Decode(&bookmark); err != nil {
			return nil, err
		}
		if bookmark.Remote != "" {
			continue
		}
		for _, sha := range bookmark.Target {
			bookmarksBySha[sha] = append(bookmarksBySha[sha], bookmark.Name)
		}
	}
	return bookmarksBySha, nil
}

func (r *Repository) getChangesets(ctx context.Context) ([]Changeset, error) {
	bookmarksBySha, err := r.getBookmarks(ctx)
	if err != nil {
		return nil, err
	}

	out, err := r.runJj(ctx, "log", "-r", "mutable() & (ancestors(bookmarks()) ~ ::trunk())", "--no-graph", "-T", "json(self)")
	if err != nil {
		return nil, err
	}
	var changesets []Changeset
	decoder := json.NewDecoder(bytes.NewReader(out))
	for decoder.More() {
		var changeset Changeset
		if err := decoder.Decode(&changeset); err != nil {
			return nil, err
		}
		changeset.Bookmarks = bookmarksBySha[changeset.Sha]
		changesets = append(changesets, changeset)
	}
	return changesets, nil
}

func NewRepository(root string) Repository {
	return Repository{root}
}

func getCurrentRoot(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "jj", "root")
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", fmt.Errorf("jj root: %w\n%s", err, exitErr.Stderr)
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
