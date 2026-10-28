package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// chdirTemp runs the test inside a fresh directory so store/ is isolated.
func chdirTemp(t *testing.T) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })
}

func mode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func TestSecureStoreFixesPermissions(t *testing.T) {
	chdirTemp(t)
	if err := os.Mkdir(storeDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("store/messages.db", nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := secureStore(); err != nil {
		t.Fatal(err)
	}
	if m := mode(t, storeDir); m != 0700 {
		t.Errorf("store mode = %o, want 700", m)
	}
	if m := mode(t, "store/messages.db"); m != 0600 {
		t.Errorf("db mode = %o, want 600", m)
	}
}

func TestTokenCreatedOnceWithPrivateMode(t *testing.T) {
	chdirTemp(t)
	if err := secureStore(); err != nil {
		t.Fatal(err)
	}
	first, err := loadOrCreateToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 64 {
		t.Errorf("token length = %d, want 64", len(first))
	}
	if m := mode(t, tokenFile); m != 0600 {
		t.Errorf("token mode = %o, want 600", m)
	}
	second, err := loadOrCreateToken()
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Error("token changed between starts")
	}
}

func TestRequireAuth(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	h := requireAuth("secret-token", ok)

	cases := []struct {
		name   string
		header string
		want   int
	}{
		{"missing", "", http.StatusUnauthorized},
		{"wrong", "Bearer nope", http.StatusUnauthorized},
		{"no scheme", "secret-token", http.StatusUnauthorized},
		{"valid", "Bearer secret-token", http.StatusOK},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodPost, "/api/send", nil)
		if c.header != "" {
			req.Header.Set("Authorization", c.header)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("%s: status = %d, want %d", c.name, rec.Code, c.want)
		}
	}
}

func TestListenHostIsLoopback(t *testing.T) {
	if listenHost != "127.0.0.1" {
		t.Errorf("listenHost = %q, want 127.0.0.1", listenHost)
	}
}
