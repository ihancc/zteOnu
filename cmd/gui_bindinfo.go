//go:build windows

package cmd

import (
	"fmt"
	"strings"

	"github.com/lxn/walk"

	"github.com/septrum101/zteOnu/app/query"
	"github.com/septrum101/zteOnu/app/sso"
)

// onBindInfoRun batch-queries the compId=317 flavor (single-account bindinfo
// view) for the accounts in the 绑定信息查询 tab. Shares the on-disk SSO cache
// with the 光猫查询 tab so filling the 工号 in either tab primes both.
func (g *gui) onBindInfoRun() {
	if g.bindBusy {
		return
	}
	loginName := strings.TrimSpace(g.bindLoginEdit.Text())
	accounts := parseAccounts(g.bindAccountsText.Text())
	if loginName == "" {
		walk.MsgBox(g.mw, "提示", "请先填写工号 (loginName)", walk.MsgBoxIconWarning)
		return
	}
	if len(accounts) == 0 {
		walk.MsgBox(g.mw, "提示", "账号列表为空", walk.MsgBoxIconWarning)
		return
	}
	saveLoginName(loginName)

	concurrency := atoiDefault(g.bindConcurrencyEdit.Text(), 4)
	if concurrency > 20 {
		concurrency = 20
	}

	g.bindBusy = true
	g.bindRunBtn.SetEnabled(false)
	g.bindRunBtn.SetText("查询中…")
	g.bindModel.Reset()
	g.bindHint.SetText("正在换取 access_token……")

	go func() {
		ssoCli := sso.New()
		tok, err := sso.EnsureToken(loginName, tokenCachePath(), ssoCli)
		if err != nil {
			g.mw.Synchronize(func() {
				g.bindBusy = false
				g.bindRunBtn.SetEnabled(true)
				g.bindRunBtn.SetText("批量查询")
				g.bindHint.SetText("SSO 失败：" + err.Error())
			})
			return
		}
		g.mw.Synchronize(func() {
			g.bindHint.SetText(fmt.Sprintf("token 就绪，%d 个账号 / 并发 %d……", len(accounts), concurrency))
		})

		client, err := query.NewForwardClient(tok)
		if err != nil {
			g.mw.Synchronize(func() {
				g.bindBusy = false
				g.bindRunBtn.SetEnabled(true)
				g.bindRunBtn.SetText("批量查询")
				g.bindHint.SetText("初始化查询客户端失败：" + err.Error())
			})
			return
		}
		client.TokenProvider = func() (string, error) {
			sso.Invalidate(tokenCachePath())
			return sso.EnsureToken(loginName, tokenCachePath(), ssoCli)
		}

		sem := make(chan struct{}, concurrency)
		done := make(chan *BindInfoRow, len(accounts))
		for _, acct := range accounts {
			acct := acct
			sem <- struct{}{}
			go func() {
				defer func() { <-sem }()
				resp, qerr := client.QueryBindInfo(acct)
				done <- buildBindInfoRow(acct, resp, qerr)
			}()
		}
		completed := 0
		for range accounts {
			row := <-done
			completed++
			cnt := completed
			g.mw.Synchronize(func() {
				g.bindModel.Append(row)
				g.bindHint.SetText(fmt.Sprintf("已完成 %d/%d", cnt, len(accounts)))
			})
		}
		g.mw.Synchronize(func() {
			g.bindBusy = false
			g.bindRunBtn.SetEnabled(true)
			g.bindRunBtn.SetText("批量查询")
			g.bindHint.SetText(fmt.Sprintf("完成：%d 条", len(accounts)))
		})
	}()
}

