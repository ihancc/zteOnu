//go:build windows

package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/septrum101/zteOnu/app/query"
)

func TestBuildQueryRowFromPon(t *testing.T) {
	// happy path: pon returns one row
	pon := &query.PonResponse{Status: 0, Data: []query.PonItem{{
		CustomersAccount: "13800001111",
		NewState:         "在线",
		OltName:          "OLT-A",
		PosPortName:      "1:2:3",
		OnuEquipName:     "F613EV9",
		Passwd:           "secret", // should NOT appear in the row anymore
	}}}
	r := buildQueryRowFromPon("13800001111", pon, nil)
	if r.OnlineState != "在线" || r.OLT != "OLT-A" || r.POSPort != "1:2:3" || r.ONUEquip != "F613EV9" {
		t.Errorf("bad fields: %+v", r)
	}
	if r.Notes != "" {
		t.Errorf("expected empty notes, got %q", r.Notes)
	}

	// transport error
	r2 := buildQueryRowFromPon("x", nil, errors.New("timeout"))
	if r2.Notes != "timeout" || r2.OLT != "" {
		t.Errorf("bad err row: %+v", r2)
	}

	// pon non-zero status
	r3 := buildQueryRowFromPon("x", &query.PonResponse{Status: 3, Message: "号码不存在"}, nil)
	if r3.Notes == "" || r3.OLT != "" {
		t.Errorf("bad status row: %+v", r3)
	}

	// pon empty data
	r4 := buildQueryRowFromPon("x", &query.PonResponse{Status: 0, Data: nil}, nil)
	if r4.Notes != "无数据" {
		t.Errorf("bad empty row: %+v", r4)
	}
}

func TestParseAccounts(t *testing.T) {
	in := "13800001111\n  13800002222 \n\n13800001111\r\n13800003333,\n"
	got := parseAccounts(in)
	want := []string{"13800001111", "13800002222", "13800003333"}
	if len(got) != len(want) {
		t.Fatalf("got %d rows %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestExportSelectedCSV(t *testing.T) {
	m := NewQueryTableModel()
	m.Append(&QueryRow{Selected: true, Account: "a", OnlineState: "在线", OLT: "olt-A"})
	m.Append(&QueryRow{Selected: false, Account: "b"}) // not exported
	m.Append(&QueryRow{Selected: true, Account: "c", Notes: "err"})

	path := filepath.Join(t.TempDir(), "out.csv")
	n, err := m.ExportSelectedCSV(path)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("exported %d rows, want 2", n)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	if len(s) < 3 || s[0] != 0xEF || s[1] != 0xBB || s[2] != 0xBF {
		t.Error("missing UTF-8 BOM")
	}
	for _, want := range []string{"账号", "在线状态", "OLT", "在线", "olt-A", "err"} {
		if !contains(s, want) {
			t.Errorf("CSV missing %q; got:\n%s", want, s)
		}
	}
	// The unchecked row must NOT appear.
	if contains(s, ",b,") || contains(s, "\nb,") {
		t.Errorf("unchecked row leaked into CSV:\n%s", s)
	}
}

// contains is stdlib strings.Contains inlined so the test file needs no import
// beyond os + testing.
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
