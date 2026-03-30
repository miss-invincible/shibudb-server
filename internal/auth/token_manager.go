package auth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type TokenRecord struct {
	ID        string    `json:"id"`
	Hash      string    `json:"hash"`
	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
}

type TokenManager struct {
	filePath string
	lock     sync.RWMutex
	tokens   map[string]TokenRecord
}

func NewTokenManager(filePath string) (*TokenManager, error) {
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create token directory: %w", err)
	}

	tm := &TokenManager{
		filePath: filePath,
		tokens:   make(map[string]TokenRecord),
	}

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		if err := tm.saveLocked(); err != nil {
			return nil, err
		}
		return tm, nil
	}

	if err := tm.loadLocked(); err != nil {
		return nil, err
	}
	return tm, nil
}

func (t *TokenManager) loadLocked() error {
	t.lock.Lock()
	defer t.lock.Unlock()
	return t.loadNoLock()
}

func (t *TokenManager) loadNoLock() error {
	data, err := os.ReadFile(t.filePath)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		t.tokens = make(map[string]TokenRecord)
		return nil
	}
	loaded := make(map[string]TokenRecord)
	if err := json.Unmarshal(data, &loaded); err != nil {
		return err
	}
	t.tokens = loaded
	return nil
}

func (t *TokenManager) saveLocked() error {
	t.lock.Lock()
	defer t.lock.Unlock()
	return t.saveNoLock()
}

func (t *TokenManager) saveNoLock() error {
	data, err := json.MarshalIndent(t.tokens, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(t.filePath, data, 0644)
}

func generateTokenID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func generateRawToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (t *TokenManager) GenerateToken(createdBy string) (string, string, error) {
	tokenID, err := generateTokenID()
	if err != nil {
		return "", "", err
	}
	rawToken, err := generateRawToken()
	if err != nil {
		return "", "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(rawToken), bcrypt.DefaultCost)
	if err != nil {
		return "", "", err
	}

	t.lock.Lock()
	defer t.lock.Unlock()

	t.tokens[tokenID] = TokenRecord{
		ID:        tokenID,
		Hash:      string(hash),
		CreatedAt: time.Now().UTC(),
		CreatedBy: createdBy,
	}

	if err := t.saveNoLock(); err != nil {
		return "", "", err
	}
	return tokenID, rawToken, nil
}

func (t *TokenManager) ListTokens() []TokenRecord {
	t.lock.RLock()
	defer t.lock.RUnlock()

	result := make([]TokenRecord, 0, len(t.tokens))
	for _, rec := range t.tokens {
		result = append(result, TokenRecord{
			ID:        rec.ID,
			Hash:      "",
			CreatedAt: rec.CreatedAt,
			CreatedBy: rec.CreatedBy,
		})
	}
	return result
}

func (t *TokenManager) DeleteToken(tokenID string) error {
	t.lock.Lock()
	defer t.lock.Unlock()

	if _, ok := t.tokens[tokenID]; !ok {
		return errors.New("token not found")
	}
	delete(t.tokens, tokenID)
	return t.saveNoLock()
}

func (t *TokenManager) ValidateToken(token string) bool {
	t.lock.Lock()
	defer t.lock.Unlock()

	// Tokens can be generated/deleted by separate CLI processes.
	// Reload before validation so a long-running server sees latest tokens.
	if err := t.loadNoLock(); err != nil {
		return false
	}

	for _, rec := range t.tokens {
		if bcrypt.CompareHashAndPassword([]byte(rec.Hash), []byte(token)) == nil {
			return true
		}
	}
	return false
}