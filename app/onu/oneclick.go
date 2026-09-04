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

// Permanent telnet credentials written by Solidify and used to log back in.
const (
	permRootUser = "root"
	permRootPass = "Zte521"
)

// Timing budgets for the reboot / restart round-trips.
const (
	// After a full reboot the device is unreachable for a while; settle first,
	// then poll for it to come back.
	rebootSettleDelay = 15 * time.Second
	rebootWaitTimeout = 8 * time.Minute

	// After an in-place telnetd restart the device stays up, so a short budget
	// is enough to reconnect.
	restartSettleDelay = 2 * time.Second
	restartWaitTimeout = 60 * time.Second

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
// embedded Options describe the device and the webFac client settings (also
// reused to re-open telnet after the 集采 reset); SN, Password, PON and RegionID
// describe the target configuration. ReopenTelnet re-opens permanent telnet
// through webFac after the final reboot, since 集采 wipes it.
type OneClickOptions struct {
	Options
	SN           string
	Password     string
	PON          PONType
	RegionID     int
	ReopenTelnet bool
}

// RunOneClick provisions the ONU end to end:
//
//  1. Ensure permanent telnet (root/Zte521) is enabled, enabling it through the
//     webFac flow when the device does not already accept it.
//  2. Set the bulk-procurement (集采) profile with `upgradetest sdefconf 466`.
//  3. Write the SN and password with setmac (two extra registers for XGPON).
//  4. Set the region profile, then reboot once so 集采 + region take effect.
//
// `upgradetest sdefconf` writes the profile and arms a reboot that fires on the
// next input (Enter), so each sdefconf is followed by a deliberate reboot and a
// reconnect. Because 集采 deletes /userconfig (which holds the telnet setting),
// telnet is gone after that reboot, so the reconnect re-opens it through webFac.
// Writing the SN (`setmac 1 512`) can itself reboot the device, so the setmac
// step also tolerates a dropped session and reconnects.
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

	logf(log, "== 步骤 1/4：检查并开启永久 telnet ==")
	t, err := ensurePermanentTelnet(o.Options, log)
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
	logf(log, "等待设备重新上线并重新开启永久 telnet……")
	t, err = reconnectAfterReboot(o.Options, log)
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

	if o.ReopenTelnet {
		logf(log, "等待设备重启完成，并重新开启永久 telnet……")
		nt, rerr := reconnectAfterReboot(o.Options, log)
		if rerr != nil {
			return fmt.Errorf("重启后重新开启 telnet 失败：%w", rerr)
		}
		nt.Conn.Close()
		logf(log, "完成：设备已按新配置重启，并已重新开启永久 telnet（账号 root，密码 Zte521），可重新连接")
		return nil
	}

	logf(log, "完成：设备正在以新配置重启（集采会关闭 telnet，如需重连请勾选“完成后重新开启 telnet”或再次开启永久 telnet）")
	return nil
}

// applySdefconfThenReboot writes the sdefconf profile and reboots the device.
// `upgradetest sdefconf` arms a reboot on the next input, so an explicit reboot
// makes it deterministic; if the command already dropped the session, that was
// the reboot. The connection is closed on return - the device is going down and
// the caller reconnects.
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

// ensurePermanentTelnet returns a logged-in permanent-telnet connection,
// enabling permanent telnet through the webFac flow first when the device does
// not already accept root/Zte521.
func ensurePermanentTelnet(opts Options, log io.Writer) (*telnet.Telnet, error) {
	if t, ok := dialPermanent(opts.IP, opts.TelnetPort); ok {
		logf(log, "永久 telnet 已开启")
		return t, nil
	}
	logf(log, "永久 telnet 未开启，正在通过 webFac 开启……")
	return enablePermanentTelnet(opts, log)
}

