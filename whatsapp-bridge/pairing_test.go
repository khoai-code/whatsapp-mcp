package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestUseDataDirCreatesPrivateDir(t *testing.T) {
	chdirTemp(t)
	dir := filepath.Join(t.TempDir(), "WhatsApp MCP")
	if err := useDataDir(dir); err != nil {
		t.Fatal(err)
	}
	if m := mode(t, dir); m != 0700 {
		t.Errorf("data dir mode = %o, want 700", m)
	}
	wd, _ := os.Getwd()
	if real, _ := filepath.EvalSymlinks(dir); wd != real {
		t.Errorf("cwd = %s, want %s", wd, real)
	}
}

func TestUseDataDirEmptyIsNoop(t *testing.T) {
	chdirTemp(t)
	before, _ := os.Getwd()
	if err := useDataDir(""); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.Getwd(); after != before {
		t.Errorf("cwd changed to %s", after)
	}
}

func TestBridgePort(t *testing.T) {
	for v, want := range map[string]int{"": 8080, "8765": 8765, "abc": 8080, "0": 8080, "70000": 8080} {
		t.Setenv("WHATSAPP_MCP_BRIDGE_PORT", v)
		if got := bridgePort(); got != want {
			t.Errorf("port %q = %d, want %d", v, got, want)
		}
	}
}

// The lock is per process, so the second holder has to be another process.
func TestLockStoreRefusesSecondBridge(t *testing.T) {
	if os.Getenv("LOCK_HELPER") == "1" {
		os.Chdir(os.Getenv("LOCK_DIR"))
		ok, err := lockStore()
		if err != nil {
			os.Exit(2)
		}
		if ok {
			os.Exit(0)
		}
		os.Exit(1)
	}

	chdirTemp(t)
	if err := secureStore(); err != nil {
		t.Fatal(err)
	}
	ok, err := lockStore()
	if err != nil || !ok {
		t.Fatalf("first lock: ok=%t err=%v", ok, err)
	}
	t.Cleanup(func() { lockFile.Close(); lockFile = nil })

	wd, _ := os.Getwd()
	cmd := exec.Command(os.Args[0], "-test.run", "TestLockStoreRefusesSecondBridge")
	cmd.Env = append(os.Environ(), "LOCK_HELPER=1", "LOCK_DIR="+wd)
	err = cmd.Run()
	if exitErr, isExit := err.(*exec.ExitError); !isExit || exitErr.ExitCode() != 1 {
		t.Errorf("second process: err=%v, want exit code 1 (lock held)", err)
	}
}
