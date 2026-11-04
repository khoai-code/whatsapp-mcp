package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseConfigLine(t *testing.T) {
	cases := []struct {
		line, key, value string
		ok               bool
	}{
		{"WHATSAPP_MCP_ALLOW_WRITE=true", "WHATSAPP_MCP_ALLOW_WRITE", "true", true},
		{"export A=1", "A", "1", true},
		{`B="84 111, 84 222"`, "B", "84 111, 84 222", true},
		{"C='x'", "C", "x", true},
		{"D=false # off for now", "D", "false", true},
		{"  # comment", "", "", false},
		{"", "", "", false},
		{"no equals", "", "", false},
	}
	for _, c := range cases {
		k, v, ok := parseConfigLine(c.line)
		if k != c.key || v != c.value || ok != c.ok {
			t.Errorf("%q: got (%q, %q, %t)", c.line, k, v, ok)
		}
	}
}

func TestLoadConfigFileDoesNotOverrideEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	os.WriteFile(path, []byte("WHATSAPP_MCP_ALLOW_WRITE=true\nWHATSAPP_MCP_SEND_ALLOWLIST=84111\n"), 0600)
	t.Setenv("WHATSAPP_MCP_ALLOW_WRITE", "false")
	os.Unsetenv("WHATSAPP_MCP_SEND_ALLOWLIST")
	t.Cleanup(func() { os.Unsetenv("WHATSAPP_MCP_SEND_ALLOWLIST") })
	if err := loadConfigFile(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("WHATSAPP_MCP_ALLOW_WRITE"); got != "false" {
		t.Errorf("env overridden: %q", got)
	}
	if got := os.Getenv("WHATSAPP_MCP_SEND_ALLOWLIST"); got != "84111" {
		t.Errorf("allowlist = %q, want 84111", got)
	}
}

func TestMissingConfigFileIsFine(t *testing.T) {
	if err := loadConfigFile(filepath.Join(t.TempDir(), "nope")); err != nil {
		t.Errorf("err = %v", err)
	}
}