// enablePermanentTelnet runs the webFac flow, writes the permanent telnet
// settings, restarts telnetd in place and returns a fresh logged-in connection.
func enablePermanentTelnet(opts Options, log io.Writer) (*telnet.Telnet, error) {
	tmp, _, _, err := OpenTempTelnet(opts)
	if err != nil {
		return nil, err
	}
	if err := SolidifyAndRestart(tmp, opts.IP, opts.TelnetPort, log); err != nil {
		tmp.Conn.Close()
		return nil, err
	}
	tmp.Conn.Close()

	logf(log, "永久 telnet 已开启，正在连接……")
	return waitLogin(opts.IP, opts.TelnetPort, restartSettleDelay, restartWaitTimeout, log)
}

// reconnectAfterReboot waits for the device to come back after the 集采 reboot.
// The 集采 reset usually disables telnet, so it first tries an existing
// permanent telnet and, once the device's web service answers again, re-opens
// permanent telnet through the webFac flow. A heartbeat keeps the log alive so
// a long wait does not look frozen.
func reconnectAfterReboot(opts Options, log io.Writer) (*telnet.Telnet, error) {
	time.Sleep(rebootSettleDelay)

	start := time.Now()
	deadline := start.Add(rebootWaitTimeout)
	lastBeat := time.Now()

	for time.Now().Before(deadline) {
		// Permanent telnet may have survived the reset.
		if t, ok := dialPermanent(opts.IP, opts.TelnetPort); ok {
			logf(log, "已连接（永久 telnet 仍然有效）")
			return t, nil
		}

		// Once the web service is back, re-open permanent telnet through webFac.
		if tcpReachable(opts.IP, opts.HTTPPort) {
			logf(log, "检测到设备 Web 服务已恢复，正在重新开启永久 telnet……")
			if t, err := enablePermanentTelnet(opts, log); err == nil {
				return t, nil
			} else {
				logf(log, "重新开启 telnet 失败：%v；稍后重试", err)
			}
		} else if time.Since(lastBeat) >= heartbeatInterval {
			logf(log, "仍在等待设备重新上线……（已等待 %ds）", int(time.Since(start).Seconds()))
			lastBeat = time.Now()
		}

		time.Sleep(reconnectInterval)
	}
	return nil, fmt.Errorf("设备在 %s 内未恢复", rebootWaitTimeout)
}

// dialPermanent probes the telnet port once and reports whether the device
// already accepts the permanent root/Zte521 login.
func dialPermanent(ip string, port int) (*telnet.Telnet, bool) {
	t, err := telnet.NewRetry(permRootUser, permRootPass, ip, port, 1, 0)
	if err != nil {
		return nil, false
	}
	if err := t.Login(); err != nil {
		t.Conn.Close()
		return nil, false
	}
	return t, true
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

// waitLogin waits initialDelay for the device to settle, then polls the telnet
// port until a permanent (root/Zte521) login succeeds or timeout elapses.
func waitLogin(ip string, port int, initialDelay, timeout time.Duration, log io.Writer) (*telnet.Telnet, error) {
	time.Sleep(initialDelay)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if t, ok := dialPermanent(ip, port); ok {
			logf(log, "已连接")
			return t, nil
		}
		time.Sleep(reconnectInterval)
	}
	return nil, fmt.Errorf("在 %s 内设备未接受永久 telnet", timeout)
}

// writeSNAndPassword writes the SN and registration password with setmac and
// returns the connection to keep using. SN and password are independent: an
// empty SN skips the three SN registers, an empty password skips the password
// registers. GPON writes the password to one register; XGPON writes it to two
// more. Writing the SN (register 512) can reboot the device on some firmwares;
// when a command drops the session it is assumed applied, the device is waited
// for, telnet is re-opened, and the remaining commands continue.
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
// drops the session it is assumed applied, the device is waited for, telnet is
// re-opened, and the remaining commands continue. Returns the connection to use.
func runSetmac(t *telnet.Telnet, cmds []string, opts Options, log io.Writer) (*telnet.Telnet, error) {
	for _, c := range cmds {
		if _, err := t.Exec(c); err != nil {
			// Session dropped: the command triggered a reboot. Assume it applied,
			// reconnect and continue with the rest.
			logf(log, "设备已重启，等待重新连接……")
			t.Conn.Close()
			nt, rerr := reconnectAfterReboot(opts, log)
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
