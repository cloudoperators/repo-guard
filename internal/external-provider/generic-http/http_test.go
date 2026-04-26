// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package generic_http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestOAuthFlow(t *testing.T) {
	tokenRequests := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/token" {
			tokenRequests++
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))

			err := r.ParseForm()
			assert.NoError(t, err)
			assert.Equal(t, "password", r.FormValue("grant_type"))
			assert.Equal(t, "user", r.FormValue("username"))
			assert.Equal(t, "pass", r.FormValue("password"))
			assert.Equal(t, "client", r.FormValue("client_id"))
			assert.Equal(t, "secret", r.FormValue("client_secret"))

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "test-token",
				"expires_in":   2, // 2 seconds, will be cached for 1s
			})
			return
		}

		if r.URL.Path == "/api/users" {
			assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]string{"user1", "user2"})
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	client := NewHTTPClient(ts.URL+"/api/users", "user", "pass", "", "client", "secret", nil)

	// First call should trigger token request
	users, err := client.Users(context.Background(), "group1")
	assert.NoError(t, err)
	assert.Equal(t, []string{"user1", "user2"}, users)
	assert.Equal(t, 1, tokenRequests)

	// Second call should use cached token
	_, err = client.Users(context.Background(), "group1")
	assert.NoError(t, err)
	assert.Equal(t, 1, tokenRequests)

	// Wait for token to expire (cached for half of 2s = 1s)
	time.Sleep(1500 * time.Millisecond)

	// Third call should trigger new token request
	_, err = client.Users(context.Background(), "group1")
	assert.NoError(t, err)
	assert.Equal(t, 2, tokenRequests, "Token requests should be 2 after expiration")
}

func TestBasicAuthFallback(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		assert.True(t, ok)
		assert.Equal(t, "user", u)
		assert.Equal(t, "pass", p)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]string{"user1"})
	}))
	defer ts.Close()

	client := NewHTTPClient(ts.URL, "user", "pass", "", "", "", nil)
	users, err := client.Users(context.Background(), "group1")
	assert.NoError(t, err)
	assert.Equal(t, []string{"user1"}, users)
}
