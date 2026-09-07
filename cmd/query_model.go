//go:build windows

package cmd

import (
	"encoding/csv"
	"os"

	"github.com/lxn/walk"

	"github.com/septrum101/zteOnu/app/query"
)

// QueryRow is one row of the 光猫查询 table.
type QueryRow struct {
	Selected    bool
	Account     string
	OnlineState string
	OLT         string
	POSPort     string
	ONUEquip    string
	Notes       string
}

// buildQueryRowFromPon fills a row from the PON API response only. The batch
// path in the UI does not call the password/ONU-detail endpoints (they are
// not displayed in the table), keeping the round-trip lighter.
func buildQueryRowFromPon(account string, pon *query.PonResponse, err error) *QueryRow {
	row := &QueryRow{Account: account}
	switch {
	case err != nil:
		row.Notes = err.Error()
	case pon == nil:
		row.Notes = "无响应"
	case pon.Status != 0:
		row.Notes = "status=" + itoa(pon.Status) + " " + pon.Message
	case len(pon.Data) == 0:
		row.Notes = "无数据"
	default:
		it := pon.Data[0]
		row.OnlineState = it.NewState
		row.OLT = it.OltName
		row.POSPort = it.PosPortName
		row.ONUEquip = it.OnuEquipName
	}
	return row
}

func itoa(n int) string {
	// tiny inline int→str to keep the file dependency-light
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

// Value returns the cell text for column col, row row. Column indices match
// the TableViewColumn declarations in gui.go.
func (m *QueryTableModel) Value(row, col int) any {
	r := m.rows[row]
	switch col {
	case 0:
		return r.Account
	case 1:
		return r.OnlineState
	case 2:
		return r.OLT
	case 3:
		return r.POSPort
	case 4:
		return r.ONUEquip
	case 5:
		return r.Notes
	}
	return ""
}

// Checked / SetChecked implement walk.ItemChecker; combined with
// CheckBoxes:true on the TableView they render as a leading checkbox column.
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

// Append adds a row and notifies the view.
func (m *QueryTableModel) Append(r *QueryRow) {
	m.rows = append(m.rows, r)
	m.PublishRowsInserted(len(m.rows)-1, len(m.rows)-1)
}

// Reset drops all rows and notifies the view.
func (m *QueryTableModel) Reset() {
	m.rows = nil
	m.PublishRowsReset()
}

// SelectAll toggles selection on all rows and notifies row updates.
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

	header := []string{"账号", "在线状态", "OLT", "POS 端口", "ONU 设备", "备注"}
	if err := w.Write(header); err != nil {
		return 0, err
	}
	n := 0
	for _, r := range m.rows {
		if !r.Selected {
			continue
		}
		if err := w.Write([]string{
			r.Account, r.OnlineState, r.OLT, r.POSPort, r.ONUEquip, r.Notes,
		}); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
