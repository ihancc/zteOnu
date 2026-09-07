package query

import (
	"fmt"
	"strings"
)

// FormatResult renders a LookupResult into a plain, Chinese-labelled text block
// suitable for a TextEdit panel. Per-field errors are shown inline rather than
// hidden.
func FormatResult(r *LookupResult) string {
	if r == nil {
		return "无结果"
	}
	var b strings.Builder
	sb(&b, "账号 %s", r.Account)
	sb(&b, "")

	// PON info (main table with per-service rows)
	sb(&b, "== 宽带 / PON 信息 ==")
	if r.PonErr != nil {
		sb(&b, "  查询失败：%s", r.PonErr)
	} else if r.Pon == nil {
		sb(&b, "  无响应")
	} else if r.Pon.Status == 0 {
		if len(r.Pon.Data) == 0 {
			sb(&b, "  接口返回成功但无数据（Message: %s）", r.Pon.Message)
		} else {
			for i, it := range r.Pon.Data {
				if i > 0 {
					sb(&b, "  --")
				}
				sb(&b, "  客户账号   : %s", it.CustomersAccount)
				sb(&b, "  在线状态   : %s", it.NewState)
				sb(&b, "  OLT        : %s", it.OltName)
				sb(&b, "  POS 端口   : %s", it.PosPortName)
				sb(&b, "  ONU 设备   : %s", it.OnuEquipName)
				if it.Passwd != "" {
					sb(&b, "  宽带密码   : %s", it.Passwd)
				}
			}
		}
	} else {
		sb(&b, "  接口 status=%d：%s", r.Pon.Status, r.Pon.Message)
	}

	// Admin password
	sb(&b, "")
	sb(&b, "== 管理员密码 (CMCCAdmin) ==")
	if r.PasswdErr != nil {
		sb(&b, "  查询失败：%s", r.PasswdErr)
	} else if r.Passwd == nil {
		sb(&b, "  无响应")
	} else if r.Passwd.Status == 0 {
		if r.Passwd.Data == "" {
			sb(&b, "  接口返回成功但为空（Message: %s）", r.Passwd.Message)
		} else {
			sb(&b, "  密码       : %s", r.Passwd.Data)
		}
	} else {
		sb(&b, "  接口 status=%d：%s", r.Passwd.Status, r.Passwd.Message)
	}

	// ONU raw info (varies by account, dump raw JSON)
	sb(&b, "")
	sb(&b, "== ONU 详细信息 ==")
	if r.OnuErr != nil {
		sb(&b, "  查询失败：%s", r.OnuErr)
	} else if r.Onu == nil {
		sb(&b, "  无响应")
	} else if r.Onu.Status == 0 {
		if len(r.Onu.Data) == 0 || string(r.Onu.Data) == "null" {
			sb(&b, "  接口返回成功但无数据（Message: %s）", r.Onu.Message)
		} else {
			// Raw JSON. UI can copy it as-is.
			sb(&b, "  %s", string(r.Onu.Data))
		}
	} else {
		sb(&b, "  接口 status=%d：%s", r.Onu.Status, r.Onu.Message)
	}
	return b.String()
}

func sb(b *strings.Builder, format string, a ...any) {
	fmt.Fprintf(b, format+"\r\n", a...)
}
