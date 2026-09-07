package onu

import (
	"fmt"
	"io"
	"strings"

	"github.com/septrum101/zteOnu/app/telnet"
)

// DiscoverWAN runs a small set of `sendcmd 1 DB` inspection commands and dumps
// their raw output to log. It is a discovery helper: the exact node names and
// field layout for WAN connections vary between firmwares, so before writing
// the actual "check and create 4034/4031" step we need to see what the device
// actually exposes. The user runs this on the real ONU, shares the output, and
// the create logic can then be written against the real format.
func DiscoverWAN(t *telnet.Telnet, log io.Writer) error {
	// Nodes that ZTE firmwares typically use for WAN / TR-069 connections.
	nodes := []string{
		"WANMultiTag",
		"WANIPCfg",
		"WANBridgeCfg",
		"WANPPPCfg",
		"WANCommonCfg",
		"WANL2Cfg",
		"IGDWanConnDev",
		"ServiceConnectionInfo",
		"TR069Cfg",
		"BridgeCfg",
		"VlanCfg",
	}

	logf(log, "== 诊断：查询 WAN 相关 DB 节点 ==")
	logf(log, "（把下面从此行开始的全部输出复制发我，用来定位实际字段）")
	logf(log, "-----BEGIN WAN DISCOVERY-----")

	tried := 0
	got := 0
	for _, node := range nodes {
		// `DB p` prints the whole table; some firmwares use `DB show`. Try p first,
		// fall through to show on error / empty.
		out, err := t.Exec(fmt.Sprintf("sendcmd 1 DB p %s", node))
		if err == nil && strings.TrimSpace(out) != "" && !isNodeMissing(out) {
			logf(log, "$ sendcmd 1 DB p %s", node)
			logf(log, "%s", strings.TrimRight(out, "\r\n"))
			logf(log, "")
			got++
		} else {
			out2, err2 := t.Exec(fmt.Sprintf("sendcmd 1 DB show %s", node))
			if err2 == nil && strings.TrimSpace(out2) != "" && !isNodeMissing(out2) {
				logf(log, "$ sendcmd 1 DB show %s", node)
				logf(log, "%s", strings.TrimRight(out2, "\r\n"))
				logf(log, "")
				got++
			}
		}
		tried++
	}

	// Also probe a couple of "list all nodes" variants, useful when none of the
	// specific names above match.
	for _, cmd := range []string{"sendcmd 1 DB list", "sendcmd 1 DB p"} {
		out, err := t.Exec(cmd)
		if err == nil && strings.TrimSpace(out) != "" {
			logf(log, "$ %s", cmd)
			logf(log, "%s", strings.TrimRight(out, "\r\n"))
			logf(log, "")
			break
		}
	}

	logf(log, "-----END WAN DISCOVERY-----")
	logf(log, "共尝试 %d 个节点，命中 %d 个", tried, got)
	return nil
}

// isNodeMissing reports whether device output indicates the DB node does not
// exist ("no such", "not found", "invalid"), so we do not treat it as a match.
func isNodeMissing(out string) bool {
	s := strings.ToLower(out)
	for _, hint := range []string{"no such", "not found", "invalid", "unknown", "not exist"} {
		if strings.Contains(s, hint) {
			return true
		}
	}
	return false
}
