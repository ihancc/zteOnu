//go:build windows

package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/septrum101/zteOnu/app/query"
)

func TestBuildQueryRowFromForward(t *testing.T) {
	// happy path
	r := buildQueryRowFromForward("15838372919", &query.ForwardResponse{
		Code: 200, Msg: "OK;OK", Data: &query.ForwardData{
			UserName:    "15838372919",
			UserBand:    "300M_40M300M@101",
			UserNode:    "郑州",
			OrderStatus: "正常",
			BindInfo:    "3109.13 172.21.70.84/0/0/19/0/6/CMDCA1F51676 GP",
			UpdateTime:  "20241003050820",
		},
	}, nil)
	if r.UserName != "15838372919" || r.UserBand != "300M_40M300M@101" || r.OrderStatus != "正常" {
		t.Errorf("bad happy row: %+v", r)
	}
	if r.Notes != "" {
		t.Errorf("expected empty notes on 200; got %q", r.Notes)
	}

	// 500: account not found
	r2 := buildQueryRowFromForward("bad", &query.ForwardResponse{
		Code: 500, Msg: "根据宽带账号未找到所属地市信息！",
	}, nil)
	if r2.Notes != "根据宽带账号未找到所属地市信息！" || r2.UserName != "" {
		t.Errorf("bad 500 row: %+v", r2)
	}

	// transport error
	r3 := buildQueryRowFromForward("x", nil, errors.New("timeout"))
	if r3.Notes != "timeout" {
		t.Errorf("bad err row: %+v", r3)
	}

	// unexpected code
	r4 := buildQueryRowFromForward("x", &query.ForwardResponse{Code: 502, Msg: "gateway"}, nil)
	if !strings.HasPrefix(r4.Notes, "code=502") {
		t.Errorf("expected code prefix; got %q", r4.Notes)
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
	m.Append(&QueryRow{Selected: true, Account: "a", OrderStatus: "正常", UserName: "u1"})
	m.Append(&QueryRow{Selected: false, Account: "b"})
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
	for _, want := range []string{"账号", "状态", "用户名", "正常", "u1", "err"} {
		if !strings.Contains(s, want) {
			t.Errorf("CSV missing %q; got:\n%s", want, s)
		}
	}
	if strings.Contains(s, ",b,") || strings.Contains(s, "\nb,") {
		t.Errorf("unchecked row leaked into CSV:\n%s", s)
	}
}
