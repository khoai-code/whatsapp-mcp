package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	storeDir  = "store"
	tokenFile = "store/bridge.token"
	// Loopback only: the bridge must never be reachable from the network.
	listenHost = "127.0.0.1"
)

// secureStore makes sure the store directory and everything in it is private
// to the current user. New files inherit this through the umask; existing
// installs are fixed up with explicit chmods.
func secureStore() error {
	syscall.Umask(0077)
	if err := os.MkdirAll(storeDir, 0700); err != nil {
		return fmt.Errorf("failed to create store directory: %v", err)
	}
	return filepath.Walk(storeDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return os.Chmod(path, 0700)
		}
		return os.Chmod(path, 0600)
	})
}

// loadOrCreateToken returns the bridge API token, generating it on first start.
func loadOrCreateToken() (string, error) {
	data, err := os.ReadFile(tokenFile)
	if err == nil {
		token := strings.TrimSpace(string(data))
		if len(token) >= 32 {
			return token, os.Chmod(tokenFile, 0600)
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("failed to read token file: %v", err)
	}

	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("failed to generate token: %v", err)
	}
	token := hex.EncodeToString(buf)
	if err := os.WriteFile(tokenFile, []byte(token+"\n"), 0600); err != nil {
		return "", fmt.Errorf("failed to write token file: %v", err)
	}
	return token, nil
}

// requireAuth rejects any request without a matching bearer token.
func requireAuth(token string, next http.Handler) http.Handler {
	expected := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get("Authorization"))
		if subtle.ConstantTimeCompare(got, expected) != 1 {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
