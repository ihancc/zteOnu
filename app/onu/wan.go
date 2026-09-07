package onu

import (
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/septrum101/zteOnu/app/telnet"
)

// WAN check/create step: after 集采 + region config the ONU may lose the WAN
// connections needed for management (TR-069 on VLAN 4034) and the bridged
// internet service (VLAN 4031). This step reads the WANC table, checks whether
// each is present, and creates the missing one(s) by replaying the exact field
// layout captured from a working reference device (see comments below).

// wancRow is one parsed row of the WANC (WAN Connection) DB table.
type wancRow struct {
	viewName    string
	strServList string
	connType    int
	wancType    int
	vlanID      int
	wancIndex   int
	name        string
}

// dmRE extracts one <DM name="..." val="..."/> pair from device DB output.
var dmRE = regexp.MustCompile(`<DM name="([^"]+)" val="([^"]*)"/>`)

// rowRE splits a table dump into its <Row>...</Row> chunks.
var rowRE = regexp.MustCompile(`(?s)<Row No="(\d+)">(.*?)</Row>`)

// parseWANC parses `sendcmd 1 DB p WANC` output into rows.
func parseWANC(dump string) []wancRow {
	var out []wancRow
	for _, m := range rowRE.FindAllStringSubmatch(dump, -1) {
		body := m[2]
		r := wancRow{}
		for _, dm := range dmRE.FindAllStringSubmatch(body, -1) {
			switch dm[1] {
			case "ViewName":
				r.viewName = dm[2]
			case "StrServList":
				r.strServList = dm[2]
			case "ConnType":
				r.connType, _ = strconv.Atoi(dm[2])
			case "WANCType":
				r.wancType, _ = strconv.Atoi(dm[2])
			case "VLANID":
				r.vlanID, _ = strconv.Atoi(dm[2])
			case "WancIndex":
				r.wancIndex, _ = strconv.Atoi(dm[2])
			case "WANCName":
				r.name = dm[2]
			}
		}
		out = append(out, r)
	}
	return out
}

// has4034TR069 reports whether any WANC row is TR-069 on VLAN 4034.
func has4034TR069(rows []wancRow) bool {
	for _, r := range rows {
		if r.vlanID == 4034 && strings.EqualFold(r.strServList, "TR069") {
			return true
		}
	}
	return false
}

// has4031Bridge reports whether any WANC row is a bridged INTERNET on VLAN 4031.
// Bridge mode is WANCType=0 with ConnType=4 in this firmware.
func has4031Bridge(rows []wancRow) bool {
	for _, r := range rows {
		if r.vlanID == 4031 && strings.EqualFold(r.strServList, "INTERNET") &&
			r.wancType == 0 && r.connType == 4 {
			return true
		}
	}
	return false
}

// EnsureWANConnections checks the WANC table and creates the 4034 TR-069 (Route)
// and 4031 INTERNET (Bridge) connections when either is missing, then saves the
// DB. It does not reboot; the caller decides. bridgePortMask is the LAN-port
// bitmap for the 4031 bridge (bit0=LAN1..bit3=LAN4, 15 = all four). Progress is
// written to log.
func EnsureWANConnections(t *telnet.Telnet, bridgePortMask int, log io.Writer) error {
	logf(log, "正在检查 WAN 连接（4034 TR069 / 4031 桥接）……")
	dump, err := t.Exec("sendcmd 1 DB p WANC")
	if err != nil {
		return fmt.Errorf("读取 WANC 表失败：%w", err)
	}
	rows := parseWANC(dump)
	logf(log, "当前 WANC 有 %d 条连接", len(rows))
	for _, r := range rows {
		logf(log, "  - %s  (VLAN=%d, %s)", r.name, r.vlanID, r.strServList)
	}

	need4034 := !has4034TR069(rows)
	need4031 := !has4031Bridge(rows)

	if !need4034 && !need4031 {
		logf(log, "4034 TR069 与 4031 桥接都已存在，无需新建")
		return nil
	}

	// Compute the next unused WancIndex + view indices from the existing rows.
	next := nextIndices(rows)

	if need4034 {
		logf(log, "4034 TR069 不存在，正在创建……")
		if err := createTR069_4034(t, next, log); err != nil {
			return fmt.Errorf("创建 4034 TR069 失败：%w", err)
		}
		next.wancIndex++
		next.wcip++
		logf(log, "4034 TR069 创建完成")
	} else {
		logf(log, "4034 TR069 已存在，跳过")
	}

	if need4031 {
		if bridgePortMask <= 0 || bridgePortMask > 15 {
			bridgePortMask = 15
		}
		logf(log, "4031 桥接不存在，正在创建（端口掩码=%d，即 %s）……", bridgePortMask, portMaskLabel(bridgePortMask))
		if err := createBridge_4031(t, next, bridgePortMask, log); err != nil {
			return fmt.Errorf("创建 4031 桥接失败：%w", err)
		}
		logf(log, "4031 桥接创建完成")
	} else {
		logf(log, "4031 桥接已存在，跳过")
	}

	logf(log, "正在保存配置……")
	if _, err := t.Exec("sendcmd 1 DB save"); err != nil {
		return fmt.Errorf("保存配置失败：%w", err)
	}
	logf(log, "WAN 连接配置已保存")
	return nil
}

