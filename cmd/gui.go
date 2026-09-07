//go:build windows

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"github.com/lxn/walk"
	// declarative is the idiomatic dot-import for walk GUI descriptions.
	. "github.com/lxn/walk/declarative"
	"github.com/spf13/cobra"

	"github.com/septrum101/zteOnu/app/factory"
	"github.com/septrum101/zteOnu/app/onu"
	"github.com/septrum101/zteOnu/app/query"
	"github.com/septrum101/zteOnu/app/sso"
	tnet "github.com/septrum101/zteOnu/app/telnet"
	"github.com/septrum101/zteOnu/version"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "gui",
		Short: "Start the native Windows GUI",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			runGUI()
		},
	})
}

// gui holds the widgets that the run handler reads from and writes to.
type gui struct {
	mw *walk.MainWindow

	ipEdit, httpPortEdit, userEdit, passEdit *walk.LineEdit
	macEdit, telnetPortEdit                  *walk.LineEdit
	macModeCB, ifaceCB, actionCB             *walk.ComboBox
	logView                                  *walk.TextEdit
	credLabel                                *walk.Label
	runBtn                                   *walk.PushButton

	// one-click provisioning
	snEdit, onePassEdit            *walk.LineEdit
	ponCB, regionCB                *walk.ComboBox
	ensureWANCB, rebootAfterCB     *walk.CheckBox
	lan1CB, lan2CB, lan3CB, lan4CB *walk.CheckBox
	ensureRxCB                     *walk.CheckBox
	rxMaxEdit, rxTargetEdit        *walk.LineEdit
	oneBtn                         *walk.PushButton
	oneStatus                      *walk.Label

	// telnet command console
	cmdUserEdit, cmdPassEdit, cmdEdit           *walk.LineEdit
	cmdConnectBtn, cmdDisconnectBtn, cmdSendBtn *walk.PushButton
	cmdStatus                                   *walk.Label
	console                                     *tnet.Telnet
	consoleBusy                                 bool

	// status tab
	statusText       *walk.TextEdit
	statusRefreshBtn *walk.PushButton
	statusHint       *walk.Label

	// 光猫查询 tab (independent of the ONU-side flows above)
	queryLoginEdit                  *walk.LineEdit
	queryAccountEdit                *walk.LineEdit
	faceImageEdit                   *walk.LineEdit
	faceBrowseBtn, faceLoginBtn     *walk.PushButton
	faceStatus                      *walk.Label
	combineToken                    string
	queryRunBtn                     *walk.PushButton
	querySelAllBtn, querySelNoneBtn *walk.PushButton
	querySelOfflineBtn              *walk.PushButton
	queryFetchBtn                   *walk.PushButton
	queryOfflineOnlyCB              *walk.CheckBox
	queryExportBtn, queryClearBtn   *walk.PushButton
	queryHint                       *walk.Label
	queryTable                      *walk.TableView
	queryModel                      *QueryTableModel
	queryBusy                       bool

	// 绑定信息 tab (compId=317 single-account view)
	bindLoginEdit, bindConcurrencyEdit        *walk.LineEdit
	bindAccountsText                          *walk.TextEdit
	bindRunBtn, bindSelAllBtn, bindSelNoneBtn *walk.PushButton
	bindExportBtn, bindClearBtn               *walk.PushButton
	bindHint                                  *walk.Label
	bindTable                                 *walk.TableView
	bindModel                                 *BindInfoTableModel
	bindBusy                                  bool

	ifaces []factory.InterfaceInfo

	running bool
}

// regionModel builds the region combo-box labels from onu.Regions.
func regionModel() []string {
	m := make([]string, len(onu.Regions))
	for i, r := range onu.Regions {
		m[i] = fmt.Sprintf("%d  %s", r.ID, r.Name)
	}
	return m
}

// ifaceModel builds the interface combo-box labels ("name  (MAC)").
func (g *gui) ifaceModel() []string {
	m := make([]string, len(g.ifaces))
	for i, it := range g.ifaces {
		m[i] = fmt.Sprintf("%s  (%s)", it.Name, it.MAC)
	}
	return m
}

