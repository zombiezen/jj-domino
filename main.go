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
	"runtime"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/alecthomas/kong"
	"golang.org/x/term"
	"zombiezen.com/go/jj-domino/internal/jujutsu"
	"zombiezen.com/go/jj-domino/internal/sigterm"
	"zombiezen.com/go/log"
	"zombiezen.com/go/xdgdir"
)

// jjDominoVersion is the version string filled in by the linker (e.g. "1.2.3").
var jjDominoVersion string

type cli struct {
	stdin    io.Reader         `kong:"-"`
	environ  map[string]string `kong:"-"`
	lookPath lookPathFunc      `kong:"-"`

	Debug bool `kong:"help=Show debug logs"`

	Submit  submitCmd        `kong:"cmd,default=withargs,help=Submit a review stack"`
	Auth    authCmd          `kong:"cmd,help=Manage credentials"`
	Version kong.VersionFlag `kong:"help=Show version information."`
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
	ctx, cancel := sigterm.NotifyContext(context.Background())

	c := &cli{
		stdin:    os.Stdin,
		environ:  environMap(),
		lookPath: exec.LookPath,
	}
	version := jjDominoVersion
	if version == "" {
		if info, ok := debug.ReadBuildInfo(); !ok {
			version = "(devel)"
		} else {
			version = strings.TrimPrefix(info.Main.Version, "v")
		}
	}
	k := kong.Must(c,
		kong.Name("jj-domino"),
		kong.Vars{"version": "jj-domino " + version},
	)

	var logger log.Logger = &logger{
		out:   os.Stderr,
		color: useColors(os.Stderr, lookupEnvMapFunc(c.environ)),
	}

	kc, err := k.Parse(os.Args[1:])
	if !c.Debug {
		logger = &log.LevelFilter{
			Min:    log.Info,
			Output: logger,
		}
	}
	log.SetDefault(logger)
	if err != nil {
		log.Errorf(ctx, "%v", err)
		os.Exit(1)
	}
	kc.BindTo(ctx, (*context.Context)(nil))
	err = kc.Run()
	cancel()
	if err != nil {
		log.Errorf(ctx, "%v", err)
		os.Exit(1)
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

func readConfigFile(ctx context.Context, lookupEnv lookupEnvFunc, name string) ([]byte, error) {
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
		path := filepath.Join(configDir, configSubdirName, name)
		data, err := os.ReadFile(path)
		if err == nil {
			log.Debugf(ctx, "Found config file at %s", path)
			return data, nil
		}
		log.Debugf(ctx, "Searching config file: %v", err)
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

type logger struct {
	color bool

	mu  sync.Mutex
	out io.Writer
	buf []byte
}

// Log implements [log.Logger].
func (l *logger) Log(ctx context.Context, e log.Entry) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.buf = l.buf[:0]
	l.buf = append(l.buf, "jj-domino: "...)
	switch {
	case e.Level >= log.Error:
		if l.color {
			l.buf = append(l.buf, csi+"31m"...) // red
		}
		l.buf = append(l.buf, "error:"...)
		if l.color {
			l.buf = append(l.buf, csi+"39m"...) // default foreground color
		}
		l.buf = append(l.buf, ' ')
	case e.Level >= log.Warn:
		if l.color {
			l.buf = append(l.buf, csi+"33m"...) // yellow
		}
		l.buf = append(l.buf, "warning:"...)
		if l.color {
			l.buf = append(l.buf, csi+"39m"...) // default foreground color
		}
		l.buf = append(l.buf, ' ')
	}
	l.buf = append(l.buf, e.Msg...)
	if !bytes.HasSuffix(l.buf, []byte("\n")) {
		l.buf = append(l.buf, '\n')
	}

	l.out.Write(l.buf)
}

// LogEnabled implements [log.Logger].
func (l *logger) LogEnabled(log.Entry) bool {
	return true
}

// ANSI escape codes.
// See https://en.wikipedia.org/wiki/ANSI_escape_code
// and details about hyperlinks at https://gist.github.com/egmontkob/eb114294efbcd5adb1944c9f3cb5feda
const (
	// csi is the Control Sequence Introducer (CSI) escape sequence.
	csi = "\x1b["
	// osc is the escape sequence for an Operating System Command (OSC).
	osc = "\x1b]"
	// st is the escape sequence for a String Terminator (ST).
	st = "\x1b\\"

	// endLink is the escape sequence that ends a hyperlink.
	endLink = osc + "8;;" + st
)

func asTerminal(f *os.File, lookupEnv lookupEnvFunc) (int, bool) {
	fd := int(f.Fd())
	if term.IsTerminal(fd) && lookupEnv.get("TERM") != "dumb" {
		return fd, true
	} else {
		return -1, false
	}
}

func useANSIEscapes(f io.Writer, lookupEnv lookupEnvFunc) bool {
	osFile, ok := f.(*os.File)
	if !ok {
		return false
	}
	_, ok = asTerminal(osFile, lookupEnv)
	return ok
}

func useColors(f io.Writer, lookupEnv lookupEnvFunc) bool {
	return useANSIEscapes(f, lookupEnv) && lookupEnv.get("NO_COLOR") == ""
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
