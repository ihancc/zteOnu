//go:build windows

package cmd

import (
	"encoding/csv"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/lxn/walk"

	"github.com/septrum101/zteOnu/app/query"
)

// QueryRow is one row of the 光猫查询 table - one ONU on the same PON port as
// the queried broadband account. A single query typically produces many rows
// (35+ on a fully-loaded PON), all sharing the same QueryAccount. Fields
// with the "detail." prefix (in comments) are filled in by the compId=1230
// per-ONU-detail step, which the user triggers with the "获取详情" button.
type QueryRow struct {
	Selected         bool
	QueryAccount     string // the account the user queried
	ONUID            string
	OperState        string // from compId=1310 (list view; may be stale)
	AuthType         string
	AuthInfo         string // MAC address or password string
	CustomersAccount string // this ONU's own customer account (often blank)
	LastOffTime      string
	// filled by QueryDetail (compId=1230):
	Password       string // detail.onuPasswd
	AccountStatus  string // detail.orderStatus (正常 / 暂停 / …)
	ONURunState    string // detail.OperState (fresh)
	PonPortName    string // detail.oltPort
	SplitterName   string // detail.spos (分光器)
	LastAuthTime   string // detail.logTime (formatted "yyyy-MM-dd HH:mm:ss")
	LastAuthResult string // detail.bmsOperateType
}

// buildQueryRowsFromForward flattens one forward response into 0..N rows -
// one per DeviceItem when code=200, or a single row surfacing the error text
// in OperState (which also triggers the offline-row red highlight) when the
// query failed. LASTOFFTIME arrives with a trailing "\r" that we strip.
func buildQueryRowsFromForward(account string, r *query.ForwardResponse, err error) []*QueryRow {
	switch {
	case err != nil:
		return []*QueryRow{{QueryAccount: account, OperState: "错误: " + err.Error()}}
	case r == nil:
		return []*QueryRow{{QueryAccount: account, OperState: "无响应"}}
	case r.Code == 200:
		if len(r.Data) == 0 {
			return []*QueryRow{{QueryAccount: account, OperState: "无邻居数据"}}
		}
		rows := make([]*QueryRow, 0, len(r.Data))
		for _, d := range r.Data {
			rows = append(rows, &QueryRow{
				QueryAccount:     account,
				ONUID:            d.ONUID,
				OperState:        d.OperState,
				AuthType:         d.AuthType,
				AuthInfo:         d.AuthInfo,
				CustomersAccount: d.CustomersAccount,
				LastOffTime:      strings.TrimSpace(strings.Trim(d.LastOffTime, "\r\n")),
			})
		}
		return rows
	case r.Code == 500:
		return []*QueryRow{{QueryAccount: account, OperState: r.Msg}}
	default:
		return []*QueryRow{{QueryAccount: account, OperState: "code=" + itoa(r.Code) + " " + r.Msg}}
	}
}

