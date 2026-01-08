package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type server struct {
	username string
	password string
	groupID  string
	userID   string
}

func (s *server) authOK(r *http.Request) bool {
	if s.username == "" && s.password == "" {
		return true
	}
	u, p, ok := r.BasicAuth()
	return ok && u == s.username && p == s.password
}

func (s *server) handleSearch(w http.ResponseWriter, r *http.Request) {
	if !s.authOK(r) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func (s *server) handleGroupUsers(w http.ResponseWriter, r *http.Request) {
	if !s.authOK(r) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	// Expect /api/sp/groups/{group}/users.json
	path := strings.TrimPrefix(r.URL.Path, "/api/sp/groups/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 || !strings.HasSuffix(parts[1], "users.json") {
		http.NotFound(w, r)
		return
	}
	group := parts[0]
	page := 1
	if q := r.URL.Query().Get("page"); q != "" {
		if v, err := strconv.Atoi(q); err == nil {
			page = v
		}
	}
	type item struct {
		ID string `json:"id"`
	}
	resp := map[string]any{
		"results":     []item{},
		"total_pages": 1,
	}
	if page == 1 && group == s.groupID {
		resp["results"] = []item{{ID: s.userID}}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func main() {
	var (
		listen     string
		username   string
		password   string
		groupID    string
		userID     string
		readiness  string
		readyAfter time.Duration
	)
	flag.StringVar(&listen, "listen", ":18080", "listen address, e.g., :18080 or 127.0.0.1:0 for random port")
	flag.StringVar(&username, "username", os.Getenv("EMP_HTTP_USERNAME"), "basic auth username")
	flag.StringVar(&password, "password", os.Getenv("EMP_HTTP_PASSWORD"), "basic auth password")
	flag.StringVar(&groupID, "group", os.Getenv("EMP_HTTP_GROUP_ID"), "group id to respond with")
	flag.StringVar(&userID, "user", os.Getenv("EMP_HTTP_USER_INTERNAL_USERNAME"), "user id to return in results")
	flag.StringVar(&readiness, "ready-file", "", "optional file path to write base URL when server is ready")
	flag.DurationVar(&readyAfter, "ready-after", 0, "optional artificial delay before reporting readiness")
	flag.Parse()

	s := &server{username: username, password: password, groupID: groupID, userID: userID}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/sp/search.json", s.handleSearch)
	mux.HandleFunc("/api/sp/groups/", s.handleGroupUsers)

	ln, err := net.Listen("tcp", listen)
	if err != nil {
		log.Fatalf("listen error: %v", err)
	}
	addr := ln.Addr().String()
	base := fmt.Sprintf("http://%s", addr)
	log.Printf("EMP dummy server listening at %s", base)

	go func() {
		if readyAfter > 0 {
			time.Sleep(readyAfter)
		}
		if readiness != "" {
			_ = os.WriteFile(readiness, []byte(base), 0o644)
		}
	}()

	srv := &http.Server{Handler: mux}
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Fatalf("serve error: %v", err)
	}
}