func runGUI() {
	// When double-clicked, this process owns a fresh console window; hide it so
	// only the GUI is visible. When launched from an existing shell, leave that
	// shell alone.
	hideOwnConsole()

	g := &gui{}
	g.ifaces = factory.Interfaces()
	g.queryModel = NewQueryTableModel()
	g.bindModel = NewBindInfoTableModel()

	if err := (MainWindow{
		AssignTo: &g.mw,
		Title:    "ZTE ONU 工具",
		MinSize:  Size{Width: 900, Height: 560},
		Size:     Size{Width: 1100, Height: 680},
		Layout:   VBox{},
		Children: []Widget{
			TabWidget{
				Pages: []TabPage{
					{
						Title:  "光猫配置",
						Layout: HBox{},
						Children: []Widget{
							Composite{
								MaxSize: Size{Width: 400},
								Layout:  VBox{},
								Children: []Widget{
									GroupBox{
										Title:  "连接设置",
										Layout: Grid{Columns: 2},
										Children: []Widget{
											Label{Text: "IP 地址"},
											LineEdit{AssignTo: &g.ipEdit, Text: "192.168.1.1"},
											Label{Text: "HTTP 端口"},
											LineEdit{AssignTo: &g.httpPortEdit, Text: "80"},
											Label{Text: "telnet 端口"},
											LineEdit{AssignTo: &g.telnetPortEdit, Text: "23"},
											Label{Text: "工厂用户名"},
											LineEdit{AssignTo: &g.userEdit, Text: "CMCCAdmin"},
											Label{Text: "工厂密码"},
											LineEdit{AssignTo: &g.passEdit, Text: "aDm8H%MdA"},
										},
									},
									GroupBox{
										Title:  "客户端 MAC",
										Layout: Grid{Columns: 2},
										Children: []Widget{
											Label{Text: "来源"},
											ComboBox{
												AssignTo:              &g.macModeCB,
												Editable:              false,
												Model:                 []string{"自动检测", "指定网络接口", "自定义 MAC"},
												CurrentIndex:          0,
												OnCurrentIndexChanged: g.syncMacFields,
											},
											Label{Text: "网络接口"},
											ComboBox{
												AssignTo:     &g.ifaceCB,
												Editable:     false,
												Enabled:      false,
												Model:        g.ifaceModel(),
												CurrentIndex: 0,
											},
											Label{Text: "自定义 MAC"},
											LineEdit{AssignTo: &g.macEdit, Enabled: false, CueBanner: "00:07:29:55:35:57"},
										},
									},
									TabWidget{
										Pages: []TabPage{
											{
												Title:  "手动",
												Layout: VBox{},
												Children: []Widget{
													Composite{
														Layout: Grid{Columns: 2},
														Children: []Widget{
															Label{Text: "模式"},
															ComboBox{
																AssignTo:     &g.actionCB,
																Editable:     false,
																Model:        []string{"仅打开临时 telnet", "永久 telnet（重启服务）", "永久 telnet（重启设备）"},
																CurrentIndex: 0,
															},
														},
													},
													PushButton{
														AssignTo:  &g.runBtn,
														Text:      "运行",
														OnClicked: g.onRun,
													},
													Label{
														AssignTo:  &g.credLabel,
														Text:      "凭据将在此显示",
														TextColor: walk.RGB(0x66, 0x66, 0x66),
													},
													VSpacer{},
												},
											},
											{
												Title:  "一键配置",
												Layout: VBox{},
												Children: []Widget{
													GroupBox{
														Title:  "设备参数",
														Layout: Grid{Columns: 2},
														Children: []Widget{
															Label{Text: "SN"},
															LineEdit{AssignTo: &g.snEdit, CueBanner: "ZTEGXXXXXXXX"},
															Label{Text: "密码"},
															LineEdit{AssignTo: &g.onePassEdit, CueBanner: "认证 / 注册密码"},
															Label{Text: "类型"},
															ComboBox{
																AssignTo:     &g.ponCB,
																Editable:     false,
																Model:        []string{"GPON", "XGPON"},
																CurrentIndex: 0,
															},
															Label{Text: "区域"},
															ComboBox{
																AssignTo:     &g.regionCB,
																Editable:     false,
																Model:        regionModel(),
																CurrentIndex: onu.RegionIndexByID(onu.DefaultRegionID),
															},
														},
													},
													CheckBox{
														AssignTo: &g.ensureWANCB,
														Text:     "检查并创建 4034 TR069 / 4031 桥接连接（必要时新建）",
														Checked:  true,
													},
													GroupBox{
														Title:  "4031 桥接绑定端口",
														Layout: HBox{},
														Children: []Widget{
															CheckBox{AssignTo: &g.lan1CB, Text: "LAN1", Checked: true},
															CheckBox{AssignTo: &g.lan2CB, Text: "LAN2", Checked: true},
															CheckBox{AssignTo: &g.lan3CB, Text: "LAN3", Checked: true},
															CheckBox{AssignTo: &g.lan4CB, Text: "LAN4", Checked: true},
														},
													},
													CheckBox{
														AssignTo: &g.rebootAfterCB,
														Text:     "创建 WAN 连接后再重启一次（使连接立即生效）",
														Checked:  true,
													},
													CheckBox{
														AssignTo: &g.ensureRxCB,
														Text:     "RX 光功率超阈值时自动补偿（写 OPTICAL.RxOffset）",
														Checked:  true,
													},
													Composite{
														Layout: HBox{},
														Children: []Widget{
															Label{Text: "|RX| 阈值 (dB)"},
															LineEdit{AssignTo: &g.rxMaxEdit, Text: "25"},
															Label{Text: "目标 |RX| (dB)"},
															LineEdit{AssignTo: &g.rxTargetEdit, Text: "23"},
														},
													},
													PushButton{
														AssignTo:  &g.oneBtn,
														Text:      "一键配置",
														OnClicked: g.onOneClick,
													},
													Label{
														AssignTo:  &g.oneStatus,
														Text:      "流程: 临时telnet → 集采→重启 → 写SN/密码 → 区域→重启（全程只用临时telnet）",
														TextColor: walk.RGB(0x66, 0x66, 0x66),
													},
													VSpacer{},
												},
											},
											{
												Title:  "命令",
												Layout: VBox{},
												Children: []Widget{
													GroupBox{
														Title:  "telnet 控制台",
														Layout: Grid{Columns: 2},
														Children: []Widget{
															Label{Text: "用户名"},
															LineEdit{AssignTo: &g.cmdUserEdit, Text: "root"},
															Label{Text: "密码"},
															LineEdit{AssignTo: &g.cmdPassEdit, Text: "Zte521"},
														},
													},
													Composite{
														Layout: HBox{},
														Children: []Widget{
															PushButton{AssignTo: &g.cmdConnectBtn, Text: "连接", OnClicked: g.onCmdConnect},
															PushButton{AssignTo: &g.cmdDisconnectBtn, Text: "断开", Enabled: false, OnClicked: g.onCmdDisconnect},
														},
													},
													Label{
														AssignTo:  &g.cmdStatus,
														Text:      "未连接",
														TextColor: walk.RGB(0x66, 0x66, 0x66),
													},
													Composite{
														Layout: HBox{},
														Children: []Widget{
															LineEdit{
																AssignTo:  &g.cmdEdit,
																CueBanner: "输入命令，回车或点“执行”，如 sendcmd 1 DB show",
																OnKeyDown: func(key walk.Key) {
																	if key == walk.KeyReturn {
																		g.onCmdSend()
																	}
																},
															},
															PushButton{AssignTo: &g.cmdSendBtn, Text: "执行", OnClicked: g.onCmdSend},
														},
													},
													Label{
														Text:      "用永久 telnet（默认 root/Zte521）连接；命令与输出显示在右侧日志。",
														TextColor: walk.RGB(0x66, 0x66, 0x66),
													},
													VSpacer{},
												},
											},
											{
												Title:  "状态",
												Layout: VBox{},
												Children: []Widget{
													PushButton{
														AssignTo:  &g.statusRefreshBtn,
														Text:      "刷新状态",
														OnClicked: g.onStatusRefresh,
													},
													Label{
														AssignTo:  &g.statusHint,
														Text:      "点“刷新状态”读取一次；使用永久 telnet（root/Zte521）",
														TextColor: walk.RGB(0x66, 0x66, 0x66),
													},
													TextEdit{
														AssignTo:      &g.statusText,
														ReadOnly:      true,
														VScroll:       true,
														HScroll:       true,
														Font:          Font{Family: "NSimSun", PointSize: 10},
														CompactHeight: false,
														Text:          "等待读取…",
													},
												},
											},
										},
									},
									VSpacer{},
								},
							},
							Composite{
								Layout: VBox{},
								Children: []Widget{
									Label{Text: "日志输出"},
									TextEdit{
										AssignTo: &g.logView,
										ReadOnly: true,
										VScroll:  true,
										// Consolas has no CJK glyphs (Chinese shows as tofu), so use
										// NSimSun: a monospaced font that ships with Windows and
										// renders both ASCII and Chinese. A missing face falls back
										// to the default CJK-capable face rather than to tofu.
										Font: Font{Family: "NSimSun", PointSize: 10},
									},
								},
							},
						}, // end 光猫配置 page.Children
					},
					{
						Title:  "光猫查询",
						Layout: VBox{},
						Children: []Widget{
							Composite{
								Layout: Grid{Columns: 4},
								Children: []Widget{
									Label{Text: "工号"},
									LineEdit{
										AssignTo:  &g.queryLoginEdit,
										CueBanner: "loginName，如 tt_wangbang",
									},
									Label{Text: "账号"},
									LineEdit{
										AssignTo: &g.queryAccountEdit,
									},
								},
							},
							Composite{
								Layout: HBox{},
								Children: []Widget{
									Label{Text: "人脸图片"},
									LineEdit{
										AssignTo: &g.faceImageEdit,
										ReadOnly: true,
									},
									PushButton{AssignTo: &g.faceBrowseBtn, Text: "浏览…", OnClicked: g.onFaceBrowse},
									PushButton{AssignTo: &g.faceLoginBtn, Text: "人脸登录", OnClicked: g.onFaceLogin},
									Label{
										AssignTo:  &g.faceStatus,
										Text:      "未登录",
										TextColor: walk.RGB(0x66, 0x66, 0x66),
									},
									HSpacer{},
								},
							},
							Composite{
								Layout: HBox{},
								Children: []Widget{
									PushButton{
										AssignTo:  &g.queryRunBtn,
										Text:      "查询",
										OnClicked: g.onQueryRun,
									},
									PushButton{
										AssignTo:  &g.querySelAllBtn,
										Text:      "全选",
										OnClicked: g.onQuerySelectAll,
									},
									PushButton{
										AssignTo:  &g.querySelNoneBtn,
										Text:      "全不选",
										OnClicked: g.onQuerySelectNone,
									},
									PushButton{
										AssignTo: &g.querySelOfflineBtn,
										Text:     "选中非在线",
										OnClicked: func() {
											g.queryModel.SelectByPredicate(func(r *QueryRow) bool {
												return !r.IsOnline()
											})
										},
									},
									PushButton{
										AssignTo:  &g.queryFetchBtn,
										Text:      "获取详情",
										OnClicked: g.onQueryFetchDetails,
									},
									PushButton{
										AssignTo:  &g.queryExportBtn,
										Text:      "导出选中 CSV",
										OnClicked: g.onQueryExport,
									},
									PushButton{
										AssignTo:  &g.queryClearBtn,
										Text:      "清空",
										OnClicked: g.onQueryClear,
									},
									CheckBox{
										AssignTo: &g.queryOfflineOnlyCB,
										Text:     "只显示非在线",
										// OnClicked fires on every user click; using
										// OnCheckedChanged has been observed to miss
										// events in some walk builds.
										OnClicked: func() {
											on := g.queryOfflineOnlyCB.Checked()
											g.queryModel.SetShowOnlyOffline(on)
											// Force a repaint - PublishRowsReset alone
											// occasionally leaves stale rows visible.
											if g.queryTable != nil {
												g.queryTable.Invalidate()
											}
											g.queryHint.SetText(fmt.Sprintf("过滤 %v：显示 %d/%d 行",
												on, g.queryModel.RowCount(), len(g.queryModel.AllRows())))
										},
									},
									Label{
										AssignTo:  &g.queryHint,
										Text:      "token 自动保存到 %APPDATA%/zteonu/token.txt",
										TextColor: walk.RGB(0x66, 0x66, 0x66),
									},
									HSpacer{},
								},
							},
							TableView{
								AssignTo:         &g.queryTable,
								AlternatingRowBG: true,
								CheckBoxes:       true,
								MultiSelection:   true,
								ColumnsOrderable: true,
								Model:            g.queryModel,
								// Non-online rows get a light-red background so
								// the eye can pick out 掉电 / 未知原因不在线 devices
								// at a glance.
								StyleCell: func(style *walk.CellStyle) {
									idx := style.Row()
									rows := g.queryModel.Rows()
									if idx < 0 || idx >= len(rows) {
										return
									}
									if !rows[idx].IsOnline() {
										style.BackgroundColor = walk.RGB(0xff, 0xe0, 0xe0)
									}
								},
								Columns: []TableViewColumn{
									{Title: "查询账号", Width: 90},
									{Title: "ONU 序号", Width: 65},
									{Title: "状态", Width: 70},
									{Title: "认证类型", Width: 60},
									{Title: "认证信息", Width: 120},
									{Title: "客户号码", Width: 95},
									{Title: "最后离线时间", Width: 130},
									{Title: "密码", Width: 90},
									{Title: "账号状态", Width: 60},
									{Title: "ONU 运行状态", Width: 90},
									{Title: "PON 口名称", Width: 200},
									{Title: "分光器名称", Width: 200},
									{Title: "最后上线时间", Width: 130},
									{Title: "最后离线原因", Width: 130},
								},
							},
						},
					},
					{
						Title:  "绑定信息查询",
						Layout: VBox{},
						Children: []Widget{
							GroupBox{
								Title:  "查询设置",
								Layout: VBox{},
								Children: []Widget{
									Composite{
										Layout: Grid{Columns: 4},
										Children: []Widget{
											Label{Text: "工号"},
											LineEdit{
												AssignTo:   &g.bindLoginEdit,
												CueBanner:  "loginName（与光猫查询共享 SSO 缓存）",
												ColumnSpan: 3,
											},
											Label{Text: "并发线程"},
											LineEdit{
												AssignTo: &g.bindConcurrencyEdit,
												Text:     "4",
												MaxSize:  Size{Width: 60},
											},
											Label{Text: "(compId=317, 单账号绑定信息视图)", ColumnSpan: 2},
										},
									},
									Label{Text: "账号列表（每行一个，可粘贴多行）"},
									TextEdit{
										AssignTo: &g.bindAccountsText,
										VScroll:  true,
										MinSize:  Size{Height: 90},
										Font:     Font{Family: "NSimSun", PointSize: 10},
									},
								},
							},
							Composite{
								Layout: HBox{},
								Children: []Widget{
									PushButton{AssignTo: &g.bindRunBtn, Text: "批量查询", OnClicked: g.onBindInfoRun},
									PushButton{AssignTo: &g.bindSelAllBtn, Text: "全选", OnClicked: func() { g.bindModel.SelectAll(true) }},
									PushButton{AssignTo: &g.bindSelNoneBtn, Text: "全不选", OnClicked: func() { g.bindModel.SelectAll(false) }},
									PushButton{AssignTo: &g.bindExportBtn, Text: "导出选中 CSV", OnClicked: g.onBindInfoExport},
									PushButton{AssignTo: &g.bindClearBtn, Text: "清空", OnClicked: func() { g.bindModel.Reset(); g.bindHint.SetText("已清空") }},
									Label{
										AssignTo:  &g.bindHint,
										Text:      "返回：账号 / 状态 / 用户名 / 带宽 / 地市 / 绑定信息 / 更新时间",
										TextColor: walk.RGB(0x66, 0x66, 0x66),
									},
									HSpacer{},
								},
							},
							TableView{
								AssignTo:         &g.bindTable,
								AlternatingRowBG: true,
								CheckBoxes:       true,
								MultiSelection:   true,
								ColumnsOrderable: true,
								Model:            g.bindModel,
								Columns: []TableViewColumn{
									{Title: "账号", Width: 120},
									{Title: "状态", Width: 60},
									{Title: "用户名", Width: 110},
									{Title: "带宽", Width: 130},
									{Title: "地市", Width: 60},
									{Title: "绑定信息", Width: 300},
									{Title: "更新时间", Width: 120},
									{Title: "备注", Width: 200},
								},
							},
						},
					},
				}, // end TabWidget.Pages
			}, // end outer TabWidget
		}, // end MainWindow.Children (outer VBox)
	}).Create(); err != nil {
		walk.MsgBox(nil, "错误", "无法创建窗口: "+err.Error(), walk.MsgBoxIconError)
		return
	}

	g.appendLog(fmt.Sprintf("%s\r\n就绪 - 设置参数后点击运行\r\n", version.Line()))
	// Prime the CMCC-query 工号, face image path and combineToken from disk
	// so users don't re-enter them each launch.
	if saved := loadSavedLoginName(); saved != "" {
		g.queryLoginEdit.SetText(saved)
	}
	if saved := loadSavedFaceImage(); saved != "" {
		g.faceImageEdit.SetText(saved)
	}
	if saved := loadSavedCombineToken(); saved != "" {
		g.combineToken = saved
		g.faceStatus.SetText("已加载缓存 token")
	}
	g.mw.Run()
}

