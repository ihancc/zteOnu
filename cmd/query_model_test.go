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

func TestBuildQueryRowsFromForward(t *testing.T) {
	// Happy path: three neighbors on the PON, one of them has \r in the
	// LASTOFFTIME field (the endpoint sends this on the wire).
	rows := buildQueryRowsFromForward("15838372919", &query.ForwardResponse{
		Code: 200, Msg: "操作成功", Data: []query.DeviceItem{
			{ONUID: "0", OperState: "在线", AuthType: "MAC", AuthInfo: "CMDCA1F51668",
				LastOffTime: "2026-09-07 00:03:26\r", CustomersAccount: "15036171211"},
			{ONUID: "2", OperState: "在线", AuthType: "MAC", AuthInfo: "CMDCA1F51676",
				LastOffTime: "2026-09-07 09:35:29\r", CustomersAccount: "15838372919"},
			{ONUID: "12", OperState: "未知原因不在线", AuthType: "MAC", AuthInfo: "YHTC8A0B47DA",
				LastOffTime: "--\r", CustomersAccount: "15378715071"},
		},
	}, nil)
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	for _, r := range rows {
		if r.QueryAccount != "15838372919" {
			t.Errorf("QueryAccount = %q, want 15838372919", r.QueryAccount)
		}
		if strings.Contains(r.LastOffTime, "\r") {
			t.Errorf("LastOffTime %q still has CR", r.LastOffTime)
		}
	}
	if rows[1].AuthInfo != "CMDCA1F51676" || rows[1].CustomersAccount != "15838372919" {
		t.Errorf("row 1 wrong: %+v", rows[1])
	}

	// 500 → single row with the error text in OperState (备注 was removed;
	// stuffing the message in OperState keeps the offline-row highlight).
	r2 := buildQueryRowsFromForward("bad", &query.ForwardResponse{Code: 500, Msg: "未找到"}, nil)
	if len(r2) != 1 || r2[0].OperState != "未找到" {
		t.Errorf("500 branch: %+v", r2)
	}
	// Transport error → single row surfacing the error message.
	r3 := buildQueryRowsFromForward("x", nil, errors.New("timeout"))
	if len(r3) != 1 || !strings.HasPrefix(r3[0].OperState, "错误:") {
		t.Errorf("err branch: %+v", r3)
	}
	// 200 with empty data → single "no data" row.
	r4 := buildQueryRowsFromForward("y", &query.ForwardResponse{Code: 200}, nil)
	if len(r4) != 1 || r4[0].OperState != "无邻居数据" {
		t.Errorf("empty data branch: %+v", r4)
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
	m.Append(&QueryRow{Selected: true, QueryAccount: "q1", ONUID: "0",
		OperState: "在线", AuthType: "MAC", AuthInfo: "AA:BB:CC"})
	m.Append(&QueryRow{Selected: false, QueryAccount: "q2", ONUID: "1"})
	m.Append(&QueryRow{Selected: true, QueryAccount: "q3", ONUID: "2", OperState: "err"})

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
	for _, want := range []string{"查询账号", "ONU 序号", "在线", "AA:BB:CC", "err"} {
		if !strings.Contains(s, want) {
			t.Errorf("CSV missing %q; got:\n%s", want, s)
		}
	}
	if strings.Contains(s, "q2") {
		t.Errorf("unchecked row leaked into CSV:\n%s", s)
	}
}

func TestAppendMany(t *testing.T) {
	m := NewQueryTableModel()
	m.AppendMany([]*QueryRow{{QueryAccount: "a"}, {QueryAccount: "b"}})
	m.AppendMany(nil)
	m.AppendMany([]*QueryRow{{QueryAccount: "c"}})
	if m.RowCount() != 3 {
		t.Errorf("RowCount = %d, want 3", m.RowCount())
	}
}

func TestSelectByPredicate_OfflineOnly(t *testing.T) {
	m := NewQueryTableModel()
	m.AppendMany([]*QueryRow{
		{QueryAccount: "a", OperState: "在线"},
		{QueryAccount: "b", OperState: "掉电"},
		{QueryAccount: "c", OperState: "未知原因不在线"},
		{QueryAccount: "d", OperState: "在线"},
	})
	m.SelectByPredicate(func(r *QueryRow) bool { return !r.IsOnline() })
	got := make([]bool, 4)
	for i, r := range m.Rows() {
		got[i] = r.Selected
	}
	want := []bool{false, true, true, false}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d selected = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestFormatLogTime(t *testing.T) {
	if got := formatLogTime("20260902173746"); got != "2026-09-02 17:37:46" {
		t.Errorf("formatLogTime = %q, want 2026-09-02 17:37:46", got)
	}
	// Unexpected length passes through unchanged.
	if got := formatLogTime("---"); got != "---" {
		t.Errorf("passthrough failed: %q", got)
	}
}

func TestFilter_ShowOnlyOffline(t *testing.T) {
	m := NewQueryTableModel()
	m.AppendMany([]*QueryRow{
		{QueryAccount: "a", OperState: "在线"},
		{QueryAccount: "b", OperState: "掉电"},
		{QueryAccount: "c", OperState: "在线"},
		{QueryAccount: "d", OperState: "未知原因不在线"},
	})
	if m.RowCount() != 4 {
		t.Fatalf("unfiltered count = %d, want 4", m.RowCount())
	}
	m.SetShowOnlyOffline(true)
	if m.RowCount() != 2 {
		t.Errorf("filtered count = %d, want 2", m.RowCount())
	}
	for _, r := range m.Rows() {
		if r.IsOnline() {
			t.Errorf("online row leaked through filter: %+v", r)
		}
	}
	// Adding while filter is on: online row must not appear.
	m.Append(&QueryRow{QueryAccount: "e", OperState: "在线"})
	m.Append(&QueryRow{QueryAccount: "f", OperState: "掉电"})
	if m.RowCount() != 3 {
		t.Errorf("filtered count after append = %d, want 3", m.RowCount())
	}
	if got := len(m.AllRows()); got != 6 {
		t.Errorf("allRows count = %d, want 6", got)
	}
	m.SetShowOnlyOffline(false)
	if m.RowCount() != 6 {
		t.Errorf("unfiltered count after toggle = %d, want 6", m.RowCount())
	}
}

func TestSort_ONUIDNumericAndDescending(t *testing.T) {
	m := NewQueryTableModel()
	// ONUIDs "2", "10", "1", "20" - lexical would give "1","10","2","20"
	// but numeric-aware sort should give "1","2","10","20".
	m.AppendMany([]*QueryRow{
		{ONUID: "2"}, {ONUID: "10"}, {ONUID: "1"}, {ONUID: "20"},
	})
	if err := m.Sort(1, 0 /* SortAscending */); err != nil {
		t.Fatal(err)
	}
	want := []string{"1", "2", "10", "20"}
	for i, r := range m.Rows() {
		if r.ONUID != want[i] {
			t.Errorf("asc row %d = %q, want %q", i, r.ONUID, want[i])
		}
	}
	// Descending order flips.
	if err := m.Sort(1, 1 /* SortDescending */); err != nil {
		t.Fatal(err)
	}
	wantDesc := []string{"20", "10", "2", "1"}
	for i, r := range m.Rows() {
		if r.ONUID != wantDesc[i] {
			t.Errorf("desc row %d = %q, want %q", i, r.ONUID, wantDesc[i])
		}
	}
}
