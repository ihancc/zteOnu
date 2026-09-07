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

// BindInfoData is the successful-case payload of scene/security/forward when
// called with compId=317: a single-account bindinfo/带宽/地市/订单状态 view.
// This is the shape the reference project (查限速批量.js) targets.
type BindInfoData struct {
	BindInfo    string `json:"bindinfo"`
	UserName    string `json:"userName"`
	UserBand    string `json:"userBand"`
	UserNode    string `json:"userNode"`
	OrderStatus string `json:"orderStatus"`
	CreateTime  string `json:"createTime"`
	UpdateTime  string `json:"updateTime"`
}

// BindInfoResponse envelopes BindInfoData.
type BindInfoResponse struct {
	Code int           `json:"code"`
	Msg  string        `json:"msg"`
	Data *BindInfoData `json:"data"`
}

// DetailData is the per-ONU detail payload returned when compId=1230 is used
// with a single customer account. Field names come from the observed wire
// format; the ones the UI displays are onuPasswd / orderStatus / OperState /
// spos, but keeping the rest here means callers get the full context.
type DetailData struct {
	OrderStatus      string `json:"orderStatus"`
	OrderStatusValue string `json:"orderStatusValue"`
	AdminState       string `json:"AdminState"`
	OperState        string `json:"OperState"`
	OnuPasswd        string `json:"onuPasswd"`
	OnuSN            string `json:"onuSn"`
	OnuName          string `json:"onuName"`
	Spos             string `json:"spos"`
	OltName          string `json:"olt"`
	OltIP            string `json:"oltIp"`
	OltPort          string `json:"oltPort"`
	OltVendor        string `json:"oltVendor"`
	UserBand         string `json:"userBand"`
	AccessMethods    string `json:"accessMethodsName"`
	CityName         string `json:"cityName"`
	CustomerName     string `json:"customerName"`
	CustomersAccount string `json:"customersAccount"`
	OnlineTime       string `json:"onlineTime"`
	BindInfo         string `json:"bindinfo"`
	StdAddress       string `json:"stdAddress"`
	LogTime          string `json:"logTime"`        // 最后一次认证时间 (yyyyMMddHHmmss)
	BmsOperateType   string `json:"bmsOperateType"` // 最后一次认证结果 (认证成功 / …)
	RxPower          string `json:"RxPower"`
	TxPower          string `json:"TxPower"`
	PRxPower         string `json:"PRxPower"`
	PTxPower         string `json:"PTxPower"`
}

// DetailResponse envelopes a DetailData object.
type DetailResponse struct {
	Code int         `json:"code"`
	Msg  string      `json:"msg"`
	Data *DetailData `json:"data"`
}

// DeviceItem is one ONU under the target account's PON port. The 施工 App /
// scene/security/forward endpoint returns a list of these when compId=1310 -
// i.e. all neighbors on the same PON as the queried broadband account.
type DeviceItem struct {
	ONUID            string `json:"ONUID"`
	OperState        string `json:"OperState"`
	AuthType         string `json:"AUTHTYPE"`
	AuthInfo         string `json:"AUTHINFO"`
	Password         string `json:"password"`
	LastOffTime      string `json:"LASTOFFTIME"`
	CustomersAccount string `json:"customersAccount"`
}