// onFaceBrowse opens a file dialog for the user to pick a face-login image.
// Image filename (without extension) is treated as the 工号 - when the top
// 工号 field is empty, it's auto-filled from the basename.
func (g *gui) onFaceBrowse() {
	dlg := new(walk.FileDialog)
	dlg.Title = "选择人脸图片（文件名即工号）"
	dlg.Filter = "图片 (*.jpg;*.jpeg;*.png)|*.jpg;*.jpeg;*.png|所有文件 (*.*)|*.*"
	dlg.FilterIndex = 1
	if ok, err := dlg.ShowOpen(g.mw); err != nil {
		walk.MsgBox(g.mw, "错误", err.Error(), walk.MsgBoxIconError)
		return
	} else if !ok {
		return
	}
	g.faceImageEdit.SetText(dlg.FilePath)
	saveFaceImagePath(dlg.FilePath)
	if strings.TrimSpace(g.queryLoginEdit.Text()) == "" {
		name := strings.TrimSuffix(filepath.Base(dlg.FilePath), filepath.Ext(dlg.FilePath))
		g.queryLoginEdit.SetText(name)
		saveLoginName(name)
	}
}

// onFaceLogin runs the face-login sequence (csrf → upload → login) and stores
// the resulting combineToken. loginName is derived from the image basename.
func (g *gui) onFaceLogin() {
	imagePath := strings.TrimSpace(g.faceImageEdit.Text())
	if imagePath == "" {
		walk.MsgBox(g.mw, "提示", "请先选择人脸图片", walk.MsgBoxIconWarning)
		return
	}
	loginName := strings.TrimSuffix(filepath.Base(imagePath), filepath.Ext(imagePath))
	if loginName == "" {
		walk.MsgBox(g.mw, "提示", "图片文件名不能为空", walk.MsgBoxIconWarning)
		return
	}
	if strings.TrimSpace(g.queryLoginEdit.Text()) == "" {
		g.queryLoginEdit.SetText(loginName)
		saveLoginName(loginName)
	}
	g.faceStatus.SetText("登录中……")
	g.faceLoginBtn.SetEnabled(false)

	go func() {
		tok, err := query.NewFaceLoginClient().Login(loginName, imagePath)
		g.mw.Synchronize(func() {
			g.faceLoginBtn.SetEnabled(true)
			if err != nil {
				g.faceStatus.SetText("登录失败：" + err.Error())
				return
			}
			g.combineToken = tok
			saveCombineToken(tok)
			g.faceStatus.SetText(fmt.Sprintf("登录成功（%s）", loginName))
		})
	}()
}

