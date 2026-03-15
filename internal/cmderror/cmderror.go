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

// Package cmderror provides a function that formats a subprocess failure into a rich error.
package cmderror

import (
	"bytes"
	"errors"
	"fmt"
)

// New returns a new error with the information from an unsuccessful run of a subprocess.
func New(prefix string, runError error, stderr []byte) error {
	stderr = bytes.TrimSuffix(stderr, []byte{'\n'})
	if len(stderr) == 0 {
		return fmt.Errorf("%s: %w", prefix, runError)
	}
	if exitCode(runError) != -1 {
		if bytes.IndexByte(stderr, '\n') == -1 {
			// Collapse into single line.
			return &formattedError{
				msg:   fmt.Sprintf("%s: %s", prefix, stderr),
				cause: runError,
			}
		}
		return &formattedError{
			msg:   fmt.Sprintf("%s:\n%s", prefix, stderr),
			cause: runError,
		}
	}
	return fmt.Errorf("%s: %w\n%s", prefix, runError, stderr)
}

// exitCode returns the exit code indicated by the error,
// zero if the error is nil,
// or -1 if the error doesn't indicate an exited process.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var coder interface {
		ExitCode() int
	}
	if !errors.As(err, &coder) {
		return -1
	}
	return coder.ExitCode()
}

type formattedError struct {
	msg   string
	cause error
}

func (e *formattedError) Error() string {
	return e.msg
}

func (e *formattedError) Unwrap() error {
	return e.cause
}
