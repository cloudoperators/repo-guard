package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	ghusers "github.com/cloudoperators/repo-guard/internal/github"

	"github.com/palantir/go-githubapp/githubapp"
)

// unescapeBackslashes treats backslashes as escape characters in a simple way:
// "\\" -> "\\", and "\x" -> "x" for any other character x.
// This allows writing secrets with special chars in env files without changing the real value.
func unescapeBackslashes(in string) string {
	var b strings.Builder
	b.Grow(len(in))
	escaped := false
	for _, r := range in {
		if escaped {
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
		// allow simple inline comments only when not inside quotes; handled below
		i := strings.Index(line, "=")
		if i <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:i])
		val := strings.TrimSpace(line[i+1:])

		// If the value starts with a quote but doesn't close on this line,
		// keep scanning subsequent lines until the matching quote is found.
		if len(val) >= 1 && (val[0] == '\'' || val[0] == '"') {
			q := val[0]
			// Check if it also ends with the same quote on this line
			if len(val) >= 2 && val[len(val)-1] == q {
				// Single-line quoted value: strip quotes
				val = val[1 : len(val)-1]
			} else {
				// Multi-line quoted value: accumulate lines until closing quote
				// Start with the remainder of the current line after the opening quote
				collected := val[1:]
				for {
					if !s.Scan() {
						// Unterminated quoted value
						return nil, fmt.Errorf("unterminated quoted value for key %s", key)
					}
					nextLine := s.Text()
					// Remove only the trailing newline behavior; keep spaces as-is.
					// Detect closing quote possibly after some trailing spaces
					trimmedRight := strings.TrimRight(nextLine, " \t")
					if len(trimmedRight) > 0 && trimmedRight[len(trimmedRight)-1] == q {
						// Remove the trailing quote, but keep original spacing before it
						// Find last index of q in the original line considering trailing spaces
						// Use the position in trimmedRight to slice original line accordingly
						cut := len(trimmedRight) - 1
						// Compose the portion without the quote, then append any removed right spaces
						withoutQuote := nextLine[:cut]
						collected += "\n" + withoutQuote
						break
					}
					collected += "\n" + nextLine
				}
				val = collected
			}
		} else {
			// Not quoted: allow simple inline comments after a space
			if c := strings.Index(val, " #"); c >= 0 {
				val = strings.TrimSpace(val[:c])
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
	// Flags to optionally override env
	orgFlag := flag.String("org", "", "GitHub organization login (overrides EMAIL_CHECK_ORG)")
	uidFlag := flag.String("uid", "", "GitHub user ID (numeric) (overrides EMAIL_CHECK_UID)")
	domainFlag := flag.String("domain", "", "Email domain to check (overrides EMAIL_CHECK_DOMAIN)")
	instFlag := flag.Int64("installation", 0, "GitHub App installation ID (overrides GITHUB_INSTALLATION_ID)")
	flag.Parse()

	// Try to load a local env file next to the checker
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

	// GitHub App/client configuration (align with internal controller expectations)
	webURL := getenv("GITHUB_WEB_URL")
	v3URL := getenv("GITHUB_V3_API_URL")
	integrationIDStr := getenv("GITHUB_INTEGRATION_ID")
	privateKey := getenv("GITHUB_APP_PRIVATE_KEY", "GITHUB_PRIVATE_KEY")
	clientUA := getenv("GITHUB_CLIENT_USER_AGENT")

	var integrationID int64
	if integrationIDStr != "" {
		if v, err := strconv.ParseInt(integrationIDStr, 10, 64); err == nil {
			integrationID = v
		} else {
			fmt.Fprintf(os.Stderr, "invalid GITHUB_INTEGRATION_ID: %v\n", err)
			os.Exit(2)
		}
	}

	// Installation/org/user/domain inputs
	org := getenv("EMAIL_CHECK_ORG")
	uid := getenv("EMAIL_CHECK_UID")
	domain := getenv("EMAIL_CHECK_DOMAIN")
	instIDStr := getenv("GITHUB_INSTALLATION_ID")

	if orgFlag != nil && strings.TrimSpace(*orgFlag) != "" {
		org = strings.TrimSpace(*orgFlag)
	}
	if uidFlag != nil && strings.TrimSpace(*uidFlag) != "" {
		uid = strings.TrimSpace(*uidFlag)
	}
	if domainFlag != nil && strings.TrimSpace(*domainFlag) != "" {
		domain = strings.TrimSpace(*domainFlag)
	}
	var installationID int64
	if instFlag != nil && *instFlag > 0 {
		installationID = *instFlag
	} else if instIDStr != "" {
		v, err := strconv.ParseInt(instIDStr, 10, 64)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid GITHUB_INSTALLATION_ID: %v\n", err)
			os.Exit(2)
		}
		installationID = v
	}

	// Validate required config
	if integrationID == 0 || privateKey == "" {
		fmt.Fprintln(os.Stderr, "missing GitHub App configuration: GITHUB_INTEGRATION_ID and GITHUB_APP_PRIVATE_KEY are required")
		os.Exit(3)
	}
	if installationID == 0 || org == "" || uid == "" || domain == "" {
		fmt.Fprintf(os.Stderr, "missing required inputs: installationID=%d org=%q uid=%q domain=%q\n", installationID, org, uid, domain)
		os.Exit(4)
	}

	// Build a githubapp client creator similar to the controller
	cfg := githubapp.Config{
		WebURL:   webURL,
		V3APIURL: v3URL,
	}
	cfg.App.IntegrationID = integrationID
	cfg.App.PrivateKey = privateKey
	if clientUA == "" {
		clientUA = "email-domain-checker"
	}
	cc, err := githubapp.NewDefaultCachingClientCreator(
		cfg,
		githubapp.WithClientUserAgent(clientUA),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create github client: %v\n", err)
		os.Exit(5)
	}

	// Create UsersProvider and run the check
	usersProvider, err := ghusers.NewUsersProvider(cc, installationID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create users provider: %v\n", err)
		os.Exit(6)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	start := time.Now()
	ok, checkErr := usersProvider.HasVerifiedEmailDomainForGithubUID(ctx, org, uid, domain)
	took := time.Since(start)
	if checkErr != nil {
		fmt.Fprintf(os.Stderr, "email domain check failed: %v\n", checkErr)
		os.Exit(7)
	}

	fmt.Printf("Has verified email with domain %q for uid %s in org %s: %t (%.0fms)\n", domain, uid, org, ok, took.Seconds()*1000)
}