// syncMacFields enables the interface / MAC input that matches the selected
// source and disables the other.
func (g *gui) syncMacFields() {
	idx := g.macModeCB.CurrentIndex()
	g.ifaceCB.SetEnabled(idx == 1)
	g.macEdit.SetEnabled(idx == 2)
}

// guiLogWriter forwards flow output to the log TextEdit on the UI thread.
type guiLogWriter struct{ g *gui }

func (w guiLogWriter) Write(p []byte) (int, error) {
	w.g.appendLog(normalizeNewlines(string(p)))
	return len(p), nil
}

// appendLog appends text to the log view from any goroutine.
func (g *gui) appendLog(s string) {
	g.mw.Synchronize(func() { g.logView.AppendText(s) })
}

// collectOptions gathers the device and client settings shared by both flows.
func (g *gui) collectOptions() onu.Options {
	opts := onu.Options{
		User:       strings.TrimSpace(g.userEdit.Text()),
		Pass:       g.passEdit.Text(),
		IP:         strings.TrimSpace(g.ipEdit.Text()),
		HTTPPort:   atoiDefault(g.httpPortEdit.Text(), 80),
		TelnetPort: atoiDefault(g.telnetPortEdit.Text(), 23),
		Log:        guiLogWriter{g},
	}
	switch g.macModeCB.CurrentIndex() {
	case 1:
		if i := g.ifaceCB.CurrentIndex(); i >= 0 && i < len(g.ifaces) {
			opts.Iface = g.ifaces[i].Name
		}
	case 2:
		opts.Mac = strings.TrimSpace(g.macEdit.Text())
	}
	return opts
}

