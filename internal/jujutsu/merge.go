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
