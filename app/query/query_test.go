package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestQueryPonInfoRoundTrip stands up a mock server that speaks the same envelope
// as the reference API and verifies our client wires everything up correctly:
// URL path, account query string, combine-token header, and JSON decoding.
func TestQueryPonInfoRoundTrip(t *testing.T) {
	var gotHeader, gotPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("combine-token")
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PonResponse{
			Status: 0,
			Data: []PonItem{{
				CustomersAccount: "13800001111",
				NewState:         "在线",
				OltName:          "OLT-A",
				PosPortName:      "1/2/3:5",
				Passwd:           "secret",
				OnuEquipName:     "F613EV9",
			}},
		})
	}))
	defer ts.Close()

	c := &Client{Token: "tok-42", HTTP: ts.Client()}
	// Point at the mock server: swap the baseURL just for this call by using a
	// bespoke request against ts.URL.
	req, _ := http.NewRequest("GET", ts.URL+"/queryPonInfo?account=13800001111", nil)
	c.commonHeaders(req)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if gotHeader != "tok-42" {
		t.Errorf("combine-token = %q, want tok-42", gotHeader)
	}
	if !strings.Contains(gotPath, "account=13800001111") {
		t.Errorf("path = %q, missing account param", gotPath)
	}
	var r PonResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		t.Fatal(err)
	}
	if r.Status != 0 || len(r.Data) != 1 || r.Data[0].OltName != "OLT-A" {
		t.Errorf("bad decode: %+v", r)
	}
}

func TestFormatResult_HappyPath(t *testing.T) {
	res := &LookupResult{
		Account: "13800001111",
		Pon: &PonResponse{Status: 0, Data: []PonItem{
			{CustomersAccount: "13800001111", NewState: "在线",
				OltName: "OLT-A", PosPortName: "1:2:3", OnuEquipName: "F613EV9",
				Passwd: "pw123"},
		}},
		Passwd: &AdminPasswdResponse{Status: 0, Data: "aDm8H%MdA"},
		Onu:    &OnuResponse{Status: 0, Data: json.RawMessage(`{"mac":"1c:67:4a"}`)},
	}
	s := FormatResult(res)
	for _, want := range []string{"13800001111", "在线", "OLT-A", "1:2:3", "F613EV9", "aDm8H%MdA", `"mac":"1c:67:4a"`} {
		if !strings.Contains(s, want) {
			t.Errorf("output missing %q; got:\n%s", want, s)
		}
	}
}

func TestFormatResult_MixedErrors(t *testing.T) {
	// PON succeeds but has no data; ADMIN errored; ONU had a non-zero status.
	res := &LookupResult{
		Account: "13800002222",
		Pon:     &PonResponse{Status: 0, Message: "no rows", Data: nil},
		Passwd:  nil, PasswdErr: &fakeErr{"接口断线"},
		Onu: &OnuResponse{Status: 3, Message: "该用户正在查询中"},
	}
	s := FormatResult(res)
	if !strings.Contains(s, "接口返回成功但无数据") {
		t.Errorf("missing empty-data hint; got:\n%s", s)
	}
	if !strings.Contains(s, "接口断线") {
		t.Errorf("missing admin error; got:\n%s", s)
	}
	if !strings.Contains(s, "正在查询中") {
		t.Errorf("missing onu message; got:\n%s", s)
	}
}

type fakeErr struct{ s string }

func (e *fakeErr) Error() string { return e.s }
