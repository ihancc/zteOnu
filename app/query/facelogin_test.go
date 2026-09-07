package query

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestComputeFacePassword locks the MD5 formula from face-login-analysis.html
// §06: MD5(csrf + MD5("abc123") + SALT). The doc's worked example is
// csrf="d672fa26-1f11-45b9-9978-9feb590dac3f" → password
// "e790bedf4741467e638da19be557769e".
func TestComputeFacePassword(t *testing.T) {
	got := ComputeFacePassword("d672fa26-1f11-45b9-9978-9feb590dac3f")
	if got != "e790bedf4741467e638da19be557769e" {
		t.Errorf("ComputeFacePassword = %q, want e790bedf4741467e638da19be557769e", got)
	}
}

// TestFaceLoginFlow stands up a mock server that speaks the three steps of
// the face-login sequence and verifies every hop: URL path, method, header
// choices (combine-token vs none), multipart fields, and JSON payload.
func TestFaceLoginFlow(t *testing.T) {
	// Prepare a tiny "image" for step 2.
	tmpImg := filepath.Join(t.TempDir(), "face.jpg")
	if err := os.WriteFile(tmpImg, []byte("fake jpeg bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	const (
		csrfTok    = "csrf-value"
		initToken  = "init-combine-token"
		faceTok    = "abcdef1234567890abcdef1234567890"
		finalToken = "final-combine-token"
	)

	var seenSteps []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/auth/csrf":
			seenSteps = append(seenSteps, "csrf")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token":        map[string]string{"token": csrfTok},
				"combineToken": initToken,
			})
		case "/sys/user/imageIdentificationFunc":
			seenSteps = append(seenSteps, "upload")
			if got := r.Header.Get("combine-token"); got != initToken {
				t.Errorf("upload combine-token = %q, want %q", got, initToken)
			}
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Fatal(err)
			}
			if r.FormValue("loginName") != "tt_wangbang" {
				t.Errorf("loginName = %q, want tt_wangbang", r.FormValue("loginName"))
			}
			if r.FormValue("type") != "6" {
				t.Errorf("type = %q, want 6", r.FormValue("type"))
			}
			file, _, err := r.FormFile("imageFile")
			if err != nil {
				t.Fatal(err)
			}
			data, _ := io.ReadAll(file)
			if string(data) != "fake jpeg bytes" {
				t.Errorf("imageFile bytes = %q", data)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"status": 0, "data": faceTok})
		case "/auth/login":
			seenSteps = append(seenSteps, "login")
			if got := r.Header.Get("combine-token"); got != initToken {
				t.Errorf("login combine-token = %q, want %q", got, initToken)
			}
			body, _ := io.ReadAll(r.Body)
			var m map[string]any
			if err := json.Unmarshal(body, &m); err != nil {
				t.Fatalf("login body: %v (%s)", err, body)
			}
			if pw := m["password"]; pw != ComputeFacePassword(csrfTok) {
				t.Errorf("login password = %v, want %s", pw, ComputeFacePassword(csrfTok))
			}
			// username is a JSON string of {userName, token}.
			var u map[string]string
			if err := json.Unmarshal([]byte(m["username"].(string)), &u); err != nil {
				t.Fatalf("username json: %v", err)
			}
			if u["userName"] != "tt_wangbang" || u["token"] != faceTok {
				t.Errorf("username inner = %+v", u)
			}
			if m["type"] != float64(6) {
				t.Errorf("type = %v", m["type"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":       200,
				"message":      "OK",
				"combineToken": finalToken,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	c := &FaceLoginClient{BaseURL: ts.URL, HTTP: ts.Client()}
	tok, err := c.Login("tt_wangbang", tmpImg)
	if err != nil {
		t.Fatal(err)
	}
	if tok != finalToken {
		t.Errorf("returned token = %q, want %q", tok, finalToken)
	}
	if strings.Join(seenSteps, ",") != "csrf,upload,login" {
		t.Errorf("steps = %v, want csrf,upload,login", seenSteps)
	}
}