// indices tracks the next unused index for each family of ViewNames.
type indices struct {
	wancIndex int // WANC.WancIndex
	wcip      int // IGD.WD1.WCD1.WCIP<n>  (Route/IP)
	wcppp     int // IGD.WD1.WCD1.WCPPP<n> (Bridge or PPPoE)
	sl        int // IGD.WANCSL<n>
	mwc       int // IGD.MultiWancConf<n>
	ext       int // IGD.WANC_EXT<n>
}

// nextIndices scans existing WANC rows and returns the next unused index for
// each family. WCIP/WCPPP/SL/MWC/EXT are counted per family from the row list;
// on a freshly reset device (empty tables) they start at 1.
func nextIndices(rows []wancRow) indices {
	n := indices{wancIndex: 1, wcip: 1, wcppp: 1, sl: 1, mwc: 1, ext: 1}
	viewRE := regexp.MustCompile(`WCIP(\d+)|WCPPP(\d+)`)
	for _, r := range rows {
		if r.wancIndex >= n.wancIndex {
			n.wancIndex = r.wancIndex + 1
		}
		if m := viewRE.FindStringSubmatch(r.viewName); m != nil {
			if m[1] != "" {
				if v, _ := strconv.Atoi(m[1]); v >= n.wcip {
					n.wcip = v + 1
				}
			}
			if m[2] != "" {
				if v, _ := strconv.Atoi(m[2]); v >= n.wcppp {
					n.wcppp = v + 1
				}
			}
		}
		// SL / MWC / EXT indices grow with WANC rows; a WCIP row has 7 SLs, a
		// WCPPP INTERNET-bridge row has 1 SL. Rather than parse companion tables
		// (fragile), track them lazily from queries when we actually create.
	}
	return n
}