// setRunning toggles the action buttons so only one flow runs at a time, and
// disables the console while a flow holds the device.
func (g *gui) setRunning(on bool) {
	g.running = on
	g.runBtn.SetEnabled(!on)
	g.oneBtn.SetEnabled(!on)
	if on {
		g.runBtn.SetText("运行中…")
		g.oneBtn.SetText("执行中…")
		g.cmdConnectBtn.SetEnabled(false)
		g.cmdDisconnectBtn.SetEnabled(false)
		g.cmdSendBtn.SetEnabled(false)
	} else {
		g.runBtn.SetText("运行")
		g.oneBtn.SetText("一键配置")
		g.cmdSendBtn.SetEnabled(true)
		g.setConsoleControls(g.console != nil)
	}
}

// setConsoleControls reflects the console connection state on its buttons.
func (g *gui) setConsoleControls(connected bool) {
	g.cmdConnectBtn.SetEnabled(!connected)
	g.cmdDisconnectBtn.SetEnabled(connected)
	if connected {
		g.cmdStatus.SetText("已连接")
	} else {
		g.cmdStatus.SetText("未连接")
	}
}

func (g *gui) onCmdConnect() {
	if g.running {
		walk.MsgBox(g.mw, "提示", "正在执行流程，请稍候", walk.MsgBoxIconWarning)
		return
	}
	if g.console != nil {
		return
	}
	ip := strings.TrimSpace(g.ipEdit.Text())
	port := atoiDefault(g.telnetPortEdit.Text(), 23)
	user := strings.TrimSpace(g.cmdUserEdit.Text())
	pass := g.cmdPassEdit.Text()

	g.cmdConnectBtn.SetEnabled(false)
	g.cmdStatus.SetText("连接中…")
	g.appendLog(fmt.Sprintf("正在连接 telnet %s:%d …\r\n", ip, port))

	go func() {
		t, err := tnet.New(user, pass, ip, port)
		if err == nil {
			err = t.Login()
		}
		g.mw.Synchronize(func() {
			if err != nil {
				if t != nil {
					t.Conn.Close()
				}
				g.appendLog("[错误] 连接失败：" + err.Error() + "\r\n")
				g.setConsoleControls(false)
				return
			}
			g.console = t
			g.appendLog("telnet 已连接\r\n")
			g.setConsoleControls(true)
		})
	}()
}

