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
	"io"
	"iter"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/alecthomas/kong"
	"golang.org/x/term"
	"zombiezen.com/go/jj-domino/internal/jujutsu"
	"zombiezen.com/go/jj-domino/internal/sigterm"
	"zombiezen.com/go/xdgdir"
)

type cli struct {
	stdin    io.Reader         `kong:"-"`
	environ  map[string]string `kong:"-"`
	lookPath lookPathFunc      `kong:"-"`

	Submit submitCmd `kong:"cmd,default=withargs,help=Submit a review stack"`
	Auth   authCmd   `kong:"cmd,help=Manage credentials"`
}

func (c *cli) newJujutsu() (*jujutsu.Jujutsu, error) {
	jjExe, err := c.lookPath("jj")
	if err != nil {
		return nil, err
	}
	return jujutsu.New(jujutsu.Options{
		JJExe: jjExe,
		Env:   environMapToSlice(c.environ),
	})
}

func main() {
	c := &cli{
		stdin:    os.Stdin,
		environ:  environMap(),
		lookPath: exec.LookPath,
	}
	k := kong.Parse(c, kong.UsageOnError())
	ctx, cancel := sigterm.NotifyContext(context.Background())
	k.BindTo(ctx, (*context.Context)(nil))
	err := k.Run()
	cancel()
	if err != nil {
		log.Fatal(err)
	}
}

// A lookupEnvFunc is a function that retrieves environment variables by key.
// The de facto implementation of lookupEnvFunc is [os.LookupEnv],
// but others exist for testing.
type lookupEnvFunc func(key string) (string, bool)

// lookupEnvMapFunc returns a [lookupEnvFunc]
// that looks up environment variables
// from the given map.
func lookupEnvMapFunc(m map[string]string) lookupEnvFunc {
	return func(key string) (string, bool) {
		v, ok := m[key]
		return v, ok
	}
}

// get retrieves the environment variable named by the key.
// It returns the value, which will be empty if the variable is not present.
func (f lookupEnvFunc) get(key string) string {
	v, _ := f(key)
	return v
}

// A lookPathFunc is a function that searches for the executable named file in the current path.
// The de facto implementation of lookPathFunc is [exec.LookPath].
type lookPathFunc func(file string) (string, error)

const configSubdirName = "jj-domino"

func readConfigFile(lookupEnv lookupEnvFunc, name string) ([]byte, error) {
	var configDirs iter.Seq[string]
	if runtime.GOOS == "windows" {
		configHome, err := windowsConfigHome(lookupEnv)
		if err != nil {
			return nil, fmt.Errorf("read %s config: %v", name, err)
		}
		configDirs = func(yield func(string) bool) {
			yield(configHome)
		}
	} else {
		configDirs = (&xdgdir.Dirs{Getenv: lookupEnv.get}).ConfigDirs()
	}

	var firstError error
	for configDir := range configDirs {
		data, err := os.ReadFile(filepath.Join(configDir, configSubdirName, name))
		if err == nil {
			return data, nil
		}
		if firstError == nil ||
			(errors.Is(firstError, os.ErrNotExist) && !errors.Is(err, os.ErrNotExist)) {
			firstError = err
		}
	}
	return nil, firstError
}

func windowsConfigHome(lookupEnv lookupEnvFunc) (string, error) {
	appData := lookupEnv.get("AppData")
	if appData == "" {
		return "", errors.New("%AppData% not set")
	}
	return appData, nil
}

// ANSI escape codes.
// Details about hyperlinks at https://gist.github.com/egmontkob/eb114294efbcd5adb1944c9f3cb5feda
const (
	// osc is the escape sequence for an Operating System Command (OSC).
	osc = "\x1b]"
	// st is the escape sequence for a String Terminator (ST).
	st = "\x1b\\"

	// endLink is the escape sequence that ends a hyperlink.
	endLink = osc + "8;;" + st
)

func isTerminal(f io.Writer) bool {
	osFile, ok := f.(*os.File)
	return ok && term.IsTerminal(int(osFile.Fd()))
}

// environMap returns the environment variables as a map.
func environMap() map[string]string {
	environ := os.Environ()
	m := make(map[string]string, len(environ))
	for _, kv := range environ {
		k, v, _ := strings.Cut(kv, "=")
		m[k] = v
	}
	return m
}

// environMapToSlice returns a slice of strings for each entry in m in the format "key=value".
// The order is undefined.
func environMapToSlice(m map[string]string) []string {
	slice := make([]string, 0, len(m))
	for k, v := range m {
		slice = append(slice, k+"="+v)
	}
	return slice
}
