// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
)

// empHTTPTestServer encapsulates a local dummy HTTP server that mimics
// the Generic HTTP provider expected payload and auth behavior.
type empHTTPTestServer struct {
	srv      *httptest.Server
	username string
	password string
	// fixed group id and user id used to produce deterministic output
	groupID string
	userID  string
}

func (s *empHTTPTestServer) Close() {
	if s != nil && s.srv != nil {
		s.srv.Close()
	}
}

// newEMPHTTPTestServer starts an httptest.Server that serves:
//   - GET /api/sp/search.json -> 200 OK (test connection)
//   - GET /api/sp/groups/{group}/users.json?page=N -> JSON with fields
//     { "results": [{"id": <userID>}], "total_pages": 1 } on page 1,
//     empty results on other pages. Requires Basic Auth if username/password set.
func newEMPHTTPTestServer(username, password, groupID, userID string) *empHTTPTestServer {
	h := &empHTTPTestServer{username: username, password: password, groupID: groupID, userID: userID}

	mux := http.NewServeMux()

	// Health/test endpoint
	mux.HandleFunc("/api/sp/search.json", func(w http.ResponseWriter, r *http.Request) {
		if !h.authOK(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	mux.HandleFunc("/api/sp/groups/", func(w http.ResponseWriter, r *http.Request) {
		if !h.authOK(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		// Expect pattern: /api/sp/groups/{group}/users.json
		path := strings.TrimPrefix(r.URL.Path, "/api/sp/groups/")
		// simple split to extract group and suffix
		// e.g., "<group>/users.json"
		segs := strings.SplitN(path, "/", 2)
		if len(segs) != 2 || !strings.HasSuffix(segs[1], "users.json") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		group := segs[0]
		// pagination: default page=1
		page := 1
		if q := r.URL.Query().Get("page"); q != "" {
			if v, err := strconv.Atoi(q); err == nil {
				page = v
			}
		}
		// Build response
		type item struct {
			ID string `json:"id"`
		}
		resp := map[string]any{
			"results":     []item{},
			"total_pages": 1,
		}
		if page == 1 && group == h.groupID {
			resp["results"] = []item{{ID: h.userID}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	srv := httptest.NewServer(mux)
	h.srv = srv
	return h
}

func (s *empHTTPTestServer) authOK(r *http.Request) bool {
	// If credentials are configured, require BasicAuth to match. If empty, allow.
	if s.username == "" && s.password == "" {
		return true
	}
	u, p, ok := r.BasicAuth()
	return ok && u == s.username && p == s.password
}

// baseURL returns the server base URL (e.g., http://127.0.0.1:XXXXX)
func (s *empHTTPTestServer) baseURL() string { return s.srv.URL }
