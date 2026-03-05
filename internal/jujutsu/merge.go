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

package jujutsu

import "iter"

// Merge represents a [merged value].
//
// [merged value]: https://docs.rs/jj-lib/latest/jj_lib/merge/struct.Merge.html
type Merge[T any] []T

// Resolved returns a [Merge] with a single resolved value.
func Resolved[T any](x T) Merge[T] {
	return Merge[T]{x}
}

// Resolved returns the resolved value, if the merge is resolved.
func (m Merge[T]) Resolved() (x T, isResolved bool) {
	if len(m) != 1 {
		return x, false
	}
	return m[0], true
}

// Adds returns an iterator over the added values.
func (m Merge[T]) Adds() iter.Seq[T] {
	return func(yield func(T) bool) {
		for i := 0; i < len(m); i += 2 {
			if !yield(m[i]) {
				return
			}
		}
	}
}

// Removes returns an iterator over the removed values.
func (m Merge[T]) Removes() iter.Seq[T] {
	return func(yield func(T) bool) {
		for i := 1; i < len(m); i += 2 {
			if !yield(m[i]) {
				return
			}
		}
	}
}
