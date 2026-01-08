package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strings"

	ldap "github.com/nmcclain/ldap"
)

// minimal LDAP test server for E2E. It supports:
//   - Simple bind with provided DN/PW (empty DN+PW allowed as anonymous)
//   - Search on provided baseDN with filter extracting CN and returning a single entry
//     with multi-valued "member" attribute for provided usernames.
type server struct {
	s      *ldap.Server
	ln     net.Listener
	bindDN string
	bindPW string
	baseDN string
	group  string
	user   string
}

type bindHandler struct{ dn, pw string }

func (b bindHandler) Bind(bindDN string, bindSimplePw string, _ net.Conn) (ldap.LDAPResultCode, error) {
	if (bindDN == "" && bindSimplePw == "") || (bindDN == b.dn && bindSimplePw == b.pw) {
		return ldap.LDAPResultSuccess, nil
	}
	return ldap.LDAPResultInvalidCredentials, nil
}

type searchHandler struct{ baseDN, group, user string }

func (h searchHandler) Search(_ string, req ldap.SearchRequest, _ net.Conn) (ldap.ServerSearchResult, error) {
	if !strings.EqualFold(req.BaseDN, h.baseDN) {
		return ldap.ServerSearchResult{ResultCode: ldap.LDAPResultSuccess}, nil
	}
	cn := extractCN(req.Filter)
	if cn == "" || cn != h.group {
		return ldap.ServerSearchResult{ResultCode: ldap.LDAPResultSuccess}, nil
	}
	entry := &ldap.Entry{DN: h.baseDN, Attributes: []*ldap.EntryAttribute{{Name: "member", Values: []string{fmt.Sprintf("CN=%s,CN=Users,%s", h.user, h.baseDN)}}}}
	return ldap.ServerSearchResult{Entries: []*ldap.Entry{entry}, ResultCode: ldap.LDAPResultSuccess}, nil
}

func newServer(bindDN, bindPW, baseDN, group, user string) *server {
	s := &server{
		s:      ldap.NewServer(),
		bindDN: bindDN,
		bindPW: bindPW,
		baseDN: baseDN,
		group:  group,
		user:   user,
	}

	s.s.BindFunc("", bindHandler{dn: s.bindDN, pw: s.bindPW})
	s.s.SearchFunc("", searchHandler{baseDN: s.baseDN, group: s.group, user: s.user})

	return s
}

func lower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

func extractCN(filter string) string {
	// very naive (CN=...) extractor; good enough for our tests
	f := lower(filter)
	idx := -1
	for i := 0; i+4 <= len(f); i++ {
		if f[i:i+4] == "(cn=" {
			idx = i + 4
			break
		}
	}
	if idx < 0 {
		return ""
	}
	end := idx
	for end < len(f) && f[end] != ')' {
		end++
	}
	if end <= idx {
		return ""
	}
	return filter[idx:end]
}

func (s *server) serve(listen string) (string, error) {
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return "", err
	}
	s.ln = ln
	go func() { _ = s.s.Serve(ln) }()
	return ln.Addr().String(), nil
}

func (s *server) close() {
	if s == nil {
		return
	}
	if s.s != nil {
		s.s.Quit <- true
	}
	if s.ln != nil {
		_ = s.ln.Close()
	}
}

func main() {
	var (
		listen    string
		bindDN    string
		bindPW    string
		baseDN    string
		group     string
		user      string
		readyFile string
	)
	flag.StringVar(&listen, "listen", "127.0.0.1:0", "listen address, e.g., 127.0.0.1:0")
	flag.StringVar(&bindDN, "bind-dn", os.Getenv("LDAP_GROUP_PROVIDER_BIND_DN"), "bind DN")
	flag.StringVar(&bindPW, "bind-pw", os.Getenv("LDAP_GROUP_PROVIDER_BIND_PW"), "bind password")
	flag.StringVar(&baseDN, "base-dn", os.Getenv("LDAP_GROUP_PROVIDER_BASE_DN"), "base DN")
	flag.StringVar(&group, "group", os.Getenv("LDAP_GROUP_PROVIDER_GROUP_NAME"), "group CN to match")
	flag.StringVar(&user, "user", os.Getenv("LDAP_GROUP_PROVIDER_USER_INTERNAL_USERNAME"), "single username to return as member")
	flag.StringVar(&readyFile, "ready-file", "", "write ldap://host:port to this file when ready")
	flag.Parse()

	srv := newServer(bindDN, bindPW, baseDN, group, user)
	addr, err := srv.serve(listen)
	if err != nil {
		log.Fatalf("listen error: %v", err)
	}
	defer srv.close()
	base := fmt.Sprintf("ldap://%s", addr)
	log.Printf("LDAP dummy server listening at %s", base)
	if readyFile != "" {
		if err := os.WriteFile(readyFile, []byte(base), 0o644); err != nil {
			log.Printf("failed to write ready file: %v", err)
		}
	}
	// Block forever
	select {}
}
