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

// Package httptransport provides HTTP client middleware.
package httptransport

import "net/http"

// BearerToken is an [http.RoundTripper]
// that adds an Authorization header to requests to a particular host.
type BearerToken struct {
	Host         string
	Token        string
	RoundTripper http.RoundTripper
}

// RoundTrip implements [http.RoundTripper]
// by adding an Authorization header if applicable.
func (bt BearerToken) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host == bt.Host && (req.Host == "" || req.Host == bt.Host) {
		req = req.Clone(req.Context())
		req.Header.Set("Authorization", "Bearer "+bt.Token)
	}
	return bt.RoundTripper.RoundTrip(req)
}

// CloseIdleConnections calls tt.rt.CloseIdleConnections(), if present.
func (bt BearerToken) CloseIdleConnections() {
	cic, ok := bt.RoundTripper.(interface {
		CloseIdleConnections()
	})
	if ok {
		cic.CloseIdleConnections()
	}
}

// UserAgent is an [http.RoundTripper]
// that adds a User-Agent header to requests.
type UserAgent struct {
	UserAgent    string
	RoundTripper http.RoundTripper
}

// RoundTrip implements [http.RoundTripper]
// by adding an User-Agent header if not present.
func (ua UserAgent) RoundTrip(req *http.Request) (*http.Response, error) {
	if len(req.Header.Values("User-Agent")) == 0 {
		req = req.Clone(req.Context())
		req.Header.Set("User-Agent", ua.UserAgent)
	}
	return ua.RoundTripper.RoundTrip(req)
}

// CloseIdleConnections calls uat.rt.CloseIdleConnections(), if present.
func (ua UserAgent) CloseIdleConnections() {
	cic, ok := ua.RoundTripper.(interface {
		CloseIdleConnections()
	})
	if ok {
		cic.CloseIdleConnections()
	}
}
