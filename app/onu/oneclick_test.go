package onu

import (
	"strings"
	"testing"
)

func TestSetmacCommands(t *testing.T) {
	const sn = "ZTEGC1234567" // first4 ZTEG, last8 C1234567
	const pass = "secret"

	tests := []struct {
		name string
		sn   string
		pass string
		pon  PONType
		want []string
	}{
		{
			name: "gpon sn and pass",
			sn:   sn,
			pass: pass,
			pon:  GPON,
			want: []string{
				"setmac 1 512 ZTEGC1234567",
				"setmac 1 2177 C1234567",
				"setmac 1 2176 ZTEG",
				"setmac 1 2178 secret",
			},
		},
		{
			name: "xgpon adds two password registers",
			sn:   sn,
			pass: pass,
			pon:  XGPON,
			want: []string{
				"setmac 1 512 ZTEGC1234567",
				"setmac 1 2177 C1234567",
				"setmac 1 2176 ZTEG",
				"setmac 1 2178 secret",
				"setmac 1 2179 secret",
				"setmac 1 2180 secret",
			},
		},
		{
			name: "empty sn skips sn registers",
			sn:   "",
			pass: pass,
			pon:  GPON,
			want: []string{"setmac 1 2178 secret"},
		},
		{
			name: "empty password skips password registers",
			sn:   sn,
			pass: "",
			pon:  XGPON,
			want: []string{
				"setmac 1 512 ZTEGC1234567",
				"setmac 1 2177 C1234567",
				"setmac 1 2176 ZTEG",
			},
		},
		{
			name: "both empty yields nothing",
			sn:   "",
			pass: "",
			pon:  GPON,
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := setmacCommands(tc.sn, tc.pass, tc.pon)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d commands %v, want %d %v", len(got), got, len(tc.want), tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("cmd %d = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestValidate(t *testing.T) {
	base := OneClickOptions{RegionID: DefaultRegionID}

	t.Run("empty sn and pass allowed", func(t *testing.T) {
		if err := base.validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("short non-empty sn rejected", func(t *testing.T) {
		o := base
		o.SN = "ZTEG"
		if err := o.validate(); err == nil {
			t.Fatal("expected error for short SN")
		}
	})

	t.Run("invalid region rejected", func(t *testing.T) {
		o := base
		o.RegionID = 0
		if err := o.validate(); err == nil {
			t.Fatal("expected error for region 0")
		}
	})
}

func TestRegionIndexByID(t *testing.T) {
	if got := Regions[RegionIndexByID(DefaultRegionID)].Name; !strings.Contains(got, "Henan") {
		t.Errorf("default region = %q, want Henan", got)
	}
	if RegionIndexByID(999999) != 0 {
		t.Error("unknown region id should fall back to index 0")
	}
}
