// Package zteonu is the gomobile-bound API for the Android app. It wraps the
// pure-Go core (app/onu, app/telnet) in a small, gomobile-compatible surface:
// only primitive parameters/returns and a Logger callback, so `gomobile bind`
// can turn it into an .aar the Kotlin UI calls.
//
// Build the library with:
//
//	gomobile bind -target=android -androidapi 21 \
//	    -o app/libs/zteonu.aar github.com/septrum101/zteOnu/mobile
package zteonu

import (
	"github.com/septrum101/zteOnu/app/onu"
	tnet "github.com/septrum101/zteOnu/app/telnet"
)

// Logger receives human-readable progress lines. Implement it on the Android
// side and pass an instance to the provisioning calls.
type Logger interface {
	Log(line string)
}

// loggerWriter adapts a Logger to the io.Writer the core packages log to.
type loggerWriter struct{ l Logger }

func (w loggerWriter) Write(p []byte) (int, error) {
	if w.l != nil {
		w.l.Log(string(p))
	}
	return len(p), nil
}

// --- Region table for the UI dropdown (gomobile can't return a slice of
// structs, so expose indexed accessors) ---

// RegionCount returns the number of selectable region profiles.
func RegionCount() int { return len(onu.Regions) }

// RegionIDAt returns the sdefconf id of the region at index i.
func RegionIDAt(i int) int {
	if i < 0 || i >= len(onu.Regions) {
		return 0
	}
	return onu.Regions[i].ID
}

// RegionNameAt returns the display name of the region at index i.
func RegionNameAt(i int) string {
	if i < 0 || i >= len(onu.Regions) {
		return ""
	}
	return onu.Regions[i].Name
}

// DefaultRegionIndex returns the index of the default region (Henan).
func DefaultRegionIndex() int { return onu.RegionIndexByID(onu.DefaultRegionID) }

// --- Permanent telnet (manual mode) ---

// Telnet-apply modes for OpenPermanentTelnet.
const (
	ModeTempOnly       = 0 // open temp telnet only, print verified credentials
	ModeRestartService = 1 // write permanent telnet, apply by restarting telnetd
	ModeRebootDevice   = 2 // write permanent telnet, apply by rebooting the device
)

// OpenPermanentTelnet runs the webFac flow with the given client MAC and, per
// mode, opens temporary telnet (0), enables permanent telnet by restarting the
// service (1) or by rebooting the device (2). Progress is streamed to log.
func OpenPermanentTelnet(ip string, httpPort int, telnetPort int, facUser string, facPass string, mac string, mode int, log Logger) error {
	w := loggerWriter{log}
	opts := onu.Options{
		User: facUser, Pass: facPass, IP: ip,
		HTTPPort: httpPort, TelnetPort: telnetPort, Mac: mac, Log: w,
	}
	t, _, _, err := onu.OpenTempTelnet(opts)
	if err != nil {
		return err
	}
	defer t.Conn.Close()

	switch mode {
	case ModeRestartService:
		return onu.SolidifyAndRestart(t, ip, telnetPort, w)
	case ModeRebootDevice:
		return onu.SolidifyAndReboot(t, w)
	default:
		return nil
	}
}

// --- One-click provisioning ---

// RunOneClick runs the full one-click flow: ensure permanent telnet, set 集采,
// write SN/password, set region and reboot. xgpon selects XGPON (false = GPON);
// reopenTelnet re-opens permanent telnet after the final reboot. Progress is
// streamed to log.
func RunOneClick(ip string, httpPort int, telnetPort int, facUser string, facPass string, mac string, sn string, password string, xgpon bool, regionID int, reopenTelnet bool, log Logger) error {
	pon := onu.GPON
	if xgpon {
		pon = onu.XGPON
	}
	return onu.RunOneClick(onu.OneClickOptions{
		Options: onu.Options{
			User: facUser, Pass: facPass, IP: ip,
			HTTPPort: httpPort, TelnetPort: telnetPort, Mac: mac, Log: loggerWriter{log},
		},
		SN:           sn,
		Password:     password,
		PON:          pon,
		RegionID:     regionID,
		ReopenTelnet: reopenTelnet,
	})
}

// --- Command console ---

// Session is a live telnet connection for the command console.
type Session struct {
	t *tnet.Telnet
}

// Connect opens a telnet session to ip:telnetPort and logs in with user/pass
// (typically root/Zte521 for permanent telnet).
func Connect(ip string, telnetPort int, user string, pass string) (*Session, error) {
	t, err := tnet.New(user, pass, ip, telnetPort)
	if err != nil {
		return nil, err
	}
	if err := t.Login(); err != nil {
		t.Conn.Close()
		return nil, err
	}
	return &Session{t: t}, nil
}

// Exec runs a shell command and returns its output.
func (s *Session) Exec(cmd string) (string, error) {
	return s.t.Exec(cmd)
}

// Close closes the telnet session.
func (s *Session) Close() error {
	if s.t == nil {
		return nil
	}
	err := s.t.Conn.Close()
	s.t = nil
	return err
}
