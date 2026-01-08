package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	generichttp "github.com/cloudoperators/repo-guard/internal/external-provider/generic-http"
)

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

// loadEnvLike loads a simple KEY=VALUE env file format, ignoring lines that are empty or start with '#'.
// It trims spaces, removes optional surrounding single/double quotes, and unescapes backslashes.
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
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.Index(line, " #"); i >= 0 { // allow simple inline comments
			line = strings.TrimSpace(line[:i])
		}
		i := strings.Index(line, "=")
		if i <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:i])
		val := strings.TrimSpace(line[i+1:])
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		val = unescapeBackslashes(val)
		out[key] = val
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
	groupFlag := flag.String("group", "", "Group identifier override (overrides EMP_HTTP_GROUP_ID)")
	flag.Parse()

	// Try to load the default test env file from a few likely locations
	candidates := []string{
		"checker.env",
	}
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
		fmt.Println("Env file not found. Proceeding with existing environment variables...")
	}

	// Read configuration from env
	endpoint := getenv("EMP_HTTP_EXTERNAL_ENDPOINT")
	testURL := getenv("EMP_HTTP_EXTERNAL_TEST_CONNECTION_URL")
	username := getenv("EMP_HTTP_EXTERNAL_USERNAME")
	password := getenv("EMP_HTTP_EXTERNAL_PASSWORD")

	group := getenv("EMP_HTTP_EXTERNAL_GROUP_ID")

	if groupFlag != nil && strings.TrimSpace(*groupFlag) != "" {
		group = strings.TrimSpace(*groupFlag)
	}

	if endpoint == "" || group == "" {
		fmt.Fprintf(os.Stderr, "missing required HTTP provider configuration. endpoint=%q group=%q\n", endpoint, group)
		os.Exit(2)
	}

	// Configure the Generic HTTP client to match the controller/test setup
	// - Results are under "results"
	// - Each item has an "id" field
	// - Pagination enabled with "page" param and "total_pages" field
	cfg := &generichttp.HTTPConfig{
		ResultsField:      "results",
		IDField:           "id",
		Paginated:         true,
		TotalPagesField:   "total_pages",
		PageParam:         "page",
		TestConnectionURL: testURL,
	}

	// Build client
	client := generichttp.NewHTTPClient(endpoint, username, password, "", cfg)

	// Test connection
	ctx := context.Background()
	start := time.Now()
	if err := client.TestConnection(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "HTTP provider test connection failed: %v\n", err)
		os.Exit(3)
	}
	fmt.Printf("HTTP provider connection OK (%.0fms)\n", time.Since(start).Seconds()*1000)

	// Fetch users
	users, err := client.Users(ctx, group)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to fetch users for group %q: %v\n", group, err)
		os.Exit(4)
	}

	fmt.Printf("Group %q users (%d):\n", group, len(users))
	for _, u := range users {
		fmt.Println(" -", u)
	}
}
