package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const MasterKeyEnv = "INTELLY_MASTER_KEY"

// LoadMasterKey returns the 32-byte key from INTELLY_MASTER_KEY (base64) or
// from dir/master.key, creating that file on first use.
func LoadMasterKey(dir string) ([]byte, error) {
	if v := os.Getenv(MasterKeyEnv); v != "" {
		return decodeKey(v, MasterKeyEnv)
	}
	path := filepath.Join(dir, "master.key")
	b, err := os.ReadFile(path)
	if err == nil {
		return decodeKey(strings.TrimSpace(string(b)), path)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	key := make([]byte, 32)
	rand.Read(key)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	if _, err := f.WriteString(base64.StdEncoding.EncodeToString(key) + "\n"); err != nil {
		f.Close()
		return nil, err
	}
	return key, f.Close()
}

func decodeKey(v, source string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(v)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("%s must hold a base64-encoded 32-byte key", source)
	}
	return key, nil
}

func newToken(prefix string) string {
	raw := make([]byte, 32)
	rand.Read(raw)
	return prefix + base64.RawURLEncoding.EncodeToString(raw)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
