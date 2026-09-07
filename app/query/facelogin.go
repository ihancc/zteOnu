package query

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// FaceLogin implements the 施工 App face-login sequence (see
// face-login-analysis.html §04):
//
//  1. GET  /auth/csrf                        → csrfToken + initial combineToken
//  2. POST /sys/user/imageIdentificationFunc → faceToken (32-hex MD5)
//  3. POST /auth/login type=6                → fresh combineToken
//
// The returned combineToken is the value carriers put in the `combine-token`
// header for all subsequent :31071 business calls (queryOnuInfo etc.).

const (
	faceBaseURL = "http://211.138.20.196:31071"

	// Fixed hash salt from the JS bundle (§06). Password formula:
	//   MD5( csrf + MD5("abc123") + SALT )
	faceSalt          = "1234567890!@#$%^&*()"
	faceFixedPassword = "abc123"
)

// csrfResponse decodes GET /auth/csrf. The combineToken is a two-UUID string
// used to authenticate the next two calls.
type csrfResponse struct {
	Token struct {
		Token string `json:"token"`
	} `json:"token"`
	CombineToken string `json:"combineToken"`
}

// faceUploadResponse decodes POST /sys/user/imageIdentificationFunc. `data` is
// the faceToken - a 32-char hex string that Step 3 embeds into the username.
type faceUploadResponse struct {
	Status  int    `json:"status"`
	Data    string `json:"data"`
	Message string `json:"message"`
}

// loginResponse decodes POST /auth/login. Status 200 = ok; the fresh
// combineToken is what the caller stores for subsequent requests.
type loginResponse struct {
	Status       int    `json:"status"`
	Message      string `json:"message"`
	CombineToken string `json:"combineToken"`
}

// FaceLoginClient runs the three-step face-login sequence. BaseURL defaults to
// the production 施工 App backend when zero.
type FaceLoginClient struct {
	BaseURL string
	HTTP    *http.Client
}

// NewFaceLoginClient returns a client with a 30s timeout on the production URL.
func NewFaceLoginClient() *FaceLoginClient {
	return &FaceLoginClient{
		BaseURL: faceBaseURL,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// Login runs the full sequence and returns the fresh combineToken. loginName is
// the 工号 associated with the face photo; imagePath is the JPEG to submit.
func (c *FaceLoginClient) Login(loginName, imagePath string) (string, error) {
	csrf, combineToken, err := c.getCSRF()
	if err != nil {
		return "", fmt.Errorf("csrf 阶段失败：%w", err)
	}
	faceToken, err := c.uploadImage(loginName, imagePath, combineToken)
	if err != nil {
		return "", fmt.Errorf("上传图片失败：%w", err)
	}
	newToken, err := c.doLogin(loginName, faceToken, csrf, combineToken)
	if err != nil {
		return "", fmt.Errorf("登录失败：%w", err)
	}
	return newToken, nil
}

// ComputeFacePassword builds the login-request password field:
// MD5(csrfToken + MD5("abc123") + SALT). Exposed for testing.
func ComputeFacePassword(csrfToken string) string {
	inner := md5Hex(faceFixedPassword)
	return md5Hex(csrfToken + inner + faceSalt)
}

func (c *FaceLoginClient) baseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return faceBaseURL
}

func (c *FaceLoginClient) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *FaceLoginClient) getCSRF() (csrfToken, combineToken string, err error) {
	req, _ := http.NewRequest("GET", c.baseURL()+"/auth/csrf", nil)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("User-Agent", "okhttp/4.8.1")
	resp, err := c.client().Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("HTTP %d：%s", resp.StatusCode, truncateStr(string(body), 200))
	}
	var r csrfResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return "", "", fmt.Errorf("解析响应失败：%w（body：%s）", err, truncateStr(string(body), 200))
	}
	if r.Token.Token == "" || r.CombineToken == "" {
		return "", "", fmt.Errorf("响应缺少 token 或 combineToken：%s", truncateStr(string(body), 200))
	}
	return r.Token.Token, r.CombineToken, nil
}

func (c *FaceLoginClient) uploadImage(loginName, imagePath, combineToken string) (string, error) {
	file, err := os.Open(imagePath)
	if err != nil {
		return "", fmt.Errorf("打开图片失败：%w", err)
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("loginName", loginName); err != nil {
		return "", err
	}
	if err := writer.WriteField("type", "6"); err != nil {
		return "", err
	}
	part, err := writer.CreateFormFile("imageFile", filepath.Base(imagePath))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, file); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}

	req, _ := http.NewRequest("POST", c.baseURL()+"/sys/user/imageIdentificationFunc", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("combine-token", combineToken)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("User-Agent", "okhttp/4.8.1")

	resp, err := c.client().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d：%s", resp.StatusCode, truncateStr(string(respBody), 200))
	}
	var r faceUploadResponse
	if err := json.Unmarshal(respBody, &r); err != nil {
		return "", fmt.Errorf("解析响应失败：%w（body：%s）", err, truncateStr(string(respBody), 200))
	}
	if r.Status != 0 {
		return "", fmt.Errorf("接口 status=%d：%s", r.Status, r.Message)
	}
	if r.Data == "" {
		return "", fmt.Errorf("响应缺少 faceToken：%s", truncateStr(string(respBody), 200))
	}
	return r.Data, nil
}

func (c *FaceLoginClient) doLogin(loginName, faceToken, csrf, combineToken string) (string, error) {
	usernameObj, _ := json.Marshal(map[string]string{
		"userName": loginName,
		"token":    faceToken,
	})
	body, _ := json.Marshal(map[string]any{
		"username": string(usernameObj),
		"password": ComputeFacePassword(csrf),
		"type":     6,
	})

	req, _ := http.NewRequest("POST", c.baseURL()+"/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("combine-token", combineToken)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("User-Agent", "okhttp/4.8.1")

	resp, err := c.client().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d：%s", resp.StatusCode, truncateStr(string(respBody), 300))
	}
	var r loginResponse
	if err := json.Unmarshal(respBody, &r); err != nil {
		return "", fmt.Errorf("解析响应失败：%w（body：%s）", err, truncateStr(string(respBody), 300))
	}
	if r.Status != 200 {
		return "", fmt.Errorf("接口 status=%d：%s", r.Status, r.Message)
	}
	if r.CombineToken == "" {
		return "", fmt.Errorf("响应缺少 combineToken：%s", truncateStr(string(respBody), 300))
	}
	return r.CombineToken, nil
}

func md5Hex(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}
