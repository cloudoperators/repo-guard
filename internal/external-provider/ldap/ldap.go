// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package ldap

import (
	"context"
	"fmt"
	"strings"
	"time"

	externalprovider "github.com/cloudoperators/repo-guard/internal/external-provider"
	"github.com/cloudoperators/repo-guard/internal/metrics"
	"github.com/go-ldap/ldap/v3"
)

const (
	MEMBER_ATTRIBUTE = "member"
)

type LDAPClient struct {
	host   string
	bindDN string
	bindPW string
	baseDN string

	conn *ldap.Conn
}

func NewLDAPClient(host, bindDN, bindPW, baseDN string) (externalprovider.ExternalProvider, error) {
	l := LDAPClient{
		host:   host,
		bindDN: bindDN,
		bindPW: bindPW,
		baseDN: baseDN,
	}

	// If host already contains a scheme (e.g., ldap:// or ldaps://), use it as-is.
	// Otherwise, default to ldaps:// for production usage.
	dialURL := l.host
	if !strings.Contains(l.host, "://") {
		dialURL = "ldaps://" + l.host
	}
	conn, err := ldap.DialURL(dialURL)
	if err != nil {
		return l, err
	}

	err = conn.Bind(l.bindDN, l.bindPW)
	if err != nil {
		return l, err
	}

	l.conn = conn
	return l, nil
}

func (a *LDAPClient) reconnect() error {
	a.conn.Close() //nolint:errcheck

	dialURL := a.host
	if !strings.Contains(a.host, "://") {
		dialURL = "ldaps://" + a.host
	}
	conn, err := ldap.DialURL(dialURL)
	if err != nil {
		return err
	}

	err = conn.Bind(a.bindDN, a.bindPW)
	if err != nil {
		return err
	}

	a.conn = conn

	return nil
}

func (l LDAPClient) Users(ctx context.Context, group string) ([]string, error) {
	start := time.Now()
	req := &ldap.SearchRequest{
		BaseDN:     l.baseDN,
		Filter:     fmt.Sprintf("(&(objectCategory=group)(CN=%s))", group),
		Scope:      ldap.ScopeWholeSubtree,
		Attributes: []string{"*"},
	}

	response, err := l.conn.Search(req)

	// Try for closed connection
	if err != nil && ldap.IsErrorWithCode(err, 200) {
		if err := l.reconnect(); err != nil {
			metrics.ObserveExternalRequest("ldap_provider", "users", "error", start)
			return nil, err
		}
		response, err = l.conn.Search(req)
	}

	if err != nil {
		metrics.ObserveExternalRequest("ldap_provider", "users", "error", start)
		return nil, err
	}

	metrics.ObserveExternalRequest("ldap_provider", "users", "success", start)
	var usernames []string

	for _, responseEntry := range response.Entries {
		for _, data := range responseEntry.GetAttributeValues(MEMBER_ATTRIBUTE) {
			cn := parseCN(data)
			if cn != "" {
				usernames = append(usernames, strings.ToUpper(cn))
			}
		}
	}

	return usernames, nil

}

func parseCN(data string) string {
	for _, s := range strings.Split(data, ",") {
		data := strings.Split(s, "=")
		if len(data) == 2 && (data[0] == "cn" || data[0] == "CN") {
			return data[1]
		}
	}

	return ""
}

func (l LDAPClient) TestConnection(ctx context.Context) error {
	start := time.Now()
	// Do a lightweight search against baseDN instead of calling Users with an empty group.
	// Some LDAP servers (and our test server) reject filters like (CN=) used when group is empty.
	req := &ldap.SearchRequest{
		BaseDN:     l.baseDN,
		Scope:      ldap.ScopeBaseObject,
		Filter:     "(objectClass=*)",
		Attributes: []string{"dn"},
		SizeLimit:  1,
	}

	_, err := l.conn.Search(req)
	if err != nil && ldap.IsErrorWithCode(err, 200) {
		if err := l.reconnect(); err != nil {
			metrics.ObserveExternalRequest("ldap_provider", "test_connection", "error", start)
			return err
		}
		_, err = l.conn.Search(req)
	}

	if err != nil {
		metrics.ObserveExternalRequest("ldap_provider", "test_connection", "error", start)
		return err
	}

	metrics.ObserveExternalRequest("ldap_provider", "test_connection", "success", start)
	return nil
}
