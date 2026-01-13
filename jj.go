package main

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"strings"
)

type Repository struct {
	root string
}

// {"commit_id":"79c26b477dbc70ffec6a897457f9bea97c0969c6","parents":["6254a90cead4685d00d556525c3d1b3e0184ccbe"],"change_id":"qyswkmwzqlpvnokvmqplzyyurtmqvmqz","description":"tunnel.handler: ignore 108/success mismatch\n","author":{"name":"Benjamin Pollack","email":"benjamin@ngrok.com","timestamp":"2026-01-13T18:31:23Z"},"committer":{"name":"Benjamin Pollack","email":"benjamin@ngrok.com","timestamp":"2026-01-13T18:32:12Z"}}
type Changeset struct {
	Id          string `json:"change_id"` // Jujutsu changeset ID
	Sha         string `json:"commit_id"` // git commit
	Description string
	Bookmarks   []string
	Parents     []string
}

func (r *Repository) runJj(args ...string) ([]byte, error) {
	cmd := exec.Command("jj", args...)
	cmd.Dir = r.root
	return cmd.Output()
}

type Bookmark struct {
	Name   string
	Remote string
	Target []string
}

func (r *Repository) getBookmarks() (map[string][]string, error) {
	bookmarksBySha := make(map[string][]string)
	out, err := r.runJj("bookmark", "list", "-T", "json(self)")
	if err != nil {
		return bookmarksBySha, err
	}
	decoder := json.NewDecoder(bytes.NewReader(out))
	for decoder.More() {
		var bookmark Bookmark
		if err := decoder.Decode(&bookmark); err != nil {
			return bookmarksBySha, err
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

func (r *Repository) getChangesets() ([]Changeset, error) {
	changesets := []Changeset{}

	bookmarksBySha, err := r.getBookmarks()
	if err != nil {
		return changesets, err
	}

	out, err := r.runJj("log", "-r", "ancestors(bookmarks()) ~ ::trunk()", "-G", "-T", "json(self)")
	if err != nil {
		return changesets, err
	}
	decoder := json.NewDecoder(bytes.NewReader(out))
	for decoder.More() {
		var changeset Changeset
		if err := decoder.Decode(&changeset); err != nil {
			return changesets, err
		}
		changeset.Bookmarks = bookmarksBySha[changeset.Sha]
		changesets = append(changesets, changeset)
	}
	return changesets, nil
}

func NewRepository(root string) Repository {
	return Repository{root}
}

func getCurrentRoot() (string, error) {
	cmd := exec.Command("jj", "root")
	root, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(root)), nil
}