// countRows returns how many rows a DB table has, from its `p` dump.
func countRows(dump string) int {
	m := regexp.MustCompile(`RowCount="(\d+)"`).FindStringSubmatch(dump)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// countTableRows reads the row count of a DB table using `sendcmd DB p`.
func countTableRows(t *telnet.Telnet, table string) (int, error) {
	out, err := t.Exec(fmt.Sprintf("sendcmd 1 DB p %s", table))
	if err != nil {
		return 0, err
	}
	return countRows(out), nil
}

// applySets adds a fresh row to `table` and sets the given (field, value) pairs
// on it. The row number is inferred from the table's current row count before
// the addr, so back-to-back applySets on the same table need refreshCount=true
// on the second call to re-read the count.
func applySets(t *telnet.Telnet, table string, sets []kv) error {
	before, err := countTableRows(t, table)
	if err != nil {
		return err
	}
	if _, err := t.Exec("sendcmd 1 DB addr " + table); err != nil {
		return fmt.Errorf("addr %s: %w", table, err)
	}
	row := before // the new row is at index==prevCount
	for _, s := range sets {
		cmd := fmt.Sprintf("sendcmd 1 DB set %s %d %s %s", table, row, s.k, s.v)
		if _, err := t.Exec(cmd); err != nil {
			return fmt.Errorf("set %s row=%d %s=%s: %w", table, row, s.k, s.v, err)
		}
	}
	return nil
}

type kv struct{ k, v string }

// createTR069_4034 recreates the 4034 TR-069 Route connection matching the
// reference device's row-0 layout (dumped from a working ONU).
func createTR069_4034(t *telnet.Telnet, ix indices, log io.Writer) error {
	view := fmt.Sprintf("IGD.WD1.WCD1.WCIP%d", ix.wcip)
	name := fmt.Sprintf("%d_TR069_R_VID_4034", ix.wancIndex)

	wanc := []kv{
		{"ViewName", view},
		{"WANCDViewName", "IGD.WD1.WCD1"},
		{"Enable", "1"},
		{"WANCType", "1"},
		{"ConnType", "1"},
		{"MediaType", "0"},
		{"TriggerEnable", "0"},
		{"WANCName", name},
		{"IPAddr", "0.0.0.0"},
		{"SubMask", "0.0.0.0"},
		{"Gateway", "0.0.0.0"},
		{"StrServList", "TR069"},
		{"ServList", "2"},
		{"DNS1", "0.0.0.0"},
		{"DNS2", "0.0.0.0"},
		{"DNS3", "0.0.0.0"},
		{"IsNAT", "0"},
		{"IsForward", "0"},
		{"IsDefGW", "0"},
		{"IsNAT6", "0"},
		{"IsDefGW6", "0"},
		{"DSCP", "-1"},
		{"DSCP6", "-1"},
		{"TC", "-1"},
		{"VLANID", "4034"},
		{"MCVLANID", "-1"},
		{"IgmpProxyEnable", "1"},
		{"UpstreamWAN", "0"},
		{"MLDProxyEnable", "1"},
		{"Priority", "7"},
		{"WBDMode", "2"},
		{"HideListView", "0"},
		{"IPMode", "1"},
		{"IsDel", "0"},
		{"DNSEnabled", "1"},
		{"WancIndex", strconv.Itoa(ix.wancIndex)},
	}
	if err := applySets(t, "WANC", wanc); err != nil {
		return err
	}

	// WANCIP companion (IPv4 lease container - values match device defaults)
	if err := applySets(t, "WANCIP", []kv{
		{"ViewName", view},
		{"IPAddress", "0.0.0.0"},
		{"SubnetMask", "0.0.0.0"},
		{"GateWay", "0.0.0.0"},
		{"Addressingtype", "0"},
		{"DNS1", "0.0.0.0"},
		{"DNS2", "0.0.0.0"},
		{"DNS3", "0.0.0.0"},
		{"MTU", "1480"},
	}); err != nil {
		return err
	}

	// WANCServList - TR-069 Route needs Applications 3,4,8,11,14,15,16
	for _, app := range []string{"3", "4", "8", "11", "14", "15", "16"} {
		if err := applySets(t, "WANCServList", []kv{
			{"WANCViewName", view},
			{"Application", app},
			{"IsDel", "0"},
		}); err != nil {
			return err
		}
	}

	// MultiWancConfProduct - route entry: Wvlan=0, PortMask=4095, IpVersion=1
	if err := applySets(t, "MultiWancConfProduct", []kv{
		{"WANCViewName", view},
		{"IpVersion", "1"},
		{"Mvlan", "-1"},
		{"Mpri", "0"},
		{"PortMask", "4095"},
		{"VlanPortMask", "0"},
		{"TagFlag", "0"},
		{"Wvlan", "0"},
	}); err != nil {
		return err
	}

	// PDTWANCEXT - DhcpEnable=0 for TR-069 (as in reference)
	if err := applySets(t, "PDTWANCEXT", []kv{
		{"WANCViewName", view},
		{"DhcpEnable", "0"},
	}); err != nil {
		return err
	}

	return nil
}

// portMaskLabel returns a human-readable "LAN1/LAN3/LAN4" style label for the
// bitmap: bit 0 = LAN1, bit 1 = LAN2, bit 2 = LAN3, bit 3 = LAN4.
func portMaskLabel(mask int) string {
	var parts []string
	for i := 0; i < 4; i++ {
		if mask&(1<<i) != 0 {
			parts = append(parts, fmt.Sprintf("LAN%d", i+1))
		}
	}
	if len(parts) == 0 {
		return "无"
	}
	return strings.Join(parts, "/")
}

// createBridge_4031 recreates the 4031 INTERNET Bridge connection matching the
// reference device's row-1 layout. portMask selects which LAN ports the bridge
// covers (bit0=LAN1..bit3=LAN4).
func createBridge_4031(t *telnet.Telnet, ix indices, portMask int, log io.Writer) error {
	view := fmt.Sprintf("IGD.WD1.WCD1.WCPPP%d", ix.wcppp)
	name := fmt.Sprintf("%d_INTERNET_B_VID_4031", ix.wancIndex)

	wanc := []kv{
		{"ViewName", view},
		{"WANCDViewName", "IGD.WD1.WCD1"},
		{"Enable", "1"},
		{"WANCType", "0"},
		{"ConnType", "4"},
		{"MediaType", "2"},
		{"TriggerEnable", "0"},
		{"WANCName", name},
		{"IPAddr", "0.0.0.0"},
		{"SubMask", "0.0.0.0"},
		{"Gateway", "0.0.0.0"},
		{"StrServList", "INTERNET"},
		{"ServList", "1"},
		{"WorkIFMac", "00:00:00:00:00:00"},
		{"DNS1", "0.0.0.0"},
		{"DNS2", "0.0.0.0"},
		{"DNS3", "0.0.0.0"},
		{"IsNAT", "1"},
		{"IsForward", "1"},
		{"IsDefGW", "1"},
		{"IsNAT6", "0"},
		{"IsDefGW6", "1"},
		{"DSCP", "-1"},
		{"DSCP6", "-1"},
		{"TC", "-1"},
		{"VLANID", "4031"},
		{"MCVLANID", "-1"},
		{"IgmpProxyEnable", "1"},
		{"UpstreamWAN", "0"},
		{"MLDProxyEnable", "1"},
		{"Priority", "0"},
		{"WBDMode", "2"},
		{"HideListView", "0"},
		{"IPMode", "3"},
		{"IsDel", "0"},
		{"DNSEnabled", "1"},
		{"WancIndex", strconv.Itoa(ix.wancIndex)},
	}
	if err := applySets(t, "WANC", wanc); err != nil {
		return err
	}

	// WANCPPP companion (bridge shares this table with PPPoE; most fields keep
	// defaults, only ViewName is meaningful).
	if err := applySets(t, "WANCPPP", []kv{
		{"ViewName", view},
		{"ConnTrigger", "0"},
		{"AuthType", "0"},
		{"IdleTime", "1200"},
		{"MaxMRU", "1480"},
		{"MTU", "1480"},
		{"EchoTime", "10"},
		{"EchoRetry", "3"},
		{"MaxUser", "4"},
		{"ValidLANTx", "1"},
		{"HostTrigger", "1"},
	}); err != nil {
		return err
	}

	// WANCServList - Bridge INTERNET needs Application=10
	if err := applySets(t, "WANCServList", []kv{
		{"WANCViewName", view},
		{"Application", "10"},
		{"IsDel", "0"},
	}); err != nil {
		return err
	}

	// MultiWancConfProduct - bridge: PortMask picks LAN ports, Wvlan=4031
	if err := applySets(t, "MultiWancConfProduct", []kv{
		{"WANCViewName", view},
		{"IpVersion", "3"},
		{"Mvlan", "-1"},
		{"Mpri", "0"},
		{"PortMask", strconv.Itoa(portMask)},
		{"VlanPortMask", "0"},
		{"TagFlag", "0"},
		{"Wvlan", "4031"},
	}); err != nil {
		return err
	}

	// PDTWANCEXT - DhcpEnable=1 for INTERNET bridge
	if err := applySets(t, "PDTWANCEXT", []kv{
		{"WANCViewName", view},
		{"DhcpEnable", "1"},
	}); err != nil {
		return err
	}

	return nil
}
