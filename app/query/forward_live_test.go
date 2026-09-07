//go:build live

package query

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/septrum101/zteOnu/app/sso"
)

// TestLive_ForwardQuery1230 hits scene/security/forward with compId=1230 to
// discover what fields the per-ONU-detail flavor of the endpoint returns.
func TestLive_ForwardQuery1230(t *testing.T) {
	tok, err := sso.New().Login("tt_wangbang")
	if err != nil {
		t.Fatalf("SSO login: %v", err)
	}
	fc, err := NewForwardClient(tok)
	if err != nil {
		t.Fatal(err)
	}
	// Use one of the neighbor accounts we saw in the earlier compId=1310 test.
	reqBody, _ := json.Marshal(map[string]string{
		// online account from the earlier 1310 dump - hopefully returns more fields.
		"account": "15838372919", "appFlag": "T", "compId": "1230",
		"isApp": "N", "sessionId": "",
	})
	ct, _ := rsa.EncryptPKCS1v15(rand.Reader, fc.pub, reqBody)
	enc := base64.StdEncoding.EncodeToString(ct)
	enc = strings.NewReplacer("+", "-", "/", "_").Replace(enc)
	enc = strings.TrimRight(enc, "=")
	req, _ := http.NewRequest("POST", forwardURL, strings.NewReader(enc))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("appVersion", "1.0.57")
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	req.Header.Set("User-Agent", "okhttp/4.9.3")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Println("== raw response ==")
	fmt.Println(string(body))
	var pretty any
	if err := json.Unmarshal(body, &pretty); err == nil {
		if b, err := json.MarshalIndent(pretty, "", "  "); err == nil {
			fmt.Println("\n== pretty ==")
			fmt.Println(string(b))
		}
	}
}

// TestLive_ForwardQuery hits the real 施工 App backend and prints the RAW
// JSON body so we can see every field the endpoint returns. Run with:
//
//	go test -tags=live ./app/query/ -run TestLive_ForwardQuery -v
func TestLive_ForwardQuery(t *testing.T) {
	tok, err := sso.New().Login("tt_wangbang")
	if err != nil {
		t.Fatalf("SSO login: %v", err)
	}
	t.Logf("token length: %d", len(tok))

	fc, err := NewForwardClient(tok)
	if err != nil {
		t.Fatal(err)
	}
	// Build the same encrypted body but do the HTTP call inline so we can
	// print the untouched response bytes (our decoded ForwardResponse would
	// otherwise drop unknown fields).
	reqBody, _ := json.Marshal(forwardRequest{
		Account: "15838372919", AppFlag: "T", CompID: "1310", IsApp: "N",
	})
	ct, _ := rsa.EncryptPKCS1v15(rand.Reader, fc.pub, reqBody)
	enc := base64.StdEncoding.EncodeToString(ct)
	enc = strings.NewReplacer("+", "-", "/", "_").Replace(enc)
	enc = strings.TrimRight(enc, "=")

	req, _ := http.NewRequest("POST", forwardURL, strings.NewReader(enc))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("appVersion", "1.0.57")
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	req.Header.Set("User-Agent", "okhttp/4.9.3")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Println("== raw response ==")
	fmt.Println(string(body))

	// Pretty-print for readability.
	var pretty any
	if err := json.Unmarshal(body, &pretty); err == nil {
		if b, err := json.MarshalIndent(pretty, "", "  "); err == nil {
			fmt.Println("\n== pretty ==")
			fmt.Println(string(b))
		}
	}
}
