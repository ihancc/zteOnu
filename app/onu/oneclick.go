package onu

import (
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/septrum101/zteOnu/app/telnet"
)

// Timing budgets for the reboot round-trips.
const (
	// After a full reboot the device is unreachable for a while; settle first,
	// then poll for it to come back.
	rebootSettleDelay = 15 * time.Second
	rebootWaitTimeout = 8 * time.Minute

	reconnectInterval = 3 * time.Second
	heartbeatInterval = 15 * time.Second

	// sdefconf can take a while to write flash before returning the prompt, so
	// wait longer than a normal command before assuming the session dropped.
	sdefconfTimeout = 60 * time.Second

	// timeout for the quick "is the device's web service up again" probe
	httpProbeTimeout = 2 * time.Second
)

// PONType selects which setmac registers the SN password is written to.
type PONType int

const (
	GPON PONType = iota
	XGPON
)

// OneClickOptions carries everything the one-click provisioning needs. The
// embedded Options describe the device and the webFac client settings (reused
// to re-acquire temporary telnet after every reboot); SN, Password, PON and
// RegionID describe the target configuration. ReopenTelnet, when set, re-acquires
// a temporary telnet after the final reboot to confirm the device recovered.
type OneClickOptions struct {
	Options
	SN                string
	Password          string
	PON               PONType
	RegionID          int
	EnsureWAN         bool // check/create 4034 TR069 + 4031 bridge after region reboot
	BridgePortMask    int  // LAN-port bitmap for the 4031 bridge (bit0=LAN1..bit3=LAN4, 15 = all)
	RebootAfterEnsure bool // reboot once more after creating WAN entries to apply
}

// RunOneClick provisions the ONU end to end using TEMPORARY telnet only:
//
//  1. Open temporary telnet through the webFac flow.
//  2. Set the bulk-procurement (集采) profile with `upgradetest sdefconf 466`,
//     which reboots; re-acquire temporary telnet after the device is back.
//  3. Write the SN and password with setmac (two extra registers for XGPON);
//     the SN write can reboot too, so re-acquire temporary telnet if it does.
//  4. Set the region profile, then reboot once so 集采 + region take effect.
//
// Nothing is made permanent: each reboot drops telnet, and the next step just
// opens a fresh temporary telnet via webFac. `upgradetest sdefconf` writes the
// profile and arms a reboot on the next input, so each sdefconf is followed by a
// deliberate reboot.
//
// Progress is written to o.Log; a nil writer falls back to os.Stdout.
func RunOneClick(o OneClickOptions) error {
	log := o.Log
	if log == nil {
		log = os.Stdout
	}
	if err := o.validate(); err != nil {
		return err
	}

	logf(log, "== 步骤 1/4：获取临时 telnet ==")
	t, _, _, err := OpenTempTelnet(o.Options)
	if err != nil {
		return err
	}
	defer func() {
		if t != nil {
			t.Conn.Close()
		}
	}()

	logf(log, "== 步骤 2/4：设置集采 (sdefconf 466)，随后重启 ==")
	applySdefconfThenReboot(t, 466, log)
	logf(log, "等待设备重新上线并重新获取临时 telnet……")
	t, err = reconnectTempAfterReboot(o.Options, log)
	if err != nil {
		return err
	}

	logf(log, "== 步骤 3/4：写入 SN 和密码 ==")
	t, err = writeSNAndPassword(t, o.SN, o.Password, o.PON, o.Options, log)
	if err != nil {
		return err
	}

	logf(log, "== 步骤 4/4：设置区域 %d，随后重启 ==", o.RegionID)
	applySdefconfThenReboot(t, o.RegionID, log)
	t = nil // applySdefconfThenReboot closed the connection

	if !o.EnsureWAN {
		logf(log, "完成：设备正在以新配置重启")
		return nil
	}

	logf(log, "等待设备重启完成，重新获取临时 telnet……")
	nt, rerr := reconnectTempAfterReboot(o.Options, log)
	if rerr != nil {
		return fmt.Errorf("重启后重新获取临时 telnet 失败：%w", rerr)
	}
	defer nt.Conn.Close()

	logf(log, "== 步骤 5：检查并创建 WAN 连接（4034 TR069 / 4031 桥接）==")
	if err := EnsureWANConnections(nt, o.BridgePortMask, log); err != nil {
		return err
	}
	if o.RebootAfterEnsure {
		logf(log, "正在重启设备以使新 WAN 连接生效……")
		_ = nt.Reboot()
		logf(log, "完成：设备正在重启，新 WAN 连接将随之生效")
		return nil
	}
	logf(log, "完成：WAN 连接检查/创建已结束")
	return nil
}

