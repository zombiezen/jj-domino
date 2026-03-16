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

import (
	"errors"
	"fmt"
	"iter"
	"maps"
	"slices"
	"strings"

	jsonv2 "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
	shsyntax "mvdan.cc/sh/v3/syntax"
)

// CommandNameAndArgs represents a subprocess invocation.
type CommandNameAndArgs struct {
	s    string
	argv []string
	env  map[string]string
}

var _ interface {
	jsonv2.MarshalerTo
	jsonv2.UnmarshalerFrom
} = (*CommandNameAndArgs)(nil)

// CommandString returns a new [*CommandNameAndArgs]
// from a string with Bash-shell-like syntax.
func CommandString(s string) *CommandNameAndArgs {
	return &CommandNameAndArgs{s: s}
}

// CommandArgv returns a new [*CommandNameAndArgs]
// from a command, its arguments, and environment variables.
func CommandArgv(command string, args []string, env map[string]string) *CommandNameAndArgs {
	c := &CommandNameAndArgs{
		argv: make([]string, 1, len(args)+1),
		env:  env,
	}
	c.argv[0] = command
	c.argv = append(c.argv, args...)
	return c
}

// Argv returns a copy of the command name and its arguments.
// If the command was returned from [CommandString] and its syntax is invalid,
// then Argv will split the string by whitespace.
func (c *CommandNameAndArgs) Argv() []string {
	if c == nil {
		return nil
	}
	if c.s == "" {
		return slices.Clone(c.argv)
	}
	words := shsyntax.NewParser(shsyntax.Variant(shsyntax.LangBash)).WordsSeq(strings.NewReader(c.s))
	var result []string
	for w, err := range words {
		if err != nil {
			return strings.Fields(c.s)
		}
		ws, err := formatShellWord(w)
		if err != nil {
			return strings.Fields(c.s)
		}
		result = append(result, ws)
	}
	return result
}

// Environ returns an iterator over the command's environment variables.
func (c *CommandNameAndArgs) Environ() iter.Seq2[string, string] {
	if c == nil {
		return func(yield func(string, string) bool) {}
	}
	return func(yield func(string, string) bool) {
		maps.All(c.env)(yield)
	}
}

// String returns the command in vaguely Bash syntax.
// If the receiver was created through [CommandString],
// it returns the argument from [CommandString].
// Otherwise, the command is escaped into Bash syntax.
func (c *CommandNameAndArgs) String() string {
	if c == nil {
		return ""
	}
	if c.s != "" {
		return c.s
	}
	sb := new(strings.Builder)
	for i, k := range slices.Sorted(maps.Keys(c.env)) {
		if i > 0 {
			sb.WriteString(" ")
		}
		sb.WriteString(k)
		sb.WriteString("=")
		if q, err := shsyntax.Quote(c.env[k], shsyntax.LangBash); err == nil {
			sb.WriteString(q)
		} else {
			sb.WriteString(`''`)
		}
	}
	for i, arg := range c.argv {
		if i > 0 || len(c.env) > 0 {
			sb.WriteString(" ")
		}
		if q, err := shsyntax.Quote(arg, shsyntax.LangBash); err == nil {
			sb.WriteString(q)
		} else {
			sb.WriteString(`''`)
		}
	}
	return sb.String()
}