// onQueryFetchDetails walks the checked rows and calls scene/security/forward
// with compId=1230 for each customer account, filling in Password /
// AccountStatus / ONURunState / PonPortName / SplitterName / LastAuthTime /
// LastAuthResult. Requests are sequential (one after the other, no
// concurrency) per user request.
func (g *gui) onQueryFetchDetails() {
	if g.queryBusy {
		return
	}
	loginName := strings.TrimSpace(g.queryLoginEdit.Text())
	if loginName == "" {
		walk.MsgBox(g.mw, "提示", "请先在“工号”栏填写 loginName", walk.MsgBoxIconWarning)
		return
	}

	// Snapshot which rows to fetch (checked + non-empty customer account).
	var targets []*QueryRow
	for _, r := range g.queryModel.AllRows() {
		if r.Selected && strings.TrimSpace(r.CustomersAccount) != "" {
			targets = append(targets, r)
		}
	}
	if len(targets) == 0 {
		walk.MsgBox(g.mw, "提示", "请先勾选要获取详情的行（客户号码非空）", walk.MsgBoxIconWarning)
		return
	}

	g.queryBusy = true
	g.queryFetchBtn.SetEnabled(false)
	g.queryFetchBtn.SetText("获取中…")
	g.queryHint.SetText(fmt.Sprintf("准备获取 %d 条详情……", len(targets)))

	go func() {
		ssoCli := sso.New()
		tok, err := sso.EnsureToken(loginName, tokenCachePath(), ssoCli)
		if err != nil {
			g.mw.Synchronize(func() {
				g.queryBusy = false
				g.queryFetchBtn.SetEnabled(true)
				g.queryFetchBtn.SetText("获取详情")
				g.queryHint.SetText("SSO 失败：" + err.Error())
			})
			return
		}
		client, err := query.NewForwardClient(tok)
		if err != nil {
			g.mw.Synchronize(func() {
				g.queryBusy = false
				g.queryFetchBtn.SetEnabled(true)
				g.queryFetchBtn.SetText("获取详情")
				g.queryHint.SetText("初始化查询客户端失败：" + err.Error())
			})
			return
		}
		client.TokenProvider = func() (string, error) {
			sso.Invalidate(tokenCachePath())
			return sso.EnsureToken(loginName, tokenCachePath(), ssoCli)
		}

		// One-at-a-time (no concurrency).
		for i, row := range targets {
			resp, qerr := client.QueryDetail(row.CustomersAccount)
			done := i + 1
			r := row
			g.mw.Synchronize(func() {
				if qerr != nil {
					r.LastAuthResult = "错误: " + qerr.Error()
				} else if resp != nil && resp.Code == 200 && resp.Data != nil {
					ApplyDetail(r, resp.Data)
				} else if resp != nil {
					r.LastAuthResult = fmt.Sprintf("code=%d %s", resp.Code, resp.Msg)
				}
				g.queryModel.PublishRowChangedFor(r)
				g.queryHint.SetText(fmt.Sprintf("已获取 %d/%d", done, len(targets)))
			})
		}
		g.mw.Synchronize(func() {
			g.queryBusy = false
			g.queryFetchBtn.SetEnabled(true)
			g.queryFetchBtn.SetText("获取详情")
			g.queryHint.SetText(fmt.Sprintf("详情获取完成：%d 条", len(targets)))
		})
	}()
}

// onBindInfoExport writes checked rows of the 绑定信息 table to a CSV picked
// by the user.
func (g *gui) onBindInfoExport() {
	if g.bindModel.RowCount() == 0 {
		walk.MsgBox(g.mw, "提示", "结果为空", walk.MsgBoxIconWarning)
		return
	}
	selCount := 0
	for _, r := range g.bindModel.Rows() {
		if r.Selected {
			selCount++
		}
	}
	if selCount == 0 {
		walk.MsgBox(g.mw, "提示", "请先勾选要导出的行", walk.MsgBoxIconWarning)
		return
	}
	dlg := new(walk.FileDialog)
	dlg.Title = "导出选中行为 CSV"
	dlg.Filter = "CSV 文件 (*.csv)|*.csv"
	dlg.FilterIndex = 1
	dlg.FilePath = "bindinfo.csv"
	if ok, err := dlg.ShowSave(g.mw); err != nil {
		walk.MsgBox(g.mw, "错误", err.Error(), walk.MsgBoxIconError)
		return
	} else if !ok {
		return
	}
	path := dlg.FilePath
	if !strings.HasSuffix(strings.ToLower(path), ".csv") {
		path += ".csv"
	}
	n, err := g.bindModel.ExportSelectedCSV(path)
	if err != nil {
		walk.MsgBox(g.mw, "错误", err.Error(), walk.MsgBoxIconError)
		return
	}
	g.bindHint.SetText(fmt.Sprintf("已导出 %d 条到 %s", n, path))
}