// ForwardResponse envelopes the DeviceItem list. code=200 success, code=500
// means "account not found", code=401 the JWT expired.
type ForwardResponse struct {
	Code int          `json:"code"`
	Msg  string       `json:"msg"`
	Data []DeviceItem `json:"data"`
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

// forwardRequest is the JSON body encrypted into the request. Field set and
// defaults match the ciphertext observed on the wire:
//
//	{account, appFlag:"T", compId:"1310", isApp:"N", ponName:"", sessionId:""}
//
// (查限速批量.js used compId=317 without ponName; the live 施工 App uses this
// wider shape - the ponName filter selects devices under a specific PON when
// non-empty.)
type forwardRequest struct {
	Account   string `json:"account"`
	AppFlag   string `json:"appFlag"`
	CompID    string `json:"compId"`
	IsApp     string `json:"isApp"`
	PonName   string `json:"ponName"`
	SessionID string `json:"sessionId"`
}

// QueryOne looks up one broadband account with an empty PON filter (returns
// all devices bound to the account). If the response is 401 and TokenProvider
// is set, it refreshes the token once and retries.
func (c *ForwardClient) QueryOne(account string) (*ForwardResponse, error) {
	return c.QueryOneWithPon(account, "")
}

// QueryOneWithPon looks up one broadband account, restricted to a specific PON
// name when ponName is non-empty. 401 → refresh + retry same as QueryOne.
func (c *ForwardClient) QueryOneWithPon(account, ponName string) (*ForwardResponse, error) {
	resp, err := c.doQuery(account, ponName)
	if err != nil {
		return nil, err
	}
	if resp.Code != 401 || c.TokenProvider == nil {
		return resp, nil
	}
	newTok, err := c.TokenProvider()
	if err != nil {
		return resp, fmt.Errorf("token 过期，刷新失败：%w", err)
	}
	c.Token = newTok
	return c.doQuery(account, ponName)
}

// QueryBindInfo is the single-account bindinfo view (compId=317, no ponName).
// On 401 it refreshes the token once, same as QueryOne.
func (c *ForwardClient) QueryBindInfo(account string) (*BindInfoResponse, error) {
	resp, err := c.doBindInfo(account)
	if err != nil {
		return nil, err
	}
	if resp.Code != 401 || c.TokenProvider == nil {
		return resp, nil
	}
	newTok, err := c.TokenProvider()
	if err != nil {
		return resp, fmt.Errorf("token 过期，刷新失败：%w", err)
	}
	c.Token = newTok
	return c.doBindInfo(account)
}

// QueryDetail hits scene/security/forward with compId=1230 to pull the full
// per-ONU detail (onuPasswd / orderStatus / OperState / spos / …) for the
// given customer account. On 401 it refreshes the token once and retries.
func (c *ForwardClient) QueryDetail(account string) (*DetailResponse, error) {
	resp, err := c.doDetail(account)
	if err != nil {
		return nil, err
	}
	if resp.Code != 401 || c.TokenProvider == nil {
		return resp, nil
	}
	newTok, err := c.TokenProvider()
	if err != nil {
		return resp, fmt.Errorf("token 过期，刷新失败：%w", err)
	}
	c.Token = newTok
	return c.doDetail(account)
}

func (c *ForwardClient) doDetail(account string) (*DetailResponse, error) {
	body, err := json.Marshal(bindInfoRequest{
		Account: account, AppFlag: "T", CompID: "1230", IsApp: "N",
	})
	if err != nil {
		return nil, err
	}
	raw, err := c.postForward(body)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return &DetailResponse{Code: 401, Msg: "HTTP 401"}, nil
	}
	var r DetailResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("解析详情响应失败：%w（body：%s）", err, truncateStr(string(raw), 200))
	}
	return &r, nil
}

// bindInfoRequest is the compId=317/1230 flavor of the request body: no
// ponName field. Reused as-is by QueryBindInfo (compId=317) and QueryDetail
// (compId=1230), which differ only in the CompID field.
type bindInfoRequest struct {
	Account   string `json:"account"`
	AppFlag   string `json:"appFlag"`
	CompID    string `json:"compId"`
	IsApp     string `json:"isApp"`
	SessionID string `json:"sessionId"`
}

func (c *ForwardClient) doBindInfo(account string) (*BindInfoResponse, error) {
	body, err := json.Marshal(bindInfoRequest{
		Account: account, AppFlag: "T", CompID: "317", IsApp: "N",
	})
	if err != nil {
		return nil, err
	}
	raw, err := c.postForward(body)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return &BindInfoResponse{Code: 401, Msg: "HTTP 401"}, nil
	}
	var r BindInfoResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("解析响应失败：%w（body：%s）", err, truncateStr(string(raw), 200))
	}
	return &r, nil
}

// postForward encrypts + POSTs the given JSON body and returns the raw
// response bytes. A nil slice means HTTP 401 was seen (token expired).
func (c *ForwardClient) postForward(body []byte) ([]byte, error) {
	enc, err := c.rsaEncrypt(string(body))
	if err != nil {
		return nil, fmt.Errorf("RSA 加密请求失败：%w", err)
	}
	req, err := http.NewRequest("POST", forwardURL, strings.NewReader(enc))
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
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, nil // caller treats nil-body as 401
	}
	if resp.StatusCode != http.StatusOK {
		return raw, fmt.Errorf("HTTP %d：%s", resp.StatusCode, truncateStr(string(raw), 200))
	}
	return raw, nil
}

// doQuery is a single request without retry.
func (c *ForwardClient) doQuery(account, ponName string) (*ForwardResponse, error) {
	body, err := json.Marshal(forwardRequest{
		Account: account, AppFlag: "T", CompID: "1310", IsApp: "N", PonName: ponName,
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
