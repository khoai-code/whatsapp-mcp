package main

import (
	"bufio"
	"os"
	"strings"
)

// configFile is the shared settings file at the repo root. The MCP server
// reads the same file, so WHATSAPP_MCP_ALLOW_WRITE and the allowlist only
// need setting once.
const configFile = "../.env"

// loadConfigFile sets variables from path that are not already set in the
// environment. A missing file is fine: everything defaults to read-only.
func loadConfigFile(path string) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		key, value, ok := parseConfigLine(scanner.Text())
		if !ok {
			continue
		}
		if _, set := os.LookupEnv(key); !set {
			os.Setenv(key, value)
		}
	}
	return scanner.Err()
}

// parseConfigLine handles KEY=VALUE, optional "export ", # comments and
// surrounding quotes.
func parseConfigLine(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	line = strings.TrimPrefix(line, "export ")
	key, value, ok := strings.Cut(line, "=")
	if !ok {
		return "", "", false
	}
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
		value = value[1 : len(value)-1]
	} else if i := strings.Index(value, " #"); i >= 0 {
		value = strings.TrimSpace(value[:i])
	}
	return key, value, key != ""
}

// interactive reports whether stdout is a terminal, i.e. someone can scan a QR.
func interactive() bool {
	info, err := os.Stdout.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