// applySdefconfThenReboot writes the sdefconf profile and reboots the device.
// `upgradetest sdefconf` arms a reboot on the next input, so an explicit reboot
// makes it deterministic; if the command already dropped the session, that was
// the reboot. The connection is closed on return - the device is going down and
// the caller re-acquires temporary telnet.
func applySdefconfThenReboot(t *telnet.Telnet, id int, log io.Writer) {
	cmd := fmt.Sprintf("upgradetest sdefconf %d", id)
	_, err := t.ExecTimeout(cmd, sdefconfTimeout)
	if err == nil {
		logf(log, "配置已写入，正在重启设备……")
		_ = t.Reboot()
	} else {
		// No prompt came back: the sdefconf itself already rebooted the device.
		logf(log, "设备正在重启……")
	}
	t.Conn.Close()
}

// validate checks the fields the flow cannot recover from a bad value for. SN
// and password are both optional: an empty one just skips its setmac commands
// (see writeSNAndPassword). A non-empty SN must be long enough to slice.
func (o OneClickOptions) validate() error {
	if sn := strings.TrimSpace(o.SN); sn != "" && len(sn) < 8 {
		return fmt.Errorf("SN %q 太短：至少需要 8 位", o.SN)
	}
	if o.RegionID <= 0 {
		return fmt.Errorf("无效的区域 id %d", o.RegionID)
	}
	return nil
}

// reconnectTempAfterReboot waits for the device to come back after a reboot and
// returns a fresh TEMPORARY telnet session opened through the webFac flow. A
// heartbeat keeps the log alive so a long wait does not look frozen.
func reconnectTempAfterReboot(opts Options, log io.Writer) (*telnet.Telnet, error) {
	time.Sleep(rebootSettleDelay)

	start := time.Now()
	deadline := start.Add(rebootWaitTimeout)
	lastBeat := time.Now()

	for time.Now().Before(deadline) {
		// Once the web service is back, open a fresh temporary telnet via webFac.
		if tcpReachable(opts.IP, opts.HTTPPort) {
			logf(log, "检测到设备 Web 服务已恢复，正在重新获取临时 telnet……")
			if t, _, _, err := OpenTempTelnet(opts); err == nil {
				return t, nil
			} else {
				logf(log, "获取临时 telnet 失败：%v；稍后重试", err)
			}
		} else if time.Since(lastBeat) >= heartbeatInterval {
			logf(log, "仍在等待设备重新上线……（已等待 %ds）", int(time.Since(start).Seconds()))
			lastBeat = time.Now()
		}

		time.Sleep(reconnectInterval)
	}
	return nil, fmt.Errorf("设备在 %s 内未恢复", rebootWaitTimeout)
}

