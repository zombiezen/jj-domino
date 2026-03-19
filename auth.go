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
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/shurcooL/githubv4"
	"golang.org/x/term"
	"zombiezen.com/go/jj-domino/internal/cmderror"
	"zombiezen.com/go/jj-domino/internal/sigterm"
	"zombiezen.com/go/log"
	"zombiezen.com/go/xdgdir"
)

type authCmd struct {
	GitHubLogin authGitHubLoginCmd `kong:"cmd,name=github-login,help=Writes GitHub token to configuration file"`
	Status      authStatusCmd      `kong:"cmd,help=Display the authentication status"`
}

type authStatusCmd struct{}

func (c *authStatusCmd) Run(ctx context.Context, k *kong.Kong, global *cli) error {
	token, err := gitHubToken(ctx, global.environ, global.lookPath)
	if err != nil {
		return err
	}
	httpClient := newGitHubHTTPClient(token)
	defer httpClient.CloseIdleConnections()
	client := githubv4.NewClient(httpClient)

	var query struct {
		Viewer struct {
			Login githubv4.String
		}
	}
	if err := client.Query(ctx, &query, nil); err != nil {
		return err
	}

	fmt.Fprintf(k.Stdout, "Authenticated as: %s\n", query.Viewer.Login)
	return nil
}

type authGitHubLoginCmd struct {
	Quiet bool `kong:"short=q,help=Don\\'t print out prompt"`
}

func (c *authGitHubLoginCmd) Run(ctx context.Context, k *kong.Kong, global *cli) error {
	var configHome string
	if runtime.GOOS == "windows" {
		var err error
		configHome, err = windowsConfigHome(lookupEnvMapFunc(global.environ))
		if err != nil {
			return err
		}
	} else {
		var err error
		configHome, err = (&xdgdir.Dirs{Getenv: lookupEnvMapFunc(global.environ).get}).ConfigHome()
		if err != nil {
			return err
		}
	}

	if !c.Quiet {
		const url = "https://github.com/settings/tokens/new?scopes=repo"
		if useANSIEscapes(k.Stderr, lookupEnvMapFunc(global.environ)) {
			io.WriteString(k.Stderr, "Visit "+osc+"8;;"+url+st+url+endLink+" to generate a new GitHub token.\n")
		} else {
			io.WriteString(k.Stderr, "Visit "+url+" to generate a new GitHub token.\n")
		}
		io.WriteString(k.Stderr, "Token: ")
	}

	type readResult struct {
		s   string
		err error
	}
	readChan := make(chan readResult)
	go func() {
		var result readResult
		result.s, result.err = readPassword(global.stdin, lookupEnvMapFunc(global.environ))
		readChan <- result
	}()
	var token string
	select {
	case result := <-readChan:
		if result.err != nil {
			return result.err
		}
		token = strings.TrimSpace(result.s)
	case <-ctx.Done():
		// Leaks a goroutine, but we'll exit soon anyway.
		io.WriteString(k.Stderr, "\n")
		return ctx.Err()
	}
	if len(token) == 0 {
		return errors.New("no token entered")
	}
	path := filepath.Join(configHome, configSubdirName, "github-token")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	fileContent := make([]byte, 0, len(token)+1)
	fileContent = append(fileContent, token...)
	fileContent = append(fileContent, '\n')
	if err := os.WriteFile(path, fileContent, 0o600); err != nil {
		return err
	}

	if !c.Quiet {
		log.Infof(ctx, "Wrote GitHub token to %s", path)
	}
	return nil
}

// gitHubToken obtains a GitHub personal access token from the environment.
func gitHubToken(ctx context.Context, environ map[string]string, lookPath lookPathFunc) (string, error) {
	const varName = "GITHUB_TOKEN"

	if token := environ[varName]; token != "" {
		log.Debugf(ctx, "Using GitHub API token from %s environment variable", varName)
		return token, nil
	}

	if tokenData, err := readConfigFile(ctx, lookupEnvMapFunc(environ), "github-token"); err == nil {
		log.Debugf(ctx, "Using GitHub API token from configuration file")
		return string(bytes.TrimSpace(tokenData)), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	ghExe, err := lookPath("gh")
	if err != nil {
		// If the gh CLI is not installed, prompt the user to set the environment variable.
		if errors.Is(err, exec.ErrNotFound) {
			return "", fmt.Errorf("%s not set", varName)
		}
		return "", fmt.Errorf("gh auth token: %v", err)
	}
	log.Debugf(ctx, "Calling gh CLI (%s) to get token", ghExe)
	cmd := exec.CommandContext(ctx, ghExe, "auth", "token", "--hostname=github.com")
	cmd.Env = environMapToSlice(environ)
	cmd.Cancel = func() error { return sigterm.CancelProcess(cmd.Process) }
	stderr := new(bytes.Buffer)
	cmd.Stderr = stderr
	raw, err := cmd.Output()
	if err != nil {
		return "", cmderror.New("gh auth token", err, stderr.Bytes())
	}
	return string(bytes.TrimSpace(raw)), nil
}

func readPassword(r io.Reader, lookupEnv lookupEnvFunc) (string, error) {
	if f, ok := r.(*os.File); ok {
		if fd, ok := asTerminal(f, lookupEnv); ok {
			token, err := term.ReadPassword(fd)
			f.WriteString("\n") // Newline from read doesn't get echoed.
			return string(token), err
		}
	}

	br, ok := r.(io.ByteReader)
	if !ok {
		br = byteReader{r}
	}

	sb := new(strings.Builder)
	for {
		b, err := br.ReadByte()
		if b == '\n' || err != nil {
			if err == io.EOF && sb.Len() > 0 {
				err = nil
			}
			s := sb.String()
			if runtime.GOOS == "windows" {
				s = strings.TrimSuffix(s, "\r")
			}
			return s, err
		}
		sb.WriteByte(b)
	}
}

type byteReader struct {
	io.Reader
}

func (br byteReader) ReadByte() (byte, error) {
	var buf [1]byte
	_, err := io.ReadFull(br.Reader, buf[:])
	return buf[0], err
}