// MarshalJSONTo implements [jsonv2.MarshalerTo]
// by encoding the command as a string, array,
// or a {"command": <array>, "env": <object>} object.
func (c *CommandNameAndArgs) MarshalJSONTo(enc *jsontext.Encoder) error {
	switch {
	case c.s == "" && len(c.argv) == 0:
		return fmt.Errorf("marshal command: empty")
	case c.s != "" && len(c.argv) > 0:
		return fmt.Errorf("marshal command: both string and vector set")
	case c.s != "" && len(c.env) > 0:
		return fmt.Errorf("marshal command: cannot set string and environment")
	case c.s != "":
		return enc.WriteToken(jsontext.String(c.s))
	case len(c.env) == 0:
		if err := c.marshalArgvJSONTo(enc); err != nil {
			return fmt.Errorf("marshal command: %w", err)
		}
		return nil
	default:
		if err := enc.WriteToken(jsontext.BeginObject); err != nil {
			return fmt.Errorf("marshal command: %w", err)
		}
		if err := enc.WriteToken(jsontext.String("env")); err != nil {
			return fmt.Errorf("marshal command: %w", err)
		}
		if err := enc.WriteToken(jsontext.BeginObject); err != nil {
			return fmt.Errorf("marshal command: %w", err)
		}
		keys := maps.Keys(c.env)
		if deterministic, _ := jsonv2.GetOption(enc.Options(), jsonv2.Deterministic); deterministic {
			keysSlice := slices.AppendSeq(make([]string, 0, len(c.env)), keys)
			slices.Sort(keysSlice)
			keys = slices.Values(keysSlice)
		}
		for k := range keys {
			if err := enc.WriteToken(jsontext.String(k)); err != nil {
				return fmt.Errorf("marshal command: %w", err)
			}
			if err := enc.WriteToken(jsontext.String(c.env[k])); err != nil {
				return fmt.Errorf("marshal command: %w", err)
			}
		}
		if err := enc.WriteToken(jsontext.EndObject); err != nil {
			return fmt.Errorf("marshal command: %w", err)
		}
		if err := enc.WriteToken(jsontext.String("command")); err != nil {
			return fmt.Errorf("marshal command: %w", err)
		}
		if err := c.marshalArgvJSONTo(enc); err != nil {
			return fmt.Errorf("marshal command: %w", err)
		}
		return nil
	}
}

func (c *CommandNameAndArgs) marshalArgvJSONTo(enc *jsontext.Encoder) error {
	if err := enc.WriteToken(jsontext.BeginArray); err != nil {
		return err
	}
	for _, arg := range c.argv {
		if err := enc.WriteToken(jsontext.String(arg)); err != nil {
			return err
		}
	}
	if err := enc.WriteToken(jsontext.EndArray); err != nil {
		return err
	}
	return nil
}

// UnmarshalJSONFrom implements [jsonv2.UnmarshalerFrom]
// by decoding a string, array, or an object.
func (c *CommandNameAndArgs) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	switch dec.PeekKind() {
	case jsontext.KindString:
		tok, err := dec.ReadToken()
		if err != nil {
			return fmt.Errorf("unmarshal command: %w", err)
		}
		*c = CommandNameAndArgs{s: tok.String()}
	case jsontext.KindBeginArray:
		c.s = ""
		clear(c.env)
		if err := jsonv2.UnmarshalDecode(dec, &c.argv); err != nil {
			return fmt.Errorf("unmarshal command: %w", err)
		}
		if len(c.argv) == 0 {
			return errors.New("unmarshal command: empty argv")
		}
	case jsontext.KindBeginObject:
		var parsed struct {
			Env  map[string]string `json:"env"`
			Argv []string          `json:"command"`
		}
		if err := jsonv2.UnmarshalDecode(dec, &parsed); err != nil {
			return fmt.Errorf("unmarshal command: %w", err)
		}
		c.s = ""
		c.argv = parsed.Argv
		c.env = parsed.Env
		if len(c.argv) == 0 {
			return errors.New("unmarshal command: empty argv")
		}
		return nil
	default:
		return fmt.Errorf("unmarshal command: must be a string, array, or object")
	}
	return nil
}

func formatShellWord(w *shsyntax.Word) (string, error) {
	sb := new(strings.Builder)
	for _, part := range w.Parts {
		if err := formatShellWordPart(sb, part); err != nil {
			return "", err
		}
	}
	return sb.String(), nil
}

func formatShellWordPart(sb *strings.Builder, part shsyntax.WordPart) error {
	switch part := part.(type) {
	case *shsyntax.Lit:
		sb.WriteString(part.Value)
	case *shsyntax.SglQuoted:
		sb.WriteString(part.Value)
	case *shsyntax.DblQuoted:
		for _, p := range part.Parts {
			formatShellWordPart(sb, p)
		}
	case *shsyntax.ParamExp:
		if !part.Short {
			return fmt.Errorf("unhandled parameter expansion for ${%s}", part.Param.Value)
		}
		sb.WriteByte('$')
		sb.WriteString(part.Param.Value)
	default:
		return errors.New("unhandled shell syntax")
	}
	return nil
}
