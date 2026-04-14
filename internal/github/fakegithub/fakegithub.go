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

package fakegithub

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"sync"

	jsonv2 "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
	"github.com/shurcooL/githubv4"
	"zombiezen.com/go/jj-domino/internal/github"
)

type repository struct {
	github.Repository
	prs map[githubv4.ID]*github.PullRequest
}

type Server struct {
	muxInit sync.Once
	mux     *http.ServeMux

	mu    sync.Mutex
	login string
	repos map[githubv4.ID]*repository
}

func (srv *Server) SetViewer(viewerLogin string) {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	srv.login = viewerLogin
}

func (srv *Server) CreateRepository(path github.RepositoryPath) error {
	if path.Owner == "" {
		return fmt.Errorf("create repository %v: missing owner", path)
	}
	if path.Repo == "" {
		return fmt.Errorf("create repository %v: missing name", path)
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()

	if srv.repos == nil {
		srv.repos = make(map[githubv4.ID]*repository)
	}
	for _, repo := range srv.repos {
		if repo.Path() == path {
			return fmt.Errorf("create repository %v: already exists", path)
		}
	}
	id := newID()
	srv.repos[id] = &repository{
		Repository: github.Repository{
			ID: id,
			Owner: &github.RepositoryOwner{
				Login: githubv4.String(path.Owner),
			},
			Name: githubv4.String(path.Repo),
		},
		prs: make(map[githubv4.ID]*github.PullRequest),
	}
	return nil
}

func (srv *Server) initMux() {
	srv.mux = http.NewServeMux()
	srv.mux.HandleFunc("POST /graphql", srv.graphql)
}

func (srv *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	srv.muxInit.Do(srv.initMux)
	srv.mux.ServeHTTP(w, r)
}

func (srv *Server) graphql(w http.ResponseWriter, r *http.Request) {
	contentType := r.Header.Get("Content-Type")
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil || mt != "application/json" {
		http.Error(w, fmt.Sprintf("invalid request type %s", contentType), http.StatusUnsupportedMediaType)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	parsed := new(graphqlRequest)
	if err := jsonv2.Unmarshal(body, parsed); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	handlers := map[string]func(*Server, *url.URL, *graphqlRequest) (*graphqlResponse, error){
		viewerLoginQuery:          (*Server).viewerLoginQuery,
		fetchRepositoryQuery:      (*Server).fetchRepositoryQuery,
		createPullRequestMutation: (*Server).createPullRequestMutation,
	}
	var resp *graphqlResponse
	var handlerError error
	if handler := handlers[parsed.Query]; handler != nil {
		base := &url.URL{
			Scheme: "http",
			Host:   r.Host,
		}
		if r.TLS != nil {
			base.Scheme = "https"
		}
		resp, handlerError = handler(srv, base, parsed)
	} else {
		resp = newErrorResponse("unhandled query: " + strconv.Quote(parsed.Query))
	}

	if resp == nil {
		resp = newErrorResponse(handlerError.Error())
	}
	responseData, err := jsonv2.Marshal(resp)
	if err != nil {
		http.Error(w, "failed to marshal response: "+err.Error(), http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/graphql-response+json")
	h.Set("Content-Length", strconv.Itoa(len(responseData)))
	if handlerError != nil {
		w.WriteHeader(http.StatusInternalServerError)
	} else if resp.isRequestError() {
		w.WriteHeader(http.StatusUnprocessableEntity)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	w.Write(responseData)
}

const viewerLoginQuery = "{viewer{login}}"

func (srv *Server) viewerLoginQuery(base *url.URL, req *graphqlRequest) (*graphqlResponse, error) {
	srv.mu.Lock()
	defer srv.mu.Unlock()

	if srv.login == "" {
		return &graphqlResponse{
			Data: jsontext.Value(`{"viewer":null}`),
		}, nil
	}

	var data struct {
		Viewer struct {
			Login githubv4.String
		}
	}
	data.Viewer.Login = githubv4.String(srv.login)
	return marshalDataResponse(data)
}

const fetchRepositoryQuery = "" +
	"query($name:String!$owner:String!){" +
	"repository(owner: $owner, name: $name){" +
	"id," +
	"name," +
	"owner{login}" +
	"}" +
	"}"

func (srv *Server) fetchRepositoryQuery(base *url.URL, req *graphqlRequest) (*graphqlResponse, error) {
	var path github.RepositoryPath
	if err := jsonv2.Unmarshal(req.Variables["owner"], &path.Owner); err != nil {
		return newErrorResponse("invalid $owner: " + err.Error()), nil
	}
	if err := jsonv2.Unmarshal(req.Variables["name"], &path.Repo); err != nil {
		return newErrorResponse("invalid $name: " + err.Error()), nil
	}

	var data struct {
		Repository *github.Repository `json:"repository"`
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()
	for _, r := range srv.repos {
		if r.Path() == path {
			data.Repository = &r.Repository
			break
		}
	}
	return marshalDataResponse(data)
}

const createPullRequestMutation = "" +
	"mutation($input:CreatePullRequestInput!){" +
	"createPullRequest(input: $input){" +
	"pullRequest{" +
	"id," +
	"number," +
	"url" +
	"}" +
	"}" +
	"}"

func (srv *Server) createPullRequestMutation(base *url.URL, req *graphqlRequest) (*graphqlResponse, error) {
	var input struct {
		BaseRefName      string      `json:"baseRefName"`
		BaseRepositoryID githubv4.ID `json:"repositoryId"`
		HeadRefName      string      `json:"headRefName"`
		HeadRepositoryID githubv4.ID `json:"headRepositoryId"`
		Title            string      `json:"title"`
		Body             string      `json:"body"`
		IsDraft          bool        `json:"draft"`
	}
	if err := jsonv2.Unmarshal(req.Variables["input"], &input); err != nil {
		return newErrorResponse(fmt.Sprintf("$input: %v", err)), nil
	}
	if input.BaseRefName == "" {
		return newErrorResponse("missing base ref"), nil
	}
	if input.BaseRepositoryID == nil {
		return newErrorResponse("missing repository ID"), nil
	}
	if input.HeadRefName == "" {
		return newErrorResponse("missing head ref"), nil
	}
	if input.Title == "" {
		return newErrorResponse("empty title"), nil
	}

	var data struct {
		CreatePullRequest struct {
			PullRequest struct {
				ID     githubv4.ID  `json:"id"`
				Number githubv4.Int `json:"number"`
				URL    githubv4.URI `json:"url"`
			} `json:"pullRequest"`
		} `json:"createPullRequest"`
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()
	r := srv.repos[input.BaseRepositoryID]
	if r == nil {
		return newErrorResponse(fmt.Sprintf("no such repository: %v", input.BaseRepositoryID)), nil
	}
	headRepo := r
	if input.HeadRepositoryID != nil {
		headRepo = srv.repos[input.HeadRepositoryID]
		if headRepo == nil {
			return newErrorResponse(fmt.Sprintf("no such repository: %v", input.HeadRepositoryID)), nil
		}
	}
	id := newID()
	n := len(r.prs) + 1
	r.prs[id] = &github.PullRequest{
		ID:             id,
		Number:         githubv4.Int(n),
		BaseRefName:    githubv4.String(input.BaseRefName),
		HeadRefName:    githubv4.String(input.HeadRefName),
		HeadRepository: &headRepo.Repository,
		Title:          githubv4.String(input.Title),
		Body:           githubv4.String(input.Body),
		IsDraft:        githubv4.Boolean(input.IsDraft),
	}
	data.CreatePullRequest.PullRequest.ID = id
	data.CreatePullRequest.PullRequest.Number = githubv4.Int(n)
	data.CreatePullRequest.PullRequest.URL = githubv4.URI{URL: base.ResolveReference(&url.URL{
		Path: fmt.Sprintf("/%s/pull/%d", r.Path().String(), n),
	})}
	return marshalDataResponse(data)
}

type graphqlRequest struct {
	Query     string                    `json:"query"`
	Operation string                    `json:"operation"`
	Variables map[string]jsontext.Value `json:"variables"`
}

type graphqlResponse struct {
	Data   jsontext.Value  `json:"data,omitzero"`
	Errors []*graphqlError `json:"errors,omitempty"`
}

func marshalDataResponse(data any) (*graphqlResponse, error) {
	dataJSON, err := jsonv2.Marshal(data)
	if err != nil {
		return nil, err
	}
	return &graphqlResponse{Data: dataJSON}, nil
}

func newErrorResponse(msg string) *graphqlResponse {
	return &graphqlResponse{
		Errors: []*graphqlError{
			{Message: msg},
		},
	}
}

func (resp *graphqlResponse) isRequestError() bool {
	return len(resp.Data) == 0 && len(resp.Errors) > 0
}

type graphqlError struct {
	Message string `json:"message"`
}

func newID() githubv4.ID {
	var buf [16]byte
	rand.Read(buf[:])
	return base64.StdEncoding.EncodeToString(buf[:])
}
