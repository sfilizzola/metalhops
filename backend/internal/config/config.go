package config

import (
	"fmt"
	"os"
	"strconv"

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
	if dir == "" {
		return nil, fmt.Errorf("read .env")
	}
	return map[string]string{}, nil
}
