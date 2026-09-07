package onu

import (
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"

	"github.com/septrum101/zteOnu/app/telnet"
)

// RX-offset step: some deployments require that the RX power reported by the
// ONU to the OLT and shown in the Web UI stays within a given attenuation
// budget (e.g. |RX| ≤ 23 dB). When the natural RX exceeds an allowed maximum
// (e.g. |RX| > 25 dB) we write a positive display-side compensation into the
// OPTICAL DB row so the reported (not the raw hardware) reading falls under
// the target. The raw `opticaltst -getpara` reading is unaffected; the
// compensation is applied by the higher-level readers (Web UI, TR-069/OMCI).
//
// Verified live: setting OPTICAL.RxOffset in DB does not change the value
// `opticaltst -getpara` reports (raw ADC read); it is only picked up by the
// user-facing readers.

// SFF-8472 style raw-to-dBm for optical power, plus the +1 dB display
// calibration ZTE applies. The +1 dB was validated against the device's own
// Web UI reading (raw 56 → user-facing -21.5 dBm).
func rawToDBm(raw int) float64 {
	if raw <= 0 {
		return math.Inf(-1)
	}
	return 10*math.Log10(float64(raw)*0.0001) + 1.0
}

var rxRawRE = regexp.MustCompile(`RXPower\s*=\s*(-?\d+)`)

// readRxDBm reads the current RX power from the ONU shell in dBm. Returns
// -Inf when the reader returns 0 (module not producing data yet).
func readRxDBm(t *telnet.Telnet) (float64, int, error) {
	out, err := t.Exec("opticaltst -getpara")
	if err != nil {
		return 0, 0, err
	}
	m := rxRawRE.FindStringSubmatch(out)
	if m == nil {
		return math.Inf(-1), 0, fmt.Errorf("解析 opticaltst 输出失败")
	}
	raw, _ := strconv.Atoi(m[1])
	return rawToDBm(raw), raw, nil
}

// EnsureRxOffset reads RX and, if the reported |dBm| exceeds maxAbsDBm, writes
// a positive OPTICAL.RxOffset (in 0.0001 dBm units) large enough to bring the
// user-facing display down to targetAbsDBm. maxAbsDBm defaults to 25 and
// targetAbsDBm to 23 when passed as 0.
func EnsureRxOffset(t *telnet.Telnet, maxAbsDBm, targetAbsDBm float64, log io.Writer) error {
	if maxAbsDBm <= 0 {
		maxAbsDBm = 25
	}
	if targetAbsDBm <= 0 {
		targetAbsDBm = 23
	}
	if targetAbsDBm > maxAbsDBm {
		return fmt.Errorf("目标 |RX| (%.1f) 不应大于阈值 |RX| (%.1f)", targetAbsDBm, maxAbsDBm)
	}
	dbm, raw, err := readRxDBm(t)
	if err != nil {
		return err
	}
	if math.IsInf(dbm, -1) {
		logf(log, "RX 读数为 0（光模块未收到光），跳过 RxOffset 检查")
		return nil
	}
	abs := -dbm
	logf(log, "当前 RX = %.2f dBm（|RX| = %.2f dB，raw=%d）", dbm, abs, raw)
	if abs <= maxAbsDBm {
		logf(log, "|RX| ≤ %.1f dB，无需补偿", maxAbsDBm)
		return nil
	}
	delta := abs - targetAbsDBm                 // dB, always positive when abs > targetAbsDBm
	offsetRaw := int(math.Round(delta * 10000)) // 0.0001 dBm units
	logf(log, "|RX| 超过 %.1f dB，写入 OPTICAL.RxOffset=%d（≈ +%.2f dB），显示将 ≤ %.1f dB", maxAbsDBm, offsetRaw, delta, targetAbsDBm)
	if _, err := t.Exec(fmt.Sprintf("sendcmd 1 DB set OPTICAL 0 RxOffset %d", offsetRaw)); err != nil {
		return fmt.Errorf("写入 RxOffset 失败：%w", err)
	}
	if _, err := t.Exec("sendcmd 1 DB set OPTICAL 0 RxCompEn 1"); err != nil {
		return fmt.Errorf("开启 RxCompEn 失败：%w", err)
	}
	if _, err := t.Exec("sendcmd 1 DB save"); err != nil {
		return fmt.Errorf("保存 DB 失败：%w", err)
	}
	logf(log, "RX 补偿已保存")
	return nil
}
