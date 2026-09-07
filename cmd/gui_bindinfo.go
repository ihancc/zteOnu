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
