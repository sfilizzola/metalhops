package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
	Port          string
	DatabaseURL   string
	DocsUIEnabled bool
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
	return Config{Port: port, DatabaseURL: databaseURL, DocsUIEnabled: docs}, nil
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
