package config

import (
	"bufio"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
	Port                   string
	DatabaseURL            string
	DocsUIEnabled          bool
	TrustedProxies         []net.IPNet
	AllowedOrigins         []string
	SupabaseURL            string
	SupabaseJWTAudience    string
	SupabaseServiceRoleKey string
	CheckInSpacing         time.Duration
	Commit                 string
}

func Load() (Config, error) {
	return load(os.LookupEnv, ".")
}

func load(lookup func(string) (string, bool), dir string) (Config, error) {
	fileEnv, err := readEnvFile(dir)
	if err != nil {
		return Config{}, err
	}
	value := func(key string) string {
		if envValue, ok := lookup(key); ok {
			return envValue
		}
		return fileEnv[key]
	}

	port, err := parsePort(value("PORT"))
	if err != nil {
		return Config{}, err
	}
	databaseURL, err := parseDatabaseURL(value("DATABASE_URL"))
	if err != nil {
		return Config{}, err
	}
	docs, err := parseDocs(value("DOCS_UI_ENABLED"))
	if err != nil {
		return Config{}, err
	}
	proxies, err := parseTrustedProxies(value("TRUSTED_PROXIES"))
	if err != nil {
		return Config{}, err
	}
	origins, err := parseAllowedOrigins(value("ALLOWED_ORIGINS"))
	if err != nil {
		return Config{}, err
	}
	supabaseURL, err := parseSupabaseURL(value("SUPABASE_URL"))
	if err != nil {
		return Config{}, err
	}
	audience := strings.TrimSpace(value("SUPABASE_JWT_AUDIENCE"))
	if audience == "" {
		return Config{}, fmt.Errorf("SUPABASE_JWT_AUDIENCE is required")
	}
	serviceRoleKey := strings.TrimSpace(value("SUPABASE_SERVICE_ROLE_KEY"))
	if serviceRoleKey == "" {
		return Config{}, fmt.Errorf("SUPABASE_SERVICE_ROLE_KEY is required")
	}
	spacing, err := parseCheckInSpacing(value("CHECKIN_SPACING"))
	if err != nil {
		return Config{}, err
	}
	return Config{
		Port:                   port,
		DatabaseURL:            databaseURL,
		DocsUIEnabled:          docs,
		TrustedProxies:         proxies,
		AllowedOrigins:         origins,
		SupabaseURL:            supabaseURL,
		SupabaseJWTAudience:    audience,
		SupabaseServiceRoleKey: serviceRoleKey,
		CheckInSpacing:         spacing,
		Commit:                 value("RENDER_GIT_COMMIT"),
	}, nil
}

func parseCheckInSpacing(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Hour, nil
	}
	spacing, err := time.ParseDuration(value)
	if err != nil || spacing <= 0 {
		return 0, fmt.Errorf("CHECKIN_SPACING must be a positive duration")
	}
	return spacing, nil
}

func parseSupabaseURL(value string) (string, error) {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	if value == "" {
		return "", fmt.Errorf("SUPABASE_URL is required")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("SUPABASE_URL must be an absolute HTTPS URL or a loopback HTTP URL")
	}
	if parsed.Scheme == "https" {
		return value, nil
	}
	if parsed.Scheme == "http" && loopbackHost(parsed.Hostname()) {
		return value, nil
	}
	return "", fmt.Errorf("SUPABASE_URL must be an absolute HTTPS URL or a loopback HTTP URL")
}

func loopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func parsePort(value string) (string, error) {
	if value == "" {
		return "8080", nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("PORT must be an integer from 1 through 65535")
	}
	return strconv.Itoa(n), nil
}

func parseDatabaseURL(value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("DATABASE_URL is required")
	}
	if _, err := pgxpool.ParseConfig(value); err != nil {
		return "", fmt.Errorf("DATABASE_URL must be a valid PostgreSQL URL")
	}
	return value, nil
}

func parseDocs(value string) (bool, error) {
	switch value {
	case "", "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("DOCS_UI_ENABLED must be true, false, or empty")
	}
}

func parseTrustedProxies(value string) ([]net.IPNet, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	nets := make([]net.IPNet, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("TRUSTED_PROXIES must be a comma-separated list of CIDRs")
		}
		_, network, err := net.ParseCIDR(part)
		if err != nil {
			return nil, fmt.Errorf("TRUSTED_PROXIES must be a comma-separated list of CIDRs")
		}
		nets = append(nets, *network)
	}
	return nets, nil
}

// parseAllowedOrigins reads a comma-separated list of browser origins that may
// call this API cross-origin. Empty or unset disables CORS entirely: no
// Access-Control-Allow-Origin header is ever sent, and native app clients are
// unaffected since they never send an Origin header.
func parseAllowedOrigins(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	origins := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		parsed, err := url.Parse(part)
		if part == "" || err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return nil, fmt.Errorf("ALLOWED_ORIGINS must be a comma-separated list of origins")
		}
		origins = append(origins, part)
	}
	return origins, nil
}

func readEnvFile(dir string) (map[string]string, error) {
	path := filepath.Join(dir, ".env")
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("read %s: %v", path, err)
	}
	defer file.Close()

	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		key, value, skip, err := parseEnvLine(scanner.Text())
		if err != nil {
			return nil, fmt.Errorf(".env line %d is malformed", lineNo)
		}
		if skip {
			continue
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %v", path, err)
	}
	return values, nil
}

func parseEnvLine(line string) (key, value string, skip bool, err error) {
	if strings.TrimSpace(line) == "" {
		return "", "", true, nil
	}
	trimmed := strings.TrimLeft(line, " \t")
	if strings.HasPrefix(trimmed, "#") {
		return "", "", true, nil
	}
	trimmed = strings.TrimPrefix(trimmed, "export ")
	key, value, ok := strings.Cut(trimmed, "=")
	if !ok {
		return "", "", false, fmt.Errorf("malformed")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", false, fmt.Errorf("malformed")
	}
	return key, value, false, nil
}
