package onu

import (
	"fmt"
	"io"
	"net"
	"os"
	"strings"

	"github.com/septrum101/zteOnu/app/factory"
	"github.com/septrum101/zteOnu/app/telnet"
)

// Options carries the device and client settings for OpenTempTelnet.
type Options struct {
	User       string
	Pass       string
	IP         string
	HTTPPort   int
	TelnetPort int
	Iface      string
	Mac        string
	// Log receives the human-readable progress. A nil writer falls back to
	// os.Stdout.
	Log io.Writer
}

// OpenTempTelnet runs the webFac flow for the client MAC selected by --iface,
// --mac or the route-based auto-detection and verifies the granted temp
// credentials with a real telnet login. The HTTP flow returns credentials even
// when the MAC is not honored, so the run only succeeds if the credentials
// actually log in; on failure the returned connection is nil.
func OpenTempTelnet(opts Options) (*telnet.Telnet, string, string, error) {
	log := opts.Log
	if log == nil {
		log = os.Stdout
	}

	fac := factory.NewWithLog(opts.User, opts.Pass, opts.IP, opts.HTTPPort, opts.Iface, opts.Mac, log)

	mac, err := fac.ClientMAC()
	if err != nil {
		return nil, "", "", err
	}
	label := net.HardwareAddr(mac[:]).String()

	fmt.Fprintln(log, strings.Repeat("-", 35))
	tlUser, tlPass, err := fac.HandleMAC(mac)
	if err != nil {
		return nil, "", "", fmt.Errorf("[%s] 工厂流程失败：%w", label, err)
	}
	fmt.Fprintf(log, "[%s] 临时账号：%s，密码：%s\n", label, tlUser, tlPass)

	t, err := telnet.New(tlUser, tlPass, opts.IP, opts.TelnetPort)
	if err != nil {
		return nil, "", "", fmt.Errorf("[%s] telnet 无法连接：%w", label, err)
	}
	if lerr := t.Login(); lerr != nil {
		t.Conn.Close()
		return nil, "", "", fmt.Errorf("[%s] telnet 验证失败：%w", label, lerr)
	}
	fmt.Fprintln(log, strings.Repeat("-", 35))
	return t, tlUser, tlPass, nil
}