func (g *gui) onCmdDisconnect() {
	if g.console == nil {
		return
	}
	g.console.Conn.Close()
	g.console = nil
	g.appendLog("telnet 已断开\r\n")
	g.setConsoleControls(false)
}

func (g *gui) onCmdSend() {
	if g.consoleBusy || g.running {
		return
	}
	cmd := strings.TrimSpace(g.cmdEdit.Text())
	if cmd == "" {
		return
	}
	if g.console == nil {
		walk.MsgBox(g.mw, "提示", "请先点击“连接”", walk.MsgBoxIconWarning)
		return
	}

	g.consoleBusy = true
	g.cmdSendBtn.SetEnabled(false)
	g.appendLog("> " + cmd + "\r\n")
	g.cmdEdit.SetText("")

	go func() {
		out, err := g.console.Exec(cmd)
		g.mw.Synchronize(func() {
			g.consoleBusy = false
			g.cmdSendBtn.SetEnabled(true)
			if err != nil {
				g.appendLog("[错误] " + err.Error() + "\r\n")
				// The session likely dropped (e.g. the command rebooted the
				// device); mark the console disconnected.
				if g.console != nil {
					g.console.Conn.Close()
					g.console = nil
				}
				g.setConsoleControls(false)
				return
			}
			if s := strings.TrimSpace(out); s != "" {
				g.appendLog(normalizeNewlines(s) + "\r\n")
			}
		})
	}()
}

// onStatusRefresh reads a fresh status snapshot from the ONU and shows it in
// the status text panel. Reuses the command console's connection when it is
// open (avoids reconnecting); otherwise dials a short-lived permanent telnet
// session with root/Zte521 just for this fetch.
func (g *gui) onStatusRefresh() {
	if g.running {
		walk.MsgBox(g.mw, "提示", "流程正在执行，请稍后再刷新", walk.MsgBoxIconWarning)
		return
	}
	ip := strings.TrimSpace(g.ipEdit.Text())
	telnetPort := atoiDefault(g.telnetPortEdit.Text(), 23)

	g.statusRefreshBtn.SetEnabled(false)
	g.statusHint.SetText("读取中……")
	g.statusText.SetText("")

	// Snapshot the console pointer so the goroutine sees a stable value.
	consoleSess := g.console

	go func() {
		var (
			out         string
			err         error
			ownedTelnet *tnet.Telnet
		)
		if consoleSess != nil {
			out, err = onu.FetchStatus(consoleSess)
		} else {
			ownedTelnet, err = tnet.New("root", "Zte521", ip, telnetPort)
			if err == nil {
				if lerr := ownedTelnet.Login(); lerr != nil {
					ownedTelnet.Conn.Close()
					ownedTelnet = nil
					err = fmt.Errorf("登录失败（永久 telnet 可能未开启）: %w", lerr)
				}
			}
			if err == nil {
				out, err = onu.FetchStatus(ownedTelnet)
			}
		}
		if ownedTelnet != nil {
			ownedTelnet.Conn.Close()
		}
		g.mw.Synchronize(func() {
			g.statusRefreshBtn.SetEnabled(true)
			if err != nil {
				g.statusHint.SetText("读取失败")
				g.statusText.SetText("[错误] " + err.Error() + "\r\n\r\n" +
					"提示：先在“手动”页选“永久 telnet（重启服务）”开启永久 telnet；\r\n" +
					"或在“命令”页点“连接”后再点“刷新状态”。")
				return
			}
			g.statusHint.SetText("已读取")
			g.statusText.SetText(normalizeNewlines(out))
		})
	}()
}

