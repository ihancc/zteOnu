package query

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Public key extracted from the 小工具/家宽综调 APK, reused verbatim from
// 查限速批量.js. Not a secret — anyone with the APK has it.
const forwardPubKey = `-----BEGIN PUBLIC KEY-----
MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQCFntBT2rjxnfCLnGil4Rcz5YyB
CVgPsztDlXl4MNcI9IVhM7f0cBL5fRkRMaTwpe2QpCsoyBpao7HtONprqhDZjmbv
gFXI1D0vNGCyH3+mysRqj81mAXipw0/EOolKluFWN/vfhaBno/QVEvLOtx86BtsA
CcrU40WsiEf/8ksqZQIDAQAB
-----END PUBLIC KEY-----`

const forwardURL = "http://211.138.20.196:31094/prod-api/scene/security/forward"

// ForwardData is the successful-case payload of scene/security/forward. The
// endpoint returns plaintext JSON (rsaDecrypt in 查限速批量.js is dead code -
// see 施工App鉴权机制分析.html §8), so decoding is straight json.Unmarshal.
type ForwardData struct {
	BindInfo    string `json:"bindinfo"`
	UserName    string `json:"userName"`
	UserBand    string `json:"userBand"`
	UserNode    string `json:"userNode"`
	OrderStatus string `json:"orderStatus"`
	CreateTime  string `json:"createTime"`
	UpdateTime  string `json:"updateTime"`
}

// ForwardResponse envelopes ForwardData; code=200 is success, code=500 means
// "account not found", code=401 means the JWT expired.
type ForwardResponse struct {
	Code int          `json:"code"`
	Msg  string       `json:"msg"`
	Data *ForwardData `json:"data"`
}

// ForwardClient wraps the /scene/security/forward endpoint and injects a JWT.
// TokenProvider is called on 401 to fetch a fresh token; when nil, a 401 is
// returned to the caller unchanged.
type ForwardClient struct {
	Token         string
	HTTP          *http.Client
	TokenProvider func() (string, error)

	pub *rsa.PublicKey
}

// NewForwardClient parses the public key once and returns a client with a 30s
// default timeout.
func NewForwardClient(token string) (*ForwardClient, error) {
	block, _ := pem.Decode([]byte(forwardPubKey))
	if block == nil {
		return nil, errors.New("解析 RSA 公钥失败")
	}
	pubAny, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("解析 RSA 公钥失败：%w", err)
	}
	pub, ok := pubAny.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("公钥不是 RSA 类型")
	}
	return &ForwardClient{
		Token: token,
		HTTP:  &http.Client{Timeout: 30 * time.Second},
		pub:   pub,
	}, nil
}

// rsaEncrypt returns URL-safe Base64 of PKCS#1 v1.5 encrypted plaintext,
// matching 查限速批量.js rsaEncrypt (+ → -, / → _, drop trailing =).
func (c *ForwardClient) rsaEncrypt(plain string) (string, error) {
	ct, err := rsa.EncryptPKCS1v15(rand.Reader, c.pub, []byte(plain))
	if err != nil {
		return "", err
	}
	s := base64.StdEncoding.EncodeToString(ct)
	s = strings.NewReplacer("+", "-", "/", "_").Replace(s)
	s = strings.TrimRight(s, "=")
	return s, nil
}

// forwardRequest is the JSON body encrypted into the request. Defaults come
// from 查限速批量.js: appFlag=T, compId=317, isApp=N.
type forwardRequest struct {
	Account   string `json:"account"`
	AppFlag   string `json:"appFlag"`
	CompID    string `json:"compId"`
	IsApp     string `json:"isApp"`
	SessionID string `json:"sessionId"`
}

// QueryOne looks up one broadband account. If the response is 401 and
// TokenProvider is set, it refreshes the token once and retries.
func (c *ForwardClient) QueryOne(account string) (*ForwardResponse, error) {
	resp, err := c.doQuery(account)
	if err != nil {
		return nil, err
	}
	if resp.Code != 401 || c.TokenProvider == nil {
		return resp, nil
	}
	// Token expired: refresh once and retry.
	newTok, err := c.TokenProvider()
	if err != nil {
		return resp, fmt.Errorf("token 过期，刷新失败：%w", err)
	}
	c.Token = newTok
	return c.doQuery(account)
}

// doQuery is a single request without retry.
func (c *ForwardClient) doQuery(account string) (*ForwardResponse, error) {
	body, err := json.Marshal(forwardRequest{
		Account: account, AppFlag: "T", CompID: "317", IsApp: "N",
	})
	if err != nil {
		return nil, err
	}
	encBody, err := c.rsaEncrypt(string(body))
	if err != nil {
		return nil, fmt.Errorf("RSA 加密请求失败：%w", err)
	}
	req, err := http.NewRequest("POST", forwardURL, strings.NewReader(encBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("appVersion", "1.0.57")
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	req.Header.Set("User-Agent", "okhttp/4.9.3")
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("Connection", "Keep-Alive")

	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	httpResp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()
	raw, _ := io.ReadAll(httpResp.Body)
	if httpResp.StatusCode == http.StatusUnauthorized {
		// 401 at HTTP layer as well as body-level 401 both mean token expired.
		return &ForwardResponse{Code: 401, Msg: "HTTP 401"}, nil
	}
	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d：%s", httpResp.StatusCode, truncateStr(string(raw), 200))
	}
	var r ForwardResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("解析响应失败：%w（body：%s）", err, truncateStr(string(raw), 200))
	}
	return &r, nil
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
