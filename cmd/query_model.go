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
// (35+ on a fully-loaded PON), all sharing the same QueryAccount.
type QueryRow struct {
	Selected         bool
	QueryAccount     string // the account the user queried
	ONUID            string
	OperState        string
	AuthType         string
	AuthInfo         string // MAC address or password string
	CustomersAccount string // this ONU's own customer account (often blank)
	LastOffTime      string
	Notes            string
}

// buildQueryRowsFromForward flattens one forward response into 0..N rows -
// one per DeviceItem when code=200, or a single row carrying the error text
// otherwise. LASTOFFTIME arrives with a trailing "\r" that we strip.
func buildQueryRowsFromForward(account string, r *query.ForwardResponse, err error) []*QueryRow {
	switch {
	case err != nil:
		return []*QueryRow{{QueryAccount: account, Notes: err.Error()}}
	case r == nil:
		return []*QueryRow{{QueryAccount: account, Notes: "无响应"}}
	case r.Code == 200:
		if len(r.Data) == 0 {
			return []*QueryRow{{QueryAccount: account, Notes: "无邻居数据"}}
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
		return []*QueryRow{{QueryAccount: account, Notes: r.Msg}}
	default:
		return []*QueryRow{{QueryAccount: account, Notes: "code=" + itoa(r.Code) + " " + r.Msg}}
	}
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

// IsOnline reports whether a row is in the "在线" state. Any other value
// (掉电 / 未知原因不在线 / 空) counts as offline and is what the UI highlights
// and what the "选中非在线" button targets.
func (r *QueryRow) IsOnline() bool { return r.OperState == "在线" }

// QueryTableModel is walk's TableView model backing the results table. It
// implements walk.TableModel plus walk.ItemChecker so each row has a checkbox,
// and walk.Sorter (via SorterBase + a custom Sort method) so column headers
// sort the data on click.
type QueryTableModel struct {
	walk.TableModelBase
	walk.SorterBase
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

func (m *QueryTableModel) Rows() []*QueryRow { return m.rows }

func (m *QueryTableModel) Append(r *QueryRow) {
	m.rows = append(m.rows, r)
	m.PublishRowsInserted(len(m.rows)-1, len(m.rows)-1)
}

// AppendMany appends rows in one shot and publishes a single change event.
func (m *QueryTableModel) AppendMany(rows []*QueryRow) {
	if len(rows) == 0 {
		return
	}
	start := len(m.rows)
	m.rows = append(m.rows, rows...)
	m.PublishRowsInserted(start, len(m.rows)-1)
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

// SelectByPredicate sets each row's Selected to pred(row). Used by
// "选中非在线" to check offline rows and uncheck the others in one click.
func (m *QueryTableModel) SelectByPredicate(pred func(*QueryRow) bool) {
	for _, r := range m.rows {
		r.Selected = pred(r)
	}
	if len(m.rows) > 0 {
		m.PublishRowsChanged(0, len(m.rows)-1)
	}
}

// Sort implements walk.Sorter. Called by the TableView when a column header
// is clicked. ONU serial is compared as an integer when both sides parse; the
// other columns fall through to lexical order.
func (m *QueryTableModel) Sort(col int, order walk.SortOrder) error {
	sort.SliceStable(m.rows, func(i, j int) bool {
		a, b := m.rows[i], m.rows[j]
		less := m.lessAt(a, b, col)
		if order == walk.SortDescending {
			return !less
		}
		return less
	})
	return m.SorterBase.Sort(col, order)
}

// lessAt reports whether a<b for the given column. Kept separate so the
// integer-aware branch for ONUID stays readable.
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
		return a.Notes < b.Notes
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

	header := []string{"查询账号", "ONU 序号", "状态", "认证类型", "认证信息", "客户号码", "最后离线时间", "备注"}
	if err := w.Write(header); err != nil {
		return 0, err
	}
	n := 0
	for _, r := range m.rows {
		if !r.Selected {
			continue
		}
		if err := w.Write([]string{
			r.QueryAccount, r.ONUID, r.OperState, r.AuthType,
			r.AuthInfo, r.CustomersAccount, r.LastOffTime, r.Notes,
		}); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
