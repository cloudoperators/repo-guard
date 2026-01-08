package controller

import (
	"fmt"
	"net"
	"strings"

	ldap "github.com/nmcclain/ldap"
)

// ldapTestServer is a minimal in-memory LDAP server for tests.
// It supports simple Bind and a basic Search used by our LDAP client:
//
//	BaseDN: provided baseDN
//	Filter: (&(objectCategory=group)(CN=<group>))
//
// It returns a single entry for the matching group with a multi-valued
// attribute "member" containing DNs like "CN=<username>,CN=Users,<baseDN>".
type ldapTestServer struct {
	srv     *ldap.Server
	ln      net.Listener
	bindDN  string
	bindPW  string
	baseDN  string
	members map[string][]string // group CN -> list of usernames
}

type bindHandler struct{ dn, pw string }

func (b bindHandler) Bind(bindDN string, bindSimplePw string, _ net.Conn) (ldap.LDAPResultCode, error) {
	if (bindDN == "" && bindSimplePw == "") || (bindDN == b.dn && bindSimplePw == b.pw) {
		return ldap.LDAPResultSuccess, nil
	}
	return ldap.LDAPResultInvalidCredentials, nil
}

type searchHandler struct {
	baseDN  string
	members map[string][]string
}

func (s searchHandler) Search(_ string, req ldap.SearchRequest, _ net.Conn) (ldap.ServerSearchResult, error) {
	if !strings.EqualFold(req.BaseDN, s.baseDN) {
		return ldap.ServerSearchResult{ResultCode: ldap.LDAPResultSuccess}, nil
	}
	cn := extractCN(req.Filter)
	users := s.members[cn]
	if len(users) == 0 {
		return ldap.ServerSearchResult{ResultCode: ldap.LDAPResultSuccess}, nil
	}
	// Build entry with member attribute values as DNs containing CN=username
	vals := make([]string, 0, len(users))
	for _, u := range users {
		vals = append(vals, fmt.Sprintf("CN=%s,CN=Users,%s", u, s.baseDN))
	}
	entry := &ldap.Entry{DN: s.baseDN, Attributes: []*ldap.EntryAttribute{{Name: "member", Values: vals}}}
	return ldap.ServerSearchResult{Entries: []*ldap.Entry{entry}, ResultCode: ldap.LDAPResultSuccess}, nil
}

func extractCN(filter string) string {
	up := strings.ToUpper(filter)
	i := strings.Index(up, "(CN=")
	if i < 0 {
		return ""
	}
	rest := filter[i+4:]
	j := strings.Index(rest, ")")
	if j < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:j])
}

// newLDAPTestServer starts a plaintext LDAP server bound to 127.0.0.1:0.
// Use host() to get the address to connect to with ldap:// scheme.
func newLDAPTestServer(bindDN, bindPW, baseDN, group string, usernames []string) *ldapTestServer {
	s := &ldapTestServer{
		srv:     ldap.NewServer(),
		bindDN:  bindDN,
		bindPW:  bindPW,
		baseDN:  baseDN,
		members: map[string][]string{group: usernames},
	}

	// Bind handler and Search handler
	s.srv.BindFunc("", bindHandler{dn: s.bindDN, pw: s.bindPW})
	s.srv.SearchFunc("", searchHandler{baseDN: s.baseDN, members: s.members})

	// Listen on localhost ephemeral port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		// In tests, panic is acceptable to fail fast
		panic(err)
	}
	s.ln = ln
	go func() { _ = s.srv.Serve(ln) }()
	return s
}

func (s *ldapTestServer) Close() {
	if s == nil {
		return
	}
	if s.srv != nil {
		s.srv.Quit <- true
	}
	if s.ln != nil {
		_ = s.ln.Close()
	}
}

// host returns host:port suitable for building ldap:// URLs
func (s *ldapTestServer) host() string {
	if s == nil || s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}
