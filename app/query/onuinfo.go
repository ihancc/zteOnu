package query

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// The queryOnuInfo endpoint lives on the 施工 App's main API port (:31071)
// and is authenticated with the `combine-token` header - the same token the
// face-login flow returns. This is different from scene/security/forward,
// which uses :31094 and a Bearer JWT.
const onuInfoURL = "http://211.138.20.196:31071/shigong/workorder/api/app/taskOrder/queryOnuInfo"

// OnuWorkingConditions is the "working conditions" section of an OnuInfoData.
type OnuWorkingConditions struct {
	EmsOperState     string `json:"emsOperState"`     // ONU 运行状态
	EmsLastUpTime    string `json:"emsLastUpTime"`    // 最后上线时间
	EmsLastDownTime  string `json:"emsLastDownTime"`  // 最后离线时间
	EmsLastDownCause string `json:"emsLastDownCause"` // 最后离线原因
	EmsRxPower       string `json:"emsRxPower"`       // 光衰
}

// OnuZgData is the "zg data" section of an OnuInfoData.
type OnuZgData struct {
	BandwidthRate    string `json:"bandwidthRate"`    // 速率
	CellName         string `json:"cellName"`         // 小区
	OltPortName      string `json:"oltPortName"`      // PON 口名称
	CustomersAddress string `json:"customersAddress"` // 地址
	PosPortName      string `json:"posPortName"`      // 分光器名称
	HighPosEquipName string `json:"highPosEquipName"` // 上层 pos
}

// OnuComplaintAndOttInfo is the "complaint and OTT info" section.
type OnuComplaintAndOttInfo struct {
	RadiusAuthTag      string `json:"radiusAuthTag"`      // 认证标签
	RadiusAuthReason   string `json:"radiusAuthReason"`   // 认证原因
	RadiusState        string `json:"radiusState"`        // 账号状态 (authenticated?)
	RadiusOnlineStatus string `json:"radiusOnlineStatus"` // 在线状态
	RadiusErrorMsg     string `json:"radiusErrorMsg"`     // 验证失败原因
	RadiusAuthTime     string `json:"radiusAuthTime"`     // 认证时间
}

// OnuInfoData is the successful-case payload of queryOnuInfo.
type OnuInfoData struct {
	WorkingConditions   *OnuWorkingConditions   `json:"workingConditions"`
	ZgData              *OnuZgData              `json:"zgData"`
	ComplaintAndOttInfo *OnuComplaintAndOttInfo `json:"complaintAndOttInfo"`
}

// OnuInfoResponse envelopes OnuInfoData. status: 0=success, 3=没有宽带,
// other=接口异常; message may carry a Chinese description on non-zero status.
type OnuInfoResponse struct {
	Status  int          `json:"status"`
	Message string       `json:"message"`
	Data    *OnuInfoData `json:"data"`
}

// OnuInfoClient calls queryOnuInfo authenticated by a face-login combineToken.
// TokenExpired is called when the server returns HTTP 401 or any status the
// caller considers an auth failure; the returned bool tells the retry loop
// whether a fresh token was installed on the client (true) or the failure
// should propagate (false).
type OnuInfoClient struct {
	CombineToken string
	HTTP         *http.Client
	// TokenExpired lets the caller refresh the combineToken (e.g. re-run the
	// face-login flow) when the server returns 401. If nil, 401s propagate.
	TokenExpired func() (string, error)
}

// NewOnuInfoClient wraps a combineToken in a client with a 30s timeout.
func NewOnuInfoClient(combineToken string) *OnuInfoClient {
	return &OnuInfoClient{
		CombineToken: combineToken,
		HTTP:         &http.Client{Timeout: 30 * time.Second},
	}
}

// Query looks up one account. On HTTP 401 it calls TokenExpired once (if set)
// and retries; any other error propagates.
func (c *OnuInfoClient) Query(account string) (*OnuInfoResponse, error) {
	resp, is401, err := c.doQuery(account)
	if err != nil {
		return nil, err
	}
	if !is401 || c.TokenExpired == nil {
		return resp, nil
	}
	newTok, err := c.TokenExpired()
	if err != nil {
		return nil, fmt.Errorf("token 已失效且刷新失败：%w", err)
	}
	c.CombineToken = newTok
	resp, _, err = c.doQuery(account)
	return resp, err
}

// doQuery is a single GET without retry. The second return reports whether the
// call was HTTP 401 (token expired) - the response is nil in that case.
func (c *OnuInfoClient) doQuery(account string) (*OnuInfoResponse, bool, error) {
	u := onuInfoURL + "?account=" + url.QueryEscape(account)
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, false, err
	}
	// Header set replicated verbatim from 查询在线_node.js so the fingerprint
	// matches the app.
	req.Header.Set("accept", "application/json, text/plain, */*")
	req.Header.Set("platform", "android")
	req.Header.Set("version", "1.1.127")
	req.Header.Set("systemversion", "android 12")
	req.Header.Set("devicebrand", "OPPO")
	req.Header.Set("devicename", "OPPO A93s 5G")
	req.Header.Set("combine-token", c.CombineToken)
	req.Header.Set("User-Agent", "okhttp/4.8.1")

	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	httpResp, err := client.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer httpResp.Body.Close()
	body, _ := io.ReadAll(httpResp.Body)
	if httpResp.StatusCode == http.StatusUnauthorized {
		return nil, true, nil
	}
	if httpResp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("HTTP %d：%s", httpResp.StatusCode, truncateStr(string(body), 200))
	}
	var r OnuInfoResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, false, fmt.Errorf("解析响应失败：%w（body：%s）", err, truncateStr(string(body), 200))
	}
	return &r, false, nil
}
