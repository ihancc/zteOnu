package onu

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/septrum101/zteOnu/app/telnet"
)

// PON state names in Chinese: O1 initial, O2 standby, O3 SN transmission,
// O4 ranging, O5 operation (正常工作), O6 popup, O7 emergency stop.
var ponStateDescCN = map[string]string{
	"O1": "初始化",
	"O2": "待机（收到下行光）",
	"O3": "发送 SN（等待 OLT 认证）",
	"O4": "测距中",
	"O5": "正常工作",
	"O6": "告警弹出",
	"O7": "紧急停止",
}

// FetchStatus reads a variety of live values from the ONU via the given open
// permanent (root/Zte521) telnet session and returns them as a formatted
// Chinese status report suitable for a UI text panel.
func FetchStatus(t *telnet.Telnet) (string, error) {
	var b strings.Builder

	// --- Device info (from DB) ---
	dev := parseDMs(mustExec(t, "sendcmd 1 DB p DevInfo"))
	sb(&b, "== 设备信息 ==")
	sb(&b, "  型号        : %s  (硬件 %s，软件 %s)", d(dev, "ModelName"), d(dev, "HardwareVer"), d(dev, "SoftwareVer"))
	sb(&b, "  厂商        : %s  (OUI %s)", d(dev, "ManuFacturer"), d(dev, "ManuFacturerOui"))
	sb(&b, "  SN          : %s", d(dev, "SerialNumber"))

	// --- PON link ---
	state := extractPONState(mustExec(t, "gpontest -gstate"))
	desc := ponStateDescCN[state]
	if desc == "" {
		desc = "未知"
	}
	sb(&b, "")
	sb(&b, "== PON 链路 ==")
	sb(&b, "  状态        : %s  (%s)", state, desc)
	stat := parsePonStat(mustExec(t, "gpontest -gponstat"))
	if len(stat) > 0 {
		sb(&b, "  TX 包/字节  : %s / %s", stat["TxEthPackets"], stat["TxEthBytes"])
		sb(&b, "  RX 包/字节  : %s / %s", stat["RxEthPackets"], stat["RxEthBytes"])
		sb(&b, "  RX 丢包     : %s", stat["RxEthDropPackets"])
		sb(&b, "  FCS 错误    : %s", stat["RxFcsErrPackets"])
		sb(&b, "  HEC 错误    : %s", stat["RxHecError"])
	}

	// --- Optical module ---
	op := parseOpticalPara(mustExec(t, "opticaltst -getpara"))
	vinfo := parseTransceiverInfo(mustExec(t, "opticaltst -gettransceiverinfo"))
	sb(&b, "")
	sb(&b, "== 光模块 ==")
	sb(&b, "  厂商/型号   : %s  PN=%s  SN=%s", vinfo["VendorName"], vinfo["VendorPN"], vinfo["VendorSN"])
	sb(&b, "  生产日期    : %s", vinfo["DateCde"])
	if v, ok := op["temp"]; ok {
		sb(&b, "  温度        : %.1f °C", float64(v)/256.0)
	}
	if v, ok := op["SupplyVoltage"]; ok {
		sb(&b, "  电压        : %.3f V", float64(v)*0.1/1000.0)
	}
	if v, ok := op["TXBiasCurrent"]; ok {
		sb(&b, "  TX 偏置电流 : %.2f mA", float64(v)*2.0/1000.0)
	}
	if v, ok := op["TXPower"]; ok {
		sb(&b, "  TX 发送功率 : %+.2f dBm", rawToDBm(v))
	}
	if v, ok := op["RXPower"]; ok {
		sb(&b, "  RX 接收功率 : %+.2f dBm", rawToDBm(v))
	}
	// Show whether an RxOffset compensation is currently applied.
	opDB := parseDMs(mustExec(t, "sendcmd 1 DB p OPTICAL"))
	if off, err := strconv.Atoi(d(opDB, "RxOffset")); err == nil && off != 0 {
		sb(&b, "  RxOffset    : %d  (约 %+.2f dB 显示补偿; RxCompEn=%s)", off, float64(off)/10000.0, d(opDB, "RxCompEn"))
	}

	// --- WAN nbif0 ---
	nbif0 := mustExec(t, "ifconfig nbif0")
	sb(&b, "")
	sb(&b, "== WAN 侧 (nbif0) ==")
	sb(&b, "  IP          : %s", ifconfigIP(nbif0))
	sb(&b, "  MAC         : %s", ifconfigMAC(nbif0))
	sb(&b, "  MTU         : %s", ifconfigMTU(nbif0))
	sb(&b, "  链路状态    : %s", ifconfigLinkState(nbif0))

	// --- LAN br0 ---
	br0 := mustExec(t, "ifconfig br0")
	sb(&b, "")
	sb(&b, "== LAN 侧 (br0) ==")
	sb(&b, "  IP          : %s", ifconfigIP(br0))
	sb(&b, "  MAC         : %s", ifconfigMAC(br0))

	return b.String(), nil
}

