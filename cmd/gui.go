//go:build windows

package cmd

import (
	"fmt"
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
	snEdit, onePassEdit *walk.LineEdit
	ponCB, regionCB     *walk.ComboBox
	reopenCB            *walk.CheckBox
	oneBtn              *walk.PushButton
	oneStatus           *walk.Label

	// telnet command console
	cmdUserEdit, cmdPassEdit, cmdEdit           *walk.LineEdit
	cmdConnectBtn, cmdDisconnectBtn, cmdSendBtn *walk.PushButton
	cmdStatus                                   *walk.Label
	console                                     *tnet.Telnet
	consoleBusy                                 bool

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

	if err := (MainWindow{
		AssignTo: &g.mw,
		Title:    "ZTE ONU 工具",
		MinSize:  Size{Width: 860, Height: 540},
		Size:     Size{Width: 960, Height: 620},
		Layout:   HBox{},
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
										AssignTo: &g.reopenCB,
										Text:     "完成后重新开启 telnet（便于重连）",
										Checked:  true,
									},
									PushButton{
										AssignTo:  &g.oneBtn,
										Text:      "一键配置",
										OnClicked: g.onOneClick,
									},
									Label{
										AssignTo:  &g.oneStatus,
										Text:      "流程: 永久telnet → 集采→重启 → 写SN/密码 → 区域→重启 →(重开telnet)",
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
		},
	}).Create(); err != nil {
		walk.MsgBox(nil, "错误", "无法创建窗口: "+err.Error(), walk.MsgBoxIconError)
		return
	}

	g.appendLog(fmt.Sprintf("%s\r\n就绪 - 设置参数后点击运行\r\n", version.Line()))
	g.mw.Run()
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

	o := onu.OneClickOptions{
		Options:      g.collectOptions(),
		SN:           sn,
		Password:     pass,
		PON:          onu.PONType(g.ponCB.CurrentIndex()),
		RegionID:     onu.Regions[g.regionCB.CurrentIndex()].ID,
		ReopenTelnet: g.reopenCB.Checked(),
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