// ApplyDetail copies the compId=1230 detail response into row's detail fields.
// Called after the "获取详情" step for each checked row.
func ApplyDetail(row *QueryRow, d *query.DetailData) {
	row.Password = d.OnuPasswd
	row.AccountStatus = d.OrderStatus
	row.ONURunState = d.OperState
	row.PonPortName = d.OltPort
	row.SplitterName = d.Spos
	row.LastAuthTime = formatLogTime(d.LogTime)
	row.LastAuthResult = d.BmsOperateType
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// IsOnline reports whether a row is in the "在线" state. The fresh value from
// the compId=1230 detail step (ONURunState) is preferred over the initial
// compId=1310 list value (OperState) when it has been populated; strings are
// whitespace-trimmed so a stray "\r" or space cannot make an online row look
// offline. Any non-"在线" value counts as offline (掉电 / 未知原因不在线 /
// error / empty), which is what the UI highlights red and what the
// "选中非在线" and "只显示非在线" toggles both key off.
func (r *QueryRow) IsOnline() bool {
	state := strings.TrimSpace(r.ONURunState)
	if state == "" {
		state = strings.TrimSpace(r.OperState)
	}
	return state == "在线"
}

// QueryTableModel is walk's TableView model backing the results table. It
// implements walk.TableModel plus walk.ItemChecker so each row has a checkbox,
// and walk.Sorter (via SorterBase + a custom Sort method) so column headers
// sort the data on click. allRows is the master list; rows is the currently-
// visible subset (rebuilt when the filter/sort changes).
type QueryTableModel struct {
	walk.TableModelBase
	walk.SorterBase
	allRows         []*QueryRow
	rows            []*QueryRow
	showOnlyOffline bool
}

// formatLogTime turns "20260902173746" into "2026-09-02 17:37:46" for display.
// Any unexpected length passes through unchanged.
func formatLogTime(s string) string {
	if len(s) != 14 {
		return s
	}
	return s[0:4] + "-" + s[4:6] + "-" + s[6:8] + " " + s[8:10] + ":" + s[10:12] + ":" + s[12:14]
}

// visible reports whether a row passes the current filter.
func (m *QueryTableModel) visible(r *QueryRow) bool {
	if m.showOnlyOffline {
		return !r.IsOnline()
	}
	return true
}

// rebuildVisible walks allRows in current sort order and re-fills rows with
// entries that pass the filter. A fresh slice (not the backing array of the
// previous rows) is allocated so walk cannot observe a stale slice header.
func (m *QueryTableModel) rebuildVisible() {
	next := make([]*QueryRow, 0, len(m.allRows))
	for _, r := range m.allRows {
		if m.visible(r) {
			next = append(next, r)
		}
	}
	m.rows = next
}

// SetShowOnlyOffline toggles the "只显示非在线" filter and republishes.
func (m *QueryTableModel) SetShowOnlyOffline(on bool) {
	if m.showOnlyOffline == on {
		return
	}
	m.showOnlyOffline = on
	m.rebuildVisible()
	m.PublishRowsReset()
}

// AllRows exposes the master list (unfiltered). CSV export and detail-fetch
// iterate this so hidden but checked rows are still processed.
func (m *QueryTableModel) AllRows() []*QueryRow { return m.allRows }

func NewQueryTableModel() *QueryTableModel { return &QueryTableModel{} }

func (m *QueryTableModel) RowCount() int { return len(m.rows) }

// Value returns the cell text for column col. Column indices match the
// TableViewColumn declarations in gui.go (14 columns; 备注 was removed and
// the compId=1230 detail step adds 7 columns starting at index 7).
func (m *QueryTableModel) Value(row, col int) any {
	r := m.rows[row]
	switch col {
	case 0:
		return r.QueryAccount
	case 1:
		return r.ONUID
	case 2:
		return r.OperState
	case 3:
		return r.AuthType
	case 4:
		return r.AuthInfo
	case 5:
		return r.CustomersAccount
	case 6:
		return r.LastOffTime
	case 7:
		return r.Password
	case 8:
		return r.AccountStatus
	case 9:
		return r.ONURunState
	case 10:
		return r.PonPortName
	case 11:
		return r.SplitterName
	case 12:
		return r.LastAuthTime
	case 13:
		return r.LastAuthResult
	}
	return ""
}

func (m *QueryTableModel) Checked(row int) bool {
	if row < 0 || row >= len(m.rows) {
		return false
	}
	return m.rows[row].Selected
}

func (m *QueryTableModel) SetChecked(row int, checked bool) error {
	if row < 0 || row >= len(m.rows) {
		return nil
	}
	m.rows[row].Selected = checked
	return nil
}

// Rows exposes the currently-visible slice (post filter+sort).
func (m *QueryTableModel) Rows() []*QueryRow { return m.rows }

// Append adds one row to allRows and, if the filter allows, to the visible list.
func (m *QueryTableModel) Append(r *QueryRow) {
	m.allRows = append(m.allRows, r)
	if m.visible(r) {
		m.rows = append(m.rows, r)
		m.PublishRowsInserted(len(m.rows)-1, len(m.rows)-1)
	}
}

// AppendMany appends rows in one shot and publishes one change event.
func (m *QueryTableModel) AppendMany(rows []*QueryRow) {
	if len(rows) == 0 {
		return
	}
	m.allRows = append(m.allRows, rows...)
	startVisible := len(m.rows)
	for _, r := range rows {
		if m.visible(r) {
			m.rows = append(m.rows, r)
		}
	}
	if endVisible := len(m.rows) - 1; endVisible >= startVisible {
		m.PublishRowsInserted(startVisible, endVisible)
	}
}

func (m *QueryTableModel) Reset() {
	m.allRows = nil
	m.rows = nil
	m.PublishRowsReset()
}

// SelectAll toggles selection on every row (allRows, so hidden rows too).
func (m *QueryTableModel) SelectAll(selected bool) {
	for _, r := range m.allRows {
		r.Selected = selected
	}
	if len(m.rows) > 0 {
		m.PublishRowsChanged(0, len(m.rows)-1)
	}
}

// SelectByPredicate walks allRows so hidden rows are toggled too - matches
// the "select all offline" intent regardless of the current filter.
func (m *QueryTableModel) SelectByPredicate(pred func(*QueryRow) bool) {
	for _, r := range m.allRows {
		r.Selected = pred(r)
	}
	if len(m.rows) > 0 {
		m.PublishRowsChanged(0, len(m.rows)-1)
	}
}

// PublishRowChangedFor finds the visible index of r and publishes a change so
// the TableView repaints that row after a detail-fetch update. No-op when r
// is not currently visible (filtered out).
func (m *QueryTableModel) PublishRowChangedFor(r *QueryRow) {
	for i, v := range m.rows {
		if v == r {
			m.PublishRowChanged(i)
			return
		}
	}
}

// Sort implements walk.Sorter. Sorts allRows then rebuilds the visible list
// in the new order. ONU serial is compared as an integer when both sides
// parse; other columns fall through to lexical order.
func (m *QueryTableModel) Sort(col int, order walk.SortOrder) error {
	sort.SliceStable(m.allRows, func(i, j int) bool {
		a, b := m.allRows[i], m.allRows[j]
		less := m.lessAt(a, b, col)
		if order == walk.SortDescending {
			return !less
		}
		return less
	})
	m.rebuildVisible()
	m.PublishRowsReset()
	return m.SorterBase.Sort(col, order)
}

// lessAt reports whether a<b for the given column.
func (m *QueryTableModel) lessAt(a, b *QueryRow, col int) bool {
	switch col {
	case 0:
		return a.QueryAccount < b.QueryAccount
	case 1:
		return cmpIntish(a.ONUID, b.ONUID)
	case 2:
		return a.OperState < b.OperState
	case 3:
		return a.AuthType < b.AuthType
	case 4:
		return a.AuthInfo < b.AuthInfo
	case 5:
		return a.CustomersAccount < b.CustomersAccount
	case 6:
		return a.LastOffTime < b.LastOffTime
	case 7:
		return a.Password < b.Password
	case 8:
		return a.AccountStatus < b.AccountStatus
	case 9:
		return a.ONURunState < b.ONURunState
	case 10:
		return a.PonPortName < b.PonPortName
	case 11:
		return a.SplitterName < b.SplitterName
	case 12:
		return a.LastAuthTime < b.LastAuthTime
	case 13:
		return a.LastAuthResult < b.LastAuthResult
	}
	return false
}

// cmpIntish returns a<b treating both as ints when they parse cleanly,
// otherwise falling back to lexical order. Keeps "10" after "2" in the ONU
// column instead of before it.
func cmpIntish(a, b string) bool {
	ai, aerr := strconv.Atoi(a)
	bi, berr := strconv.Atoi(b)
	if aerr == nil && berr == nil {
		return ai < bi
	}
	return a < b
}

// ExportSelectedCSV writes checked rows to path as UTF-8 BOM CSV suitable for
// Excel. Returns the number of rows written.
func (m *QueryTableModel) ExportSelectedCSV(path string) (int, error) {
	f, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	// UTF-8 BOM so Excel picks up the encoding correctly.
	if _, err := f.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return 0, err
	}
	w := csv.NewWriter(f)
	defer w.Flush()

	header := []string{
		"查询账号", "ONU 序号", "状态", "认证类型", "认证信息",
		"客户号码", "最后离线时间", "密码", "账号状态", "ONU 运行状态",
		"PON 口名称", "分光器名称", "最后认证时间", "最后认证结果",
	}
	if err := w.Write(header); err != nil {
		return 0, err
	}
	n := 0
	// Iterate allRows so hidden-but-selected rows still export.
	for _, r := range m.allRows {
		if !r.Selected {
			continue
		}
		if err := w.Write([]string{
			r.QueryAccount, r.ONUID, r.OperState, r.AuthType, r.AuthInfo,
			r.CustomersAccount, r.LastOffTime, r.Password, r.AccountStatus,
			r.ONURunState, r.PonPortName, r.SplitterName, r.LastAuthTime,
			r.LastAuthResult,
		}); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
