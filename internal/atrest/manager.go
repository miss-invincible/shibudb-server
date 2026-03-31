package atrest

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/crypto/scrypt"
)

const (
	EnvMasterKey          = "SHIBUDB_MASTER_KEY"
	EnvMasterPassphrase   = "SHIBUDB_MASTER_PASSPHRASE"
	fileMagic             = "SDBENC1"
	manifestFileName      = "encryption.manifest.json"
	defaultScryptN        = 1 << 15
	defaultScryptR        = 8
	defaultScryptP        = 1
	defaultDerivedKeySize = 32
)

type Config struct {
	Enabled       bool
	DataDir       string
	Passphrase    string
	MasterKey     string
	MasterKeyFile string
}

type Manifest struct {
	Version int    `json:"version"`
	KDF     string `json:"kdf"`
	SaltB64 string `json:"salt_b64"`
}

type Manager struct {
	enabled bool
	aead    cipher.AEAD
}

var (
	runtimeMu      sync.RWMutex
	runtimeManager *Manager
)

func NewManager(cfg Config) (*Manager, error) {
	if !cfg.Enabled {
		return &Manager{enabled: false}, nil
	}

	key, err := resolveKey(cfg)
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("master key must be 32 bytes, got %d", len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Manager{enabled: true, aead: aead}, nil
}

func SetRuntimeManager(m *Manager) {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	runtimeManager = m
}

func RuntimeManager() *Manager {
	runtimeMu.RLock()
	defer runtimeMu.RUnlock()
	return runtimeManager
}

func (m *Manager) Enabled() bool {
	return m != nil && m.enabled
}

func (m *Manager) Seal(plaintext []byte, aad string) ([]byte, error) {
	if !m.Enabled() {
		out := make([]byte, len(plaintext))
		copy(out, plaintext)
		return out, nil
	}
	nonce := make([]byte, m.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ciphertext := m.aead.Seal(nil, nonce, plaintext, []byte(aad))
	out := make([]byte, 0, len(fileMagic)+1+1+len(nonce)+len(ciphertext))
	out = append(out, []byte(fileMagic)...)
	out = append(out, byte(1))
	out = append(out, byte(len(nonce)))
	out = append(out, nonce...)
	out = append(out, ciphertext...)
	return out, nil
}

func (m *Manager) Open(payload []byte, aad string) ([]byte, error) {
	if len(payload) < len(fileMagic)+2 {
		return nil, errors.New("encrypted payload too short")
	}
	if string(payload[:len(fileMagic)]) != fileMagic {
		return nil, errors.New("payload is not encrypted with shibudb envelope")
	}
	version := payload[len(fileMagic)]
	if version != 1 {
		return nil, fmt.Errorf("unsupported encryption envelope version: %d", version)
	}
	nonceSize := int(payload[len(fileMagic)+1])
	start := len(fileMagic) + 2
	if len(payload) < start+nonceSize {
		return nil, errors.New("invalid encrypted payload")
	}
	nonce := payload[start : start+nonceSize]
	ciphertext := payload[start+nonceSize:]
	return m.aead.Open(nil, nonce, ciphertext, []byte(aad))
}

func (m *Manager) ReadFile(path, aad string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !m.Enabled() {
		return data, nil
	}
	if !isEncryptedPayload(data) {
		return nil, fmt.Errorf("plaintext file found while encryption-at-rest is enabled: %s", path)
	}
	return m.Open(data, aad)
}

func (m *Manager) WriteFile(path string, plaintext []byte, perm os.FileMode, aad string) error {
	data := plaintext
	var err error
	if m.Enabled() {
		data, err = m.Seal(plaintext, aad)
		if err != nil {
			return err
		}
	}
	return writeFileAtomic(path, data, perm)
}

func IsEncryptedFile(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return isEncryptedPayload(data)
}

func isEncryptedPayload(data []byte) bool {
	return len(data) >= len(fileMagic) && string(data[:len(fileMagic)]) == fileMagic
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func resolveKey(cfg Config) ([]byte, error) {
	if strings.TrimSpace(cfg.MasterKey) != "" {
		return parseRawKey(cfg.MasterKey)
	}
	if envKey := strings.TrimSpace(os.Getenv(EnvMasterKey)); envKey != "" {
		return parseRawKey(envKey)
	}
	if strings.TrimSpace(cfg.MasterKeyFile) != "" {
		b, err := os.ReadFile(cfg.MasterKeyFile)
		if err != nil {
			return nil, err
		}
		return parseRawKey(strings.TrimSpace(string(b)))
	}
	pass := strings.TrimSpace(cfg.Passphrase)
	if pass == "" {
		pass = strings.TrimSpace(os.Getenv(EnvMasterPassphrase))
	}
	if pass == "" {
		return nil, errors.New("no encryption key source configured: provide master key env/file or passphrase")
	}
	return deriveFromPassphrase(cfg.DataDir, pass)
}

func parseRawKey(v string) ([]byte, error) {
	if decoded, err := hex.DecodeString(v); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	if decoded, err := base64.StdEncoding.DecodeString(v); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	if len(v) == 32 {
		return []byte(v), nil
	}
	return nil, errors.New("raw master key must be 32-byte plain, 64-char hex, or base64")
}

func deriveFromPassphrase(dataDir, pass string) ([]byte, error) {
	manifestPath := filepath.Join(dataDir, manifestFileName)
	manifest, err := loadOrCreateManifest(manifestPath)
	if err != nil {
		return nil, err
	}
	salt, err := base64.StdEncoding.DecodeString(manifest.SaltB64)
	if err != nil {
		return nil, err
	}
	return scrypt.Key([]byte(pass), salt, defaultScryptN, defaultScryptR, defaultScryptP, defaultDerivedKeySize)
}

func loadOrCreateManifest(path string) (*Manifest, error) {
	if _, err := os.Stat(path); err == nil {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var m Manifest
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, err
		}
		return &m, nil
	}
	salt := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, err
	}
	m := Manifest{
		Version: 1,
		KDF:     "scrypt",
		SaltB64: base64.StdEncoding.EncodeToString(salt),
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := writeFileAtomic(path, b, 0600); err != nil {
		return nil, err
	}
	return &m, nil
}
