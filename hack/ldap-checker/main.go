package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	ldapprovider "github.com/cloudoperators/repo-guard/internal/external-provider/ldap"
)

// loadEnvLike loads a simple KEY=VALUE env file format, ignoring lines that:
// - are empty
// - start with '#'
// It trims spaces, and removes optional surrounding single or double quotes.
// unescapeBackslashes treats backslashes as escape characters in a simple way:
// "\\" -> "\\", and "\x" -> "x" for any other character x.
// This allows writing passwords like A\&\;X% in env files without changing the real value.
func unescapeBackslashes(in string) string {
	var b strings.Builder
	b.Grow(len(in))
	escaped := false
	for _, r := range in {
		if escaped {
			// keep the rune as-is, but collapse the preceding backslash
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		b.WriteRune(r)
	}
	// trailing solitary backslash is dropped (interpreted as escape of nothing)
	return b.String()
}

func loadEnvLike(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck

	out := make(map[string]string)
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") { // comment or empty
			continue
		}
		// allow inline comments after some spacing by splitting on # if not quoted
		// but keep it simple: only strip trailing comment when there's a space before '#'
		if i := strings.Index(line, " #"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		// split on first '='
		i := strings.Index(line, "=")
		if i <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:i])
		val := strings.TrimSpace(line[i+1:])
		// remove optional surrounding quotes
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		// interpret backslashes as escapes (so A\& becomes A&)
		val = unescapeBackslashes(val)
		out[key] = val
		// also export to process env for convenience
		_ = os.Setenv(key, val)
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func getenv(keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func main() {
	// Flags
	groupFlag := flag.String("group", "", "LDAP group name override (overrides LDAP_GROUP_PROVIDER_GROUP_NAME)")
	flag.Parse()

	// Default location of the test env in this repo
	candidates := []string{"checker.env"}
	var loaded bool
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			if _, err := loadEnvLike(p); err != nil {
				fmt.Fprintf(os.Stderr, "failed to load env file %s: %v\n", p, err)
				os.Exit(1)
			}
			fmt.Printf("Loaded environment from %s\n", p)
			loaded = true
			break
		}
	}
	if !loaded {
		// Proceed with OS env only
		fmt.Println("Env file not found. Proceeding with existing environment variables...")
	}

	host := getenv("LDAP_GROUP_PROVIDER_HOST")
	baseDN := getenv("LDAP_GROUP_PROVIDER_BASE_DN")
	bindDN := getenv("LDAP_GROUP_PROVIDER_BIND_DN")
	bindPW := getenv("LDAP_GROUP_PROVIDER_BIND_PW")
	group := getenv("LDAP_GROUP_PROVIDER_GROUP_NAME")

	// Override from flag when provided
	if groupFlag != nil && strings.TrimSpace(*groupFlag) != "" {
		group = strings.TrimSpace(*groupFlag)
	}

	if host == "" || baseDN == "" || bindDN == "" || bindPW == "" || group == "" {
		fmt.Fprintf(os.Stderr, "missing required LDAP environment variables. Have: host=%q baseDN=%q bindDN=%q bindPW(len)=%d group=%q\n", host, baseDN, bindDN, len(bindPW), group)
		os.Exit(2)
	}

	// Create LDAP client
	client, err := ldapprovider.NewLDAPClient(host, bindDN, bindPW, baseDN)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create LDAP client: %v\n", err)
		os.Exit(3)
	}

	// Test connection
	ctx := context.Background()
	start := time.Now()
	if err := client.TestConnection(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "LDAP test connection failed: %v\n", err)
		os.Exit(4)
	}
	fmt.Printf("LDAP connection OK (%.0fms)\n", time.Since(start).Seconds()*1000)

	// Query members of the given group (similar to members provider)
	users, err := client.Users(ctx, group)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to fetch users for group %q: %v\n", group, err)
		os.Exit(5)
	}

	fmt.Printf("Group %q members (%d):\n", group, len(users))
	for _, u := range users {
		fmt.Println(" -", u)
	}
}
