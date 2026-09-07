package sso

import (
	"path/filepath"
	"testing"
	"time"
)

func TestCache_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tok.json")

	c := &Cache{LoginName: "tt_wangbang", Token: "jwt-abc", IssuedAt: time.Now()}
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	back := Load(path)
	if back.LoginName != "tt_wangbang" || back.Token != "jwt-abc" {
		t.Errorf("roundtrip lost fields: %+v", back)
	}
	if !back.Valid("tt_wangbang") {
		t.Error("fresh token should be Valid")
	}
	if back.Valid("someone-else") {
		t.Error("Valid must reject a different login name")
	}
}

func TestCache_Expiry(t *testing.T) {
	stale := &Cache{
		LoginName: "tt_wangbang",
		Token:     "jwt-old",
		IssuedAt:  time.Now().Add(-2 * tokenTTL),
	}
	if stale.Valid("tt_wangbang") {
		t.Error("token older than tokenTTL should be invalid")
	}
	empty := &Cache{LoginName: "tt_wangbang", Token: "", IssuedAt: time.Now()}
	if empty.Valid("tt_wangbang") {
		t.Error("empty token should be invalid")
	}
}

func TestLoad_Missing(t *testing.T) {
	c := Load(filepath.Join(t.TempDir(), "does-not-exist"))
	if c == nil || c.Token != "" {
		t.Errorf("missing file should give empty cache; got %+v", c)
	}
	if c.Valid("anyone") {
		t.Error("empty cache should not be Valid")
	}
}
