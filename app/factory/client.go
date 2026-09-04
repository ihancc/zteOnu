package factory

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/go-resty/resty/v2"
)

// Factory drives the reverse-engineered webFac HTTP flow of the ONU.
type Factory struct {
	user   string
	passwd string
	ip     string
	port   int
	iface  string
	mac    string
	cli    *resty.Client
	key    []byte
	log    io.Writer
}

// New builds a Factory for the given device and client settings. A non-empty
// mac is the only candidate used for the SendInfo payload (see ClientMAC).
// Progress is written to os.Stdout; use NewWithLog to redirect it.
func New(user string, passwd string, ip string, port int, iface string, mac string) *Factory {
	return NewWithLog(user, passwd, ip, port, iface, mac, os.Stdout)
}

// NewWithLog is New with a caller-supplied writer for the human-readable step
// progress. A nil writer falls back to os.Stdout.
func NewWithLog(user string, passwd string, ip string, port int, iface string, mac string, log io.Writer) *Factory {
	if log == nil {
		log = os.Stdout
	}
	return &Factory{
		user:   user,
		passwd: passwd,
		ip:     ip,
		port:   port,
		iface:  iface,
		mac:    mac,
		log:    log,
		cli: resty.New().SetHeader("User-Agent", "curl/8.8.0-DEV").
			SetTimeout(10 * time.Second).
			SetBaseURL(fmt.Sprintf("http://%s:%d", ip, port)),
	}
}
