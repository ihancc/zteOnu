//go:build windows

package cmd

import (
	"encoding/csv"
	"os"

	"github.com/lxn/walk"

	"github.com/septrum101/zteOnu/app/query"
)

// BindInfoRow is one row of the 绑定信息 table (single-account, compId=317 view).
type BindInfoRow struct {
	Selected    bool
	Account     string
	OrderStatus string
	UserName    string
	UserBand    string
	UserNode    string
	BindInfo    string
	UpdateTime  string
	Notes       string
}

// buildBindInfoRow flattens a BindInfoResponse into one row for the table.
func buildBindInfoRow(account string, r *query.BindInfoResponse, err error) *BindInfoRow {
	row := &BindInfoRow{Account: account}
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

// BindInfoTableModel backs the 绑定信息 tab's TableView.
type BindInfoTableModel struct {
	walk.TableModelBase
	rows []*BindInfoRow
}

func NewBindInfoTableModel() *BindInfoTableModel { return &BindInfoTableModel{} }

func (m *BindInfoTableModel) RowCount() int { return len(m.rows) }

func (m *BindInfoTableModel) Value(row, col int) any {
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

func (m *BindInfoTableModel) Checked(row int) bool {
	if row < 0 || row >= len(m.rows) {
		return false
	}
	return m.rows[row].Selected
}

func (m *BindInfoTableModel) SetChecked(row int, checked bool) error {
	if row < 0 || row >= len(m.rows) {
		return nil
	}
	m.rows[row].Selected = checked
	return nil
}

func (m *BindInfoTableModel) Rows() []*BindInfoRow { return m.rows }

func (m *BindInfoTableModel) Append(r *BindInfoRow) {
	m.rows = append(m.rows, r)
	m.PublishRowsInserted(len(m.rows)-1, len(m.rows)-1)
}

func (m *BindInfoTableModel) Reset() {
	m.rows = nil
	m.PublishRowsReset()
}

func (m *BindInfoTableModel) SelectAll(selected bool) {
	for _, r := range m.rows {
		r.Selected = selected
	}
	if len(m.rows) > 0 {
		m.PublishRowsChanged(0, len(m.rows)-1)
	}
}

func (m *BindInfoTableModel) ExportSelectedCSV(path string) (int, error) {
	f, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
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
