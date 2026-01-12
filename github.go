package main

import (
	"errors"
	"os"
	"os/exec"
	"strings"

	"github.com/google/go-github/v81/github"
)

func githubToken() (string, error) {
	// Prefer `gh`, fall back to env vars if not available
	cmd := exec.Command("gh", "auth", "status")
	if err := cmd.Run(); err == nil {
		cmd = exec.Command("gh", "auth", "token")
		var raw []byte
		if raw, err = cmd.Output(); err == nil {
			return strings.TrimSpace(string(raw)), nil
		}
	}
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token, nil
	} else if token := os.Getenv("GH_TOKEN"); token != "" {
		return token, nil
	}
	return "", errors.New("no token found")
}

func getClient() (*github.Client, error) {
	token, err := githubToken()
	if err != nil {
		return nil, err
	}
	return github.NewClient(nil).WithAuthToken(token), nil
}
