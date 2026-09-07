package sso

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAESEncryptFingerprint matches the worked example in
// 施工App鉴权机制分析.html §3.6: AES-128-ECB / PKCS7 of the plain 工号
// "tt_wangbang" under key "1234567890!@#$%^" is "MmG3tlmYa2qiGeoA+9/31g==".
// If this ever changes, we've drifted from the app's real crypto and the
// server will reject the login.
func TestAESEncryptFingerprint(t *testing.T) {
	got, err := aesEncryptECB("tt_wangbang", zongDiaoAESKey)
	if err != nil {
		t.Fatal(err)
	}
	if got != "MmG3tlmYa2qiGeoA+9/31g==" {
		t.Errorf("AES(tt_wangbang) = %q, want MmG3tlmYa2qiGeoA+9/31g==", got)
	}
}

// TestLoginRoundTrip validates the request URL layout and picks up the
// access_token from either the flat or `data.` envelope shape.
func TestLoginRoundTrip(t *testing.T) {
	var gotPath, gotQuery string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200, "access_token": "jwt-abc",
		})
	}))
	defer ts.Close()

	c := &Client{BaseURL: ts.URL, HTTP: ts.Client()}
	tok, err := c.Login("tt_wangbang")
	if err != nil {
		t.Fatal(err)
	}
	if tok != "jwt-abc" {
		t.Errorf("token = %q, want jwt-abc", tok)
	}
	if gotPath != "/auth/appsso" {
		t.Errorf("path = %q, want /auth/appsso", gotPath)
	}
	for _, want := range []string{"back=test", "key=zwapp", "time=", "loginUser="} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("query %q missing %q", gotQuery, want)
		}
	}
}

// TestLoginNestedToken accepts the alternative envelope shape data.access_token.
func TestLoginNestedToken(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200, "data": map[string]string{"access_token": "jwt-nested"},
		})
	}))
	defer ts.Close()
	c := &Client{BaseURL: ts.URL, HTTP: ts.Client()}
	tok, err := c.Login("staff_x")
	if err != nil {
		t.Fatal(err)
	}
	if tok != "jwt-nested" {
		t.Errorf("token = %q, want jwt-nested", tok)
	}
}
