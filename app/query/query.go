// Package query calls the CMCC 施工端 (worker-side) APIs to look up broadband
// account provisioning info: OLT/POS port, ONU equipment name, online state,
// and administrator password. Endpoints and headers mirror the reference
// project (查询同pos.js) so the same combine-token works.
package query

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Base host used by 施工端 for all worker APIs.
const baseURL = "http://211.138.20.196:31071/shigong/workorder/api/app/taskOrder"

// PonItem is one row of the queryPonInfo response's data array. Field names
// (JSON tags) match the reference project's parser 1:1 so the same tokens can
// hit either implementation and get the same fields.
type PonItem struct {
	CustomersAccount string `json:"customersAccount"`
	NewState         string `json:"newState"`
	OltName          string `json:"oltName"`
	PosPortName      string `json:"posPortName"`
	Passwd           string `json:"passwd"`
	OnuEquipName     string `json:"onuEquipName"`
}

// PonResponse is the full envelope of queryPonInfo. Status 0 = success, 3 =
// "in progress" or "account not found" (message tells which).
type PonResponse struct {
	Status  int       `json:"status"`
	Message string    `json:"message"`
	Data    []PonItem `json:"data"`
}

// OnuResponse is queryOnuInfo. The device-side fields vary by account; keep it
// as a raw object so the UI can render whatever the API returns.
type OnuResponse struct {
	Status  int             `json:"status"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// AdminPasswdResponse is getCMCCAdminPassword. Data is a string on success.
type AdminPasswdResponse struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
	Data    string `json:"data"`
}

// Client calls the three CMCC APIs with the given combine-token. Zero timeout
// falls back to a 30s default.
type Client struct {
	Token   string
	HTTP    *http.Client
	Timeout time.Duration
}

// New builds a Client with a combined-token and a per-request timeout.
func New(token string) *Client {
	return &Client{Token: token, Timeout: 30 * time.Second}
}

// commonHeaders replicates the mobile app fingerprint the reference JS sends.
// The server checks combine-token but also seems to care about a couple of
// these; sending the full set matches what the JS does verbatim.
func (c *Client) commonHeaders(req *http.Request) {
	req.Header.Set("accept", "application/json, text/plain, */*")
	req.Header.Set("platform", "android")
	req.Header.Set("version", "1.1.127")
	req.Header.Set("systemversion", "android 12")
	req.Header.Set("devicebrand", "OPPO")
	req.Header.Set("devicename", "OPPO A93s 5G")
	req.Header.Set("combine-token", c.Token)
	req.Header.Set("Connection", "Keep-Alive")
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("User-Agent", "okhttp/4.8.1")
}

// httpClient returns the client to use for one request, honoring c.Timeout.
func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &http.Client{Timeout: timeout}
}

// getJSON does a GET, decodes the JSON body into out, and returns the raw body
// for diagnostics when decoding fails.
func (c *Client) getJSON(endpoint, account string, out any) ([]byte, error) {
	if strings.TrimSpace(c.Token) == "" {
		return nil, fmt.Errorf("combine-token 未填写")
	}
	if strings.TrimSpace(account) == "" {
		return nil, fmt.Errorf("账号为空")
	}
	u := fmt.Sprintf("%s/%s?account=%s", baseURL, endpoint, url.QueryEscape(account))
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	c.commonHeaders(req)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return body, fmt.Errorf("HTTP 401：combine-token 已失效")
	}
	if resp.StatusCode != http.StatusOK {
		return body, fmt.Errorf("HTTP %d：%s", resp.StatusCode, resp.Status)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return body, fmt.Errorf("解析 JSON 失败：%w", err)
	}
	return body, nil
}

// QueryPonInfo calls queryPonInfo for the given account.
func (c *Client) QueryPonInfo(account string) (*PonResponse, []byte, error) {
	var r PonResponse
	body, err := c.getJSON("queryPonInfo", account, &r)
	return &r, body, err
}

// QueryOnuInfo calls queryOnuInfo for the given account.
func (c *Client) QueryOnuInfo(account string) (*OnuResponse, []byte, error) {
	var r OnuResponse
	body, err := c.getJSON("queryOnuInfo", account, &r)
	return &r, body, err
}

// GetAdminPassword calls getCMCCAdminPassword for the given account.
func (c *Client) GetAdminPassword(account string) (*AdminPasswdResponse, []byte, error) {
	var r AdminPasswdResponse
	body, err := c.getJSON("getCMCCAdminPassword", account, &r)
	return &r, body, err
}

// LookupResult bundles the three per-account API responses so the UI can show
// them together. All three fields are independent - one call failing does not
// hide the results of the other two; each has its own Err.
type LookupResult struct {
	Account string

	Pon    *PonResponse
	PonErr error

	Onu    *OnuResponse
	OnuErr error

	Passwd    *AdminPasswdResponse
	PasswdErr error
}

// LookupAll runs all three queries for the account, returning whatever succeeds.
// Callers can inspect the per-field errors; a single-field failure does not
// short-circuit the others.
func (c *Client) LookupAll(account string) *LookupResult {
	r := &LookupResult{Account: account}
	r.Pon, _, r.PonErr = c.QueryPonInfo(account)
	r.Onu, _, r.OnuErr = c.QueryOnuInfo(account)
	r.Passwd, _, r.PasswdErr = c.GetAdminPassword(account)
	return r
}
