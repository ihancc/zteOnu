package onu

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/septrum101/zteOnu/app/telnet"
)

// SolidifyAndReboot writes the permanent telnet settings on t - which must
// already be logged in, see OpenTempTelnet - and reboots the device. The
// reboot is safe because Solidify waits for the shell prompt after "DB save",
// i.e. the flash write has completed. Progress is written to log; a nil writer
// falls back to os.Stdout.
func SolidifyAndReboot(t *telnet.Telnet, log io.Writer) error {
	if log == nil {
		log = os.Stdout
	}
	if err := t.Solidify(); err != nil {
		return err
	}
	fmt.Fprintln(log, "永久 telnet 设置成功")
	fmt.Fprintln(log, "账号：root，密码：Zte521")

	fmt.Fprintln(log, "等待重启……")
	if err := t.Reboot(); err != nil {
		return err
	}
	fmt.Fprintln(log, "设备正在重启")
	return nil
}

// SolidifyAndRestart writes the permanent telnet settings on t - which must
// already be logged in, see OpenTempTelnet - and applies them in place by
// restarting the telnetd service through the device's program manager, without
// rebooting. Restarting drops the current session, so the result is verified
// with a fresh login using the permanent credentials. Progress is written to
// log; a nil writer falls back to os.Stdout.
func SolidifyAndRestart(t *telnet.Telnet, ip string, telnetPort int, log io.Writer) error {
	if log == nil {
		log = os.Stdout
	}
	if err := t.Solidify(); err != nil {
		return err
	}
	fmt.Fprintln(log, "永久 telnet 已保存")
	fmt.Fprintln(log, "账号：root，密码：Zte521")

	fmt.Fprintln(log, "正在原地重启 telnetd（不重启设备）……")
	if err := t.RestartTelnetd(); err != nil {
		return err
	}
	fmt.Fprintln(log, "telnetd 已重启，正在验证永久 telnet……")

	// pc sometimes takes a while to respawn telnetd, so be more patient here
	// than in the initial login (30s budget) before declaring the restart bad.
	v, err := telnet.NewRetry("root", "Zte521", ip, telnetPort, 15, 2*time.Second)
	if err != nil {
		return fmt.Errorf("重启后 telnetd 未恢复：%w", err)
	}
	defer v.Conn.Close()
	if err := v.Login(); err != nil {
		return fmt.Errorf("重启后永久 telnet 验证失败：%w", err)
	}
	fmt.Fprintln(log, "原地重启后永久 telnet 验证成功")
	return nil
}
