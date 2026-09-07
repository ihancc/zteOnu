package onu

import (
	"math"
	"testing"
)

func TestRawToDBm(t *testing.T) {
	// Validated against the live device: raw 56 → -21.52 dBm (matches the Web UI).
	got := rawToDBm(56)
	if math.Abs(got-(-21.52)) > 0.05 {
		t.Errorf("rawToDBm(56) = %.3f, want ≈ -21.52", got)
	}
	// raw 0 or negative → -Inf so callers know "no signal".
	if !math.IsInf(rawToDBm(0), -1) {
		t.Error("rawToDBm(0) should be -Inf")
	}
	if !math.IsInf(rawToDBm(-1), -1) {
		t.Error("rawToDBm(-1) should be -Inf")
	}
}

// A pure arithmetic version of the compensation calculation, mirroring the
// EnsureRxOffset function without touching telnet.
func compensationRaw(currentDBm, maxAbsDBm, targetAbsDBm float64) (int, bool) {
	abs := -currentDBm
	if abs <= maxAbsDBm {
		return 0, false
	}
	delta := abs - targetAbsDBm
	return int(math.Round(delta * 10000)), true
}

func TestCompensation(t *testing.T) {
	// Below threshold: no write, no offset.
	if raw, need := compensationRaw(-21.5, 25, 23); need || raw != 0 {
		t.Errorf("at -21.5 (< 25) expected (0,false), got (%d,%v)", raw, need)
	}
	// Right at threshold: still not triggered.
	if raw, need := compensationRaw(-25.0, 25, 23); need || raw != 0 {
		t.Errorf("at exactly -25 expected (0,false), got (%d,%v)", raw, need)
	}
	// Just over: need small positive offset to bring to -23.
	if raw, need := compensationRaw(-25.5, 25, 23); !need || raw != 25000 {
		t.Errorf("at -25.5 want (25000,true), got (%d,%v)", raw, need)
	}
	// Way over: -30 dBm should get +7 dB offset (70000 in 0.0001 units).
	if raw, need := compensationRaw(-30.0, 25, 23); !need || raw != 70000 {
		t.Errorf("at -30 want (70000,true), got (%d,%v)", raw, need)
	}
}
