package sso

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// tokenTTL is how long we treat a cached JWT as valid before forcing a refresh.
// The 施工 App tokens live ~2h in practice; 90 min is a conservative floor that
// keeps us well clear of the boundary and avoids mid-request 401s.
const tokenTTL = 90 * time.Minute

// Cache is a work-number → JWT cache backed by a JSON file, so a fresh login is
// only needed after the token has aged past tokenTTL or a 401 arrives.
type Cache struct {
	LoginName string    `json:"login_name"`
	Token     string    `json:"token"`
	IssuedAt  time.Time `json:"issued_at"`
}

// Load reads the cache from path. A missing file is not an error - it returns
// an empty Cache.
func Load(path string) *Cache {
	b, err := os.ReadFile(path)
	if err != nil {
		return &Cache{}
	}
	var c Cache
	if json.Unmarshal(b, &c) != nil {
		return &Cache{}
	}
	return &c
}

// Save writes the cache to path atomically enough for a single-user desktop
// tool (write + rename).
func (c *Cache) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Valid reports whether the cached token is fresh enough to reuse for
// loginName. A blank token, mismatched name, or an issue time older than
// tokenTTL all count as invalid.
func (c *Cache) Valid(loginName string) bool {
	if c == nil || c.Token == "" || c.LoginName != loginName {
		return false
	}
	return time.Since(c.IssuedAt) < tokenTTL
}

// EnsureToken returns a fresh JWT for loginName, reusing the cached one when
// possible; otherwise it hits SSO and writes the new token back to path.
func EnsureToken(loginName, path string, sso *Client) (string, error) {
	cache := Load(path)
	if cache.Valid(loginName) {
		return cache.Token, nil
	}
	tok, err := sso.Login(loginName)
	if err != nil {
		return "", err
	}
	fresh := &Cache{LoginName: loginName, Token: tok, IssuedAt: time.Now()}
	_ = fresh.Save(path)
	return tok, nil
}

// Invalidate clears the token file so the next EnsureToken call triggers a
// fresh SSO exchange (used after a 401).
func Invalidate(path string) {
	_ = os.Remove(path)
}
