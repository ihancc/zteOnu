//go:build windows

package cmd

import (
	"encoding/csv"
	"os"

	"github.com/lxn/walk"

	"github.com/septrum101/zteOnu/app/query"
)

// QueryRow is one row of the 光猫查询 table. Fields mirror scene/security/forward
// so the CSV export lines up 1:1 with what the operator sees.
type QueryRow struct {
	Selected    bool
	Account     string
	OrderStatus string // 状态 (正常 / 暂停 / …)
	UserName    string // 用户名 (通常手机号)
	UserBand    string // 带宽 (如 300M_40M300M@101)
	UserNode    string // 地市
	BindInfo    string // OLT / POS / ONU 定位串
	UpdateTime  string // 更新时间
	Notes       string // 错误 / 未找到 说明
}

// buildQueryRowFromForward flattens a scene/security/forward response into a
// table row. code 200 populates the fields; 500 lands the message in Notes;
// 401 is unusual to reach here (retry-after-refresh should have handled it).
func buildQueryRowFromForward(account string, r *query.ForwardResponse, err error) *QueryRow {
	row := &QueryRow{Account: account}
	switch {
	case err != nil:
		row.Notes = err.Error()
	case r == nil:
		row.Notes = "无响应"
	case r.Code == 200 && r.Data != nil:
		row.OrderStatus = r.Data.OrderStatus
		row.UserName = r.Data.UserName
		row.UserBand = r.Data.UserBand
		row.UserNode = r.Data.UserNode
		row.BindInfo = r.Data.BindInfo
		row.UpdateTime = r.Data.UpdateTime
	case r.Code == 500:
		row.Notes = r.Msg
	default:
		row.Notes = "code=" + itoa(r.Code) + " " + r.Msg
	}
	return row
}

func itoa(n int) string {
	// tiny inline int→str; the whole file avoids strconv only to keep imports
	// minimal for the model layer.
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

// QueryTableModel is walk's TableView model backing the results table. It
// implements walk.TableModel plus walk.ItemChecker so each row has a checkbox.
type QueryTableModel struct {
	walk.TableModelBase
	rows []*QueryRow
}

func NewQueryTableModel() *QueryTableModel { return &QueryTableModel{} }

func (m *QueryTableModel) RowCount() int { return len(m.rows) }

// Value returns the cell text for column col. Column indices match the
// TableViewColumn declarations in gui.go.
func (m *QueryTableModel) Value(row, col int) any {
	r := m.rows[row]
	switch col {
	case 0:
		return r.Account
	case 1:
		return r.OrderStatus
	case 2:
		return r.UserName
	case 3:
		return r.UserBand
	case 4:
		return r.UserNode
	case 5:
		return r.BindInfo
	case 6:
		return r.UpdateTime
	case 7:
		return r.Notes
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

// Rows exposes the underlying slice (read-only expected).
func (m *QueryTableModel) Rows() []*QueryRow { return m.rows }

func (m *QueryTableModel) Append(r *QueryRow) {
	m.rows = append(m.rows, r)
	m.PublishRowsInserted(len(m.rows)-1, len(m.rows)-1)
}

func (m *QueryTableModel) Reset() {
	m.rows = nil
	m.PublishRowsReset()
}

func (m *QueryTableModel) SelectAll(selected bool) {
	for _, r := range m.rows {
		r.Selected = selected
	}
	if len(m.rows) > 0 {
		m.PublishRowsChanged(0, len(m.rows)-1)
	}
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

	header := []string{"账号", "状态", "用户名", "带宽", "地市", "绑定信息", "更新时间", "备注"}
	if err := w.Write(header); err != nil {
		return 0, err
	}
	n := 0
	for _, r := range m.rows {
		if !r.Selected {
			continue
		}
		if err := w.Write([]string{
			r.Account, r.OrderStatus, r.UserName, r.UserBand,
			r.UserNode, r.BindInfo, r.UpdateTime, r.Notes,
		}); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
