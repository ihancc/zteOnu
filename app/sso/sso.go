// Package sso implements the CMCC 施工 App / 家宽综调 SSO exchange: turn a
// work-number (工号) into an access_token (JWT) via the /auth/appsso endpoint.
//
// Flow (per 施工App鉴权机制分析.html §4):
//
//	loginUser = URL-encode( Base64( AES-128-ECB(pkcs7, "1234567890!@#$%^",
//	                                            loginName + "$" + timestamp) ) )
//	GET /prod-api/auth/appsso?back=test&key=zwapp&time=<ms>&loginUser=<loginUser>
//	→ { "access_token": "<JWT>", ... }
package sso

import (
	"bytes"
	"crypto/aes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// The 16-byte AES key hard-coded in the 施工 App's JS bundle
// (ZongDiaoAESEcrySecret). Not a secret in any real sense.
const zongDiaoAESKey = "1234567890!@#$%^"

// Default SSO base URL (production 施工 App backend).
const defaultBaseURL = "http://211.138.20.196:31094/prod-api"

// aesEncryptECB is AES-128-ECB with PKCS7 padding, matching FlagSecure.AESEncrypt
// in the 施工 App. plaintext is UTF-8; the output is Base64 (standard, not URL-safe).
func aesEncryptECB(plaintext, key string) (string, error) {
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return "", err
	}
	bs := block.BlockSize()
	padded := pkcs7Pad([]byte(plaintext), bs)
	out := make([]byte, len(padded))
	for i := 0; i < len(padded); i += bs {
		block.Encrypt(out[i:i+bs], padded[i:i+bs])
	}
	return base64.StdEncoding.EncodeToString(out), nil
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	pad := blockSize - (len(data) % blockSize)
	return append(data, bytes.Repeat([]byte{byte(pad)}, pad)...)
}

// Client executes the SSO exchange with an optional custom HTTP client for tests.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// New returns a client with sensible defaults (production SSO URL, 15s timeout).
func New() *Client {
	return &Client{
		BaseURL: defaultBaseURL,
		HTTP:    &http.Client{Timeout: 15 * time.Second},
	}
}

// appssoResponse is the /auth/appsso response envelope. Only access_token is
// used; the rest is ignored on purpose so the shape can evolve server-side.
type appssoResponse struct {
	Code        int    `json:"code"`
	Msg         string `json:"msg"`
	AccessToken string `json:"access_token"`
	Data        struct {
		AccessToken string `json:"access_token"`
	} `json:"data"`
}

// Login exchanges a work-number for an access_token. The token typically lives
// for a couple hours; callers should cache it and refresh on 401.
func (c *Client) Login(loginName string) (string, error) {
	loginName = strings.TrimSpace(loginName)
	if loginName == "" {
		return "", errors.New("工号为空")
	}
	ts := time.Now().UnixMilli()
	cipherText, err := aesEncryptECB(fmt.Sprintf("%s$%d", loginName, ts), zongDiaoAESKey)
	if err != nil {
		return "", fmt.Errorf("AES 加密工号失败：%w", err)
	}

	u := fmt.Sprintf("%s/auth/appsso?back=test&key=zwapp&time=%d&loginUser=%s",
		c.BaseURL, ts, url.QueryEscape(cipherText))
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("User-Agent", "okhttp/4.9.3")

	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("SSO 请求失败：%w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("SSO HTTP %d：%s", resp.StatusCode, truncate(string(body), 200))
	}

	var r appssoResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("SSO 响应解析失败：%w（body：%s）", err, truncate(string(body), 200))
	}
	// Some deployments wrap the token as data.access_token; accept either.
	tok := r.AccessToken
	if tok == "" {
		tok = r.Data.AccessToken
	}
	if tok == "" {
		return "", fmt.Errorf("SSO 响应无 access_token：%s", truncate(string(body), 200))
	}
	return tok, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