// sb is a shortcut for `fmt.Fprintf(b, format+"\r\n", args...)` — the GUI text
// control (walk) and Windows shells both expect CRLF.
func sb(b *strings.Builder, format string, a ...any) {
	fmt.Fprintf(b, format+"\r\n", a...)
}

// mustExec runs a command and returns its output; on error it returns the
// empty string so the caller's parsers just miss that block gracefully.
func mustExec(t *telnet.Telnet, cmd string) string {
	out, err := t.Exec(cmd)
	if err != nil {
		return ""
	}
	return out
}

// --- Parsers ---

// parseDMs turns a DB `p` output (`<DM name="..." val="..."/>`) into a map.
// Uses the same DM regex as wan.go.
func parseDMs(dump string) map[string]string {
	m := make(map[string]string)
	for _, mm := range dmRE.FindAllStringSubmatch(dump, -1) {
		m[mm[1]] = mm[2]
	}
	return m
}

// d returns the value or "-" when the map has no such key.
func d(m map[string]string, key string) string {
	if v, ok := m[key]; ok && v != "" {
		return v
	}
	return "-"
}

var stateRE = regexp.MustCompile(`gpon state is \[(\w+)\]`)

func extractPONState(out string) string {
	if m := stateRE.FindStringSubmatch(out); m != nil {
		return m[1]
	}
	return "?"
}

// parsePonStat parses lines like "TxBroadcastPackets     : 166" from
// `gpontest -gponstat`.
func parsePonStat(out string) map[string]string {
	m := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		if i := strings.Index(line, ":"); i > 0 {
			k := strings.TrimSpace(line[:i])
			v := strings.TrimSpace(line[i+1:])
			// only keep keys that look like ONU counters (letters + digits)
			if k != "" && v != "" && !strings.ContainsAny(k, "$#/") {
				m[k] = v
			}
		}
	}
	return m
}

// parseOpticalPara parses "  optical XXX=NNN" lines from opticaltst -getpara.
var opRE = regexp.MustCompile(`optical\s+(\w+)\s*=\s*(-?\d+)`)

func parseOpticalPara(out string) map[string]int {
	m := make(map[string]int)
	for _, mm := range opRE.FindAllStringSubmatch(out, -1) {
		n, _ := strconv.Atoi(mm[2])
		m[mm[1]] = n
	}
	return m
}

// parseTransceiverInfo parses "VendorName is XXX" lines from
// opticaltst -gettransceiverinfo.
var tinfoRE = regexp.MustCompile(`(\w+)\s+is\s+(\S.*?)\s*$`)

func parseTransceiverInfo(out string) map[string]string {
	m := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if mm := tinfoRE.FindStringSubmatch(line); mm != nil {
			m[mm[1]] = mm[2]
		}
	}
	return m
}

// --- ifconfig helpers ---

var (
	inetRE = regexp.MustCompile(`inet addr:(\S+)\s+.*Mask:(\S+)`)
	hwRE   = regexp.MustCompile(`HWaddr\s+(\S+)`)
	mtuRE  = regexp.MustCompile(`MTU:(\d+)`)
)

func ifconfigIP(ic string) string {
	if m := inetRE.FindStringSubmatch(ic); m != nil {
		return fmt.Sprintf("%s / %s", m[1], m[2])
	}
	return "无 IPv4"
}

func ifconfigMAC(ic string) string {
	if m := hwRE.FindStringSubmatch(ic); m != nil {
		return m[1]
	}
	return "-"
}

func ifconfigMTU(ic string) string {
	if m := mtuRE.FindStringSubmatch(ic); m != nil {
		return m[1]
	}
	return "-"
}

func ifconfigLinkState(ic string) string {
	if strings.Contains(ic, "RUNNING") {
		return "UP"
	}
	if strings.Contains(ic, "UP ") {
		return "配置 UP 但未 RUNNING"
	}
	return "DOWN"
}
