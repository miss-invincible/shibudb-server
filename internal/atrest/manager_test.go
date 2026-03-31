package atrest

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestManagerRoundTrip(t *testing.T) {
	t.Setenv(EnvMasterKey, hex.EncodeToString([]byte("12345678901234567890123456789012")))

	mgr, err := NewManager(Config{Enabled: true, DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	ciphertext, err := mgr.Seal([]byte("hello"), "aad")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	plain, err := mgr.Open(ciphertext, "aad")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if string(plain) != "hello" {
		t.Fatalf("unexpected plaintext: %q", string(plain))
	}
}

func TestReadWriteFileEncrypted(t *testing.T) {
	t.Setenv(EnvMasterKey, hex.EncodeToString([]byte("12345678901234567890123456789012")))
	tmpDir := t.TempDir()

	mgr, err := NewManager(Config{Enabled: true, DataDir: tmpDir})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	path := filepath.Join(tmpDir, "payload.bin")
	if err := mgr.WriteFile(path, []byte("secret"), 0600, "file-aad"); err != nil {
		t.Fatalf("write file: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read raw file: %v", err)
	}
	if string(raw) == "secret" {
		t.Fatal("expected encrypted payload, got plaintext")
	}
	opened, err := mgr.ReadFile(path, "file-aad")
	if err != nil {
		t.Fatalf("read file via manager: %v", err)
	}
	if string(opened) != "secret" {
		t.Fatalf("unexpected decrypted value: %q", string(opened))
	}
}