// tcpReachable reports whether a TCP connection to ip:port can be established,
// used to detect that the device's web service is up again after a reboot.
func tcpReachable(ip string, port int) bool {
	c, err := net.DialTimeout("tcp", net.JoinHostPort(ip, strconv.Itoa(port)), httpProbeTimeout)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// writeSNAndPassword writes the SN and registration password with setmac and
// returns the connection to keep using. SN and password are independent: an
// empty SN skips the three SN registers, an empty password skips the password
// registers. GPON writes the password to one register; XGPON writes it to two
// more. Writing the SN (register 512) can reboot the device on some firmwares;
// when a command drops the session it is assumed applied, the device is waited
// for, temporary telnet is re-acquired, and the remaining commands continue.
func writeSNAndPassword(t *telnet.Telnet, sn, pass string, pon PONType, opts Options, log io.Writer) (*telnet.Telnet, error) {
	sn = strings.TrimSpace(sn)
	pass = strings.TrimSpace(pass)

	snCmds := snSetmacCommands(sn)
	passCmds := passSetmacCommands(pass, pon)
	if len(snCmds) == 0 && len(passCmds) == 0 {
		logf(log, "未填写 SN 和密码，跳过")
		return t, nil
	}

	if len(snCmds) > 0 {
		logf(log, "正在写入 SN……")
		nt, err := runSetmac(t, snCmds, opts, log)
		if err != nil {
			return nil, err
		}
		t = nt
		logf(log, "SN 写入完成")
	} else {
		logf(log, "SN 为空，跳过")
	}

	if len(passCmds) > 0 {
		logf(log, "正在写入密码……")
		nt, err := runSetmac(t, passCmds, opts, log)
		if err != nil {
			return nil, err
		}
		t = nt
		logf(log, "密码写入完成")
	} else {
		logf(log, "密码为空，跳过")
	}
	return t, nil
}

// runSetmac runs a group of setmac commands without echoing them. Writing the
// SN (register 512) can reboot the device on some firmwares; when a command
// drops the session it is assumed applied, the device is waited for, temporary
// telnet is re-acquired, and the remaining commands continue. Returns the
// connection to use.
func runSetmac(t *telnet.Telnet, cmds []string, opts Options, log io.Writer) (*telnet.Telnet, error) {
	for _, c := range cmds {
		if _, err := t.Exec(c); err != nil {
			// Session dropped: the command triggered a reboot. Assume it applied,
			// re-acquire temporary telnet and continue with the rest.
			logf(log, "设备已重启，等待重新获取临时 telnet……")
			t.Conn.Close()
			nt, rerr := reconnectTempAfterReboot(opts, log)
			if rerr != nil {
				return nil, rerr
			}
			t = nt
		}
	}
	return t, nil
}

// snSetmacCommands returns the setmac commands that write the SN (empty SN -> no
// commands). Input is assumed already trimmed.
func snSetmacCommands(sn string) []string {
	if sn == "" {
		return nil
	}
	return []string{
		fmt.Sprintf("setmac 1 512 %s", sn),
		fmt.Sprintf("setmac 1 2177 %s", sn[len(sn)-8:]),
		fmt.Sprintf("setmac 1 2176 %s", sn[:4]),
	}
}

// passSetmacCommands returns the setmac commands that write the password (empty
// password -> no commands); XGPON writes it to two extra registers. Input is
// assumed already trimmed.
func passSetmacCommands(pass string, pon PONType) []string {
	if pass == "" {
		return nil
	}
	cmds := []string{fmt.Sprintf("setmac 1 2178 %s", pass)}
	if pon == XGPON {
		cmds = append(cmds,
			fmt.Sprintf("setmac 1 2179 %s", pass),
			fmt.Sprintf("setmac 1 2180 %s", pass),
		)
	}
	return cmds
}

// setmacCommands returns all setmac command lines for the given SN, password and
// PON type: the SN registers first, then the password registers.
func setmacCommands(sn, pass string, pon PONType) []string {
	return append(snSetmacCommands(sn), passSetmacCommands(pass, pon)...)
}

// logf writes a CRLF-terminated line; the GUI log control and Windows consoles
// both expect CRLF.
func logf(w io.Writer, format string, a ...any) {
	fmt.Fprintf(w, format+"\r\n", a...)
}