// appDataPath returns %APPDATA%/zteonu/<name>. Falls back to the exe dir when
// APPDATA is unset (portable use).
func appDataPath(name string) string {
	base := os.Getenv("APPDATA")
	if base == "" {
		if exe, err := os.Executable(); err == nil {
			base = filepath.Dir(exe)
		} else {
			base = "."
		}
	}
	dir := filepath.Join(base, "zteonu")
	_ = os.MkdirAll(dir, 0o755)
	return filepath.Join(dir, name)
}

// loginNameFilePath / tokenCachePath localize the artifacts we persist for
// the 光猫查询 tab. combineToken (from face-login) is short-lived; we still
// save it so the user avoids a re-login if the app restarts within its
// validity window - on 401 the on-disk value is refreshed via face-login.
func loginNameFilePath() string    { return appDataPath("loginname.txt") }
func tokenCachePath() string       { return appDataPath("sso_token.json") }
func faceImageFilePath() string    { return appDataPath("face_image.txt") }
func combineTokenFilePath() string { return appDataPath("combine_token.txt") }

func loadSavedFaceImage() string {
	b, err := os.ReadFile(faceImageFilePath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func saveFaceImagePath(p string) {
	_ = os.WriteFile(faceImageFilePath(), []byte(strings.TrimSpace(p)), 0o600)
}

func loadSavedCombineToken() string {
	b, err := os.ReadFile(combineTokenFilePath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func saveCombineToken(tok string) {
	_ = os.WriteFile(combineTokenFilePath(), []byte(strings.TrimSpace(tok)), 0o600)
}

func loadSavedLoginName() string {
	b, err := os.ReadFile(loginNameFilePath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func saveLoginName(name string) {
	_ = os.WriteFile(loginNameFilePath(), []byte(strings.TrimSpace(name)), 0o600)
}

// parseAccounts splits a multi-line text into a de-duplicated list of accounts.
func parseAccounts(text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		line = strings.Trim(line, ",;\t\r")
		line = strings.TrimSpace(line)
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, line)
	}
	return out
}

// onQueryRun queries scene/security/forward for one account and expands the
// response into a set of same-PON neighbor rows. Auth path: 工号 → cached JWT
// (if fresh) or fresh SSO exchange → RSA-encrypted POST.
func (g *gui) onQueryRun() {
	if g.queryBusy {
		return
	}
	loginName := strings.TrimSpace(g.queryLoginEdit.Text())
	account := strings.TrimSpace(g.queryAccountEdit.Text())
	if loginName == "" {
		walk.MsgBox(g.mw, "提示", "请先填写工号 (loginName)", walk.MsgBoxIconWarning)
		return
	}
	if account == "" {
		walk.MsgBox(g.mw, "提示", "请填写账号", walk.MsgBoxIconWarning)
		return
	}
	saveLoginName(loginName)

	g.queryBusy = true
	g.queryRunBtn.SetEnabled(false)
	g.queryRunBtn.SetText("查询中…")
	g.queryModel.Reset()
	g.queryHint.SetText("正在换取 access_token……")

	go func() {
		ssoCli := sso.New()
		tok, err := sso.EnsureToken(loginName, tokenCachePath(), ssoCli)
		if err != nil {
			g.mw.Synchronize(func() {
				g.queryBusy = false
				g.queryRunBtn.SetEnabled(true)
				g.queryRunBtn.SetText("查询")
				g.queryHint.SetText("SSO 失败：" + err.Error())
			})
			return
		}

		client, err := query.NewForwardClient(tok)
		if err != nil {
			g.mw.Synchronize(func() {
				g.queryBusy = false
				g.queryRunBtn.SetEnabled(true)
				g.queryRunBtn.SetText("查询")
				g.queryHint.SetText("初始化查询客户端失败：" + err.Error())
			})
			return
		}
		// On 401 the client asks TokenProvider for a fresh token; invalidate
		// the disk cache first so we don't loop on a stale JWT.
		client.TokenProvider = func() (string, error) {
			sso.Invalidate(tokenCachePath())
			return sso.EnsureToken(loginName, tokenCachePath(), ssoCli)
		}

		resp, qerr := client.QueryOneWithPon(account, "")
		rows := buildQueryRowsFromForward(account, resp, qerr)
		g.mw.Synchronize(func() {
			g.queryModel.AppendMany(rows)
			g.queryBusy = false
			g.queryRunBtn.SetEnabled(true)
			g.queryRunBtn.SetText("查询")
			g.queryHint.SetText(fmt.Sprintf("完成：共 %d 台设备", len(rows)))
		})
	}()
}

func (g *gui) onQuerySelectAll()  { g.queryModel.SelectAll(true) }
func (g *gui) onQuerySelectNone() { g.queryModel.SelectAll(false) }

func (g *gui) onQueryClear() {
	g.queryModel.Reset()
	g.queryHint.SetText("已清空")
}

func (g *gui) onQueryExport() {
	if g.queryModel.RowCount() == 0 {
		walk.MsgBox(g.mw, "提示", "结果为空", walk.MsgBoxIconWarning)
		return
	}
	// Count selected first so users don't get an empty file.
	selCount := 0
	for _, r := range g.queryModel.Rows() {
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
	dlg.FilePath = "onu-query.csv"
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
	n, err := g.queryModel.ExportSelectedCSV(path)
	if err != nil {
		walk.MsgBox(g.mw, "错误", err.Error(), walk.MsgBoxIconError)
		return
	}
	g.queryHint.SetText(fmt.Sprintf("已导出 %d 条到 %s", n, path))
}

func (g *gui) onRun() {
	if g.running {
		return
	}

	action := g.actionCB.CurrentIndex()
	opts := g.collectOptions()

	g.setRunning(true)
	g.logView.SetText("")
	g.credLabel.SetText("凭据将在此显示")

	go func() {
		user, pass, ok := g.execFlow(opts, action)
		g.mw.Synchronize(func() {
			g.setRunning(false)
			if ok {
				g.credLabel.SetText(fmt.Sprintf("user: %s    pass: %s", user, pass))
			}
		})
	}()
}

func (g *gui) onOneClick() {
	if g.running {
		return
	}

	// SN and password are both optional: an empty field just skips its setmac
	// commands. Confirm when both are empty so it is not an accidental no-write.
	sn := strings.TrimSpace(g.snEdit.Text())
	pass := strings.TrimSpace(g.onePassEdit.Text())
	if sn == "" && pass == "" {
		if walk.MsgBox(g.mw, "确认", "SN 和密码都为空，将只设置集采和区域。是否继续？",
			walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) != walk.DlgCmdYes {
			return
		}
	}

	// Compute PortMask from LAN checkboxes: bit0=LAN1..bit3=LAN4.
	mask := 0
	for i, cb := range []*walk.CheckBox{g.lan1CB, g.lan2CB, g.lan3CB, g.lan4CB} {
		if cb.Checked() {
			mask |= 1 << i
		}
	}
	if mask == 0 {
		mask = 15 // fallback: bind all if user unchecked everything
	}

	rxMax := parseFloatDefault(g.rxMaxEdit.Text(), 25)
	rxTarget := parseFloatDefault(g.rxTargetEdit.Text(), 23)

	o := onu.OneClickOptions{
		Options:           g.collectOptions(),
		SN:                sn,
		Password:          pass,
		PON:               onu.PONType(g.ponCB.CurrentIndex()),
		RegionID:          onu.Regions[g.regionCB.CurrentIndex()].ID,
		EnsureWAN:         g.ensureWANCB.Checked(),
		BridgePortMask:    mask,
		RebootAfterEnsure: g.rebootAfterCB.Checked(),
		EnsureRxOffset:    g.ensureRxCB.Checked(),
		RxMaxAbsDBm:       rxMax,
		RxTargetAbsDBm:    rxTarget,
	}

	g.setRunning(true)
	g.logView.SetText("")
	g.oneStatus.SetText("一键配置进行中…")

	go func() {
		err := onu.RunOneClick(o)
		g.mw.Synchronize(func() {
			g.setRunning(false)
			if err != nil {
				g.appendLog("\r\n[错误] " + err.Error() + "\r\n")
				g.oneStatus.SetText("一键配置失败")
			} else {
				g.oneStatus.SetText("一键配置完成，设备重启中")
			}
		})
	}()
}

// execFlow runs the webFac flow and any permanent-telnet step, returning the
// active credentials and whether the run succeeded.
func (g *gui) execFlow(opts onu.Options, action int) (user, pass string, ok bool) {
	t, tlUser, tlPass, err := onu.OpenTempTelnet(opts)
	if err != nil {
		g.appendLog("\r\n[error] " + err.Error() + "\r\n")
		return "", "", false
	}
	defer t.Conn.Close()
	g.appendLog("telnet 验证通过，临时工厂 telnet 已开启\r\n")

	switch action {
	case 1:
		if err := onu.SolidifyAndRestart(t, opts.IP, opts.TelnetPort, opts.Log); err != nil {
			g.appendLog("\r\n[错误] " + err.Error() + "\r\n")
			return "", "", false
		}
		return "root", "Zte521", true
	case 2:
		if err := onu.SolidifyAndReboot(t, opts.Log); err != nil {
			g.appendLog("\r\n[错误] " + err.Error() + "\r\n")
			return "", "", false
		}
		return "root", "Zte521", true
	default:
		return tlUser, tlPass, true
	}
}

func atoiDefault(s string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && n > 0 {
		return n
	}
	return def
}

// parseFloatDefault parses a decimal number from user text, falling back to def
// on any parse error or non-positive value.
func parseFloatDefault(s string, def float64) float64 {
	if v, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil && v > 0 {
		return v
	}
	return def
}

// normalizeNewlines converts any newline style to the CRLF that the Win32
// multiline edit control expects.
func normalizeNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\n", "\r\n")
}

// hideOwnConsole hides the console window, but only when this process is the
// sole owner of it (i.e. the app was double-clicked and Windows spawned a
// console for it). When launched from an existing shell, more than one process
// is attached and the shell is left untouched.
func hideOwnConsole() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getConsoleProcessList := kernel32.NewProc("GetConsoleProcessList")
	getConsoleWindow := kernel32.NewProc("GetConsoleWindow")

	var pids [4]uint32
	n, _, _ := getConsoleProcessList.Call(uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)))
	if n != 1 {
		return // launched from an existing console, or none attached
	}

	hwnd, _, _ := getConsoleWindow.Call()
	if hwnd == 0 {
		return
	}
	const swHide = 0
	syscall.NewLazyDLL("user32.dll").NewProc("ShowWindow").Call(hwnd, swHide)
}
