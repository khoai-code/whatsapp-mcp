package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newPolicy(t *testing.T, allowWrite, allowlist, mediaDir string) *writePolicy {
	t.Helper()
	chdirTemp(t)
	if err := secureStore(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WHATSAPP_MCP_ALLOW_WRITE", allowWrite)
	t.Setenv("WHATSAPP_MCP_SEND_ALLOWLIST", allowlist)
	t.Setenv("WHATSAPP_MCP_MEDIA_DIR", mediaDir)
	p, err := loadWritePolicy()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestWriteFlagOff(t *testing.T) {
	for _, v := range []string{"", "false", "1", "yes"} {
		p := newPolicy(t, v, "", "")
		if err := p.checkWrite("84123456789"); err != errWriteDisabled {
			t.Errorf("ALLOW_WRITE=%q: err = %v, want errWriteDisabled", v, err)
		}
	}
}

func TestWriteFlagOnNoAllowlist(t *testing.T) {
	p := newPolicy(t, "true", "", "")
	if err := p.checkWrite("84123456789"); err != nil {
		t.Errorf("err = %v, want nil", err)
	}
	if err := p.checkWrite("120363000000000000@g.us"); err != nil {
		t.Errorf("group: err = %v, want nil", err)
	}
}

func TestAllowlistHit(t *testing.T) {
	p := newPolicy(t, "TRUE", "+84 123-456-789, 120363000000000000@g.us", "")
	for _, r := range []string{
		"84123456789",
		"84123456789@s.whatsapp.net",
		"84123456789:12@s.whatsapp.net",
		"120363000000000000@g.us",
	} {
		if err := p.checkWrite(r); err != nil {
			t.Errorf("%s: err = %v, want nil", r, err)
		}
	}
}

func TestAllowlistMiss(t *testing.T) {
	p := newPolicy(t, "true", "84123456789", "")
	for _, r := range []string{"84999999999", "120363000000000000@g.us", "84123456789@g.us"} {
		err := p.checkWrite(r)
		if err == nil || !strings.Contains(err.Error(), "not on WHATSAPP_MCP_SEND_ALLOWLIST") {
			t.Errorf("%s: err = %v, want allowlist rejection", r, err)
		}
	}
}

func TestInvalidAllowlistEntryFailsStartup(t *testing.T) {
	chdirTemp(t)
	if err := secureStore(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WHATSAPP_MCP_SEND_ALLOWLIST", "bob")
	if _, err := loadWritePolicy(); err == nil {
		t.Error("expected error for invalid allowlist entry")
	}
}

func TestMediaPathRejectsStore(t *testing.T) {
	p := newPolicy(t, "true", "", "")
	if err := os.WriteFile("store/whatsapp.db", []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := p.checkMediaPath("store/whatsapp.db"); err == nil {
		t.Error("store file accepted, want rejection")
	}
	// A symlink pointing into the store must not get around the check.
	if err := os.Symlink("store/whatsapp.db", "innocent.jpg"); err != nil {
		t.Fatal(err)
	}
	if err := p.checkMediaPath("innocent.jpg"); err == nil {
		t.Error("symlink into store accepted, want rejection")
	}
	if err := os.WriteFile("photo.jpg", []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := p.checkMediaPath("photo.jpg"); err != nil {
		t.Errorf("normal file: err = %v, want nil", err)
	}
}

func TestMediaDirRestriction(t *testing.T) {
	chdirTemp(t)
	outbox := filepath.Join(t.TempDir(), "outbox")
	if err := os.Mkdir(outbox, 0700); err != nil {
		t.Fatal(err)
	}
	if err := secureStore(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WHATSAPP_MCP_ALLOW_WRITE", "true")
	t.Setenv("WHATSAPP_MCP_MEDIA_DIR", outbox)
	p, err := loadWritePolicy()
	if err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(outbox, "a.jpg")
	os.WriteFile(inside, []byte("x"), 0600)
	os.WriteFile("b.jpg", []byte("x"), 0600)
	if err := p.checkMediaPath(inside); err != nil {
		t.Errorf("inside: err = %v, want nil", err)
	}
	if err := p.checkMediaPath("b.jpg"); err == nil {
		t.Error("outside media dir accepted, want rejection")
	}
}

func TestWriteForbiddenIs403(t *testing.T) {
	rec := httptest.NewRecorder()
	writeForbidden(rec, errWriteDisabled)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "WHATSAPP_MCP_ALLOW_WRITE") {
		t.Errorf("body %q does not explain how to enable writes", rec.Body.String())
	}
}
