package onu

import "testing"

// Captured live from a working ONU: WANC dump with the 4034 TR-069 Route (row 0)
// and the 4031 INTERNET Bridge (row 1) both present.
const wancDumpBoth = `<Tbl name="WANC" RowCount="2">
	<Row No="0">
		<DM name="ViewName" val="IGD.WD1.WCD1.WCIP1"/>
		<DM name="WANCName" val="1_TR069_R_VID_4034"/>
		<DM name="WANCType" val="1"/>
		<DM name="ConnType" val="1"/>
		<DM name="StrServList" val="TR069"/>
		<DM name="VLANID" val="4034"/>
		<DM name="WancIndex" val="1"/>
	</Row>
	<Row No="1">
		<DM name="ViewName" val="IGD.WD1.WCD1.WCPPP1"/>
		<DM name="WANCName" val="2_INTERNET_B_VID_4031"/>
		<DM name="WANCType" val="0"/>
		<DM name="ConnType" val="4"/>
		<DM name="StrServList" val="INTERNET"/>
		<DM name="VLANID" val="4031"/>
		<DM name="WancIndex" val="2"/>
	</Row>
</Tbl>`

// WANC dump with only the 4034 TR-069 Route entry.
const wancDump4034Only = `<Tbl name="WANC" RowCount="1">
	<Row No="0">
		<DM name="ViewName" val="IGD.WD1.WCD1.WCIP1"/>
		<DM name="WANCName" val="1_TR069_R_VID_4034"/>
		<DM name="WANCType" val="1"/>
		<DM name="ConnType" val="1"/>
		<DM name="StrServList" val="TR069"/>
		<DM name="VLANID" val="4034"/>
		<DM name="WancIndex" val="1"/>
	</Row>
</Tbl>`

// Empty WANC (post-集采 reset scenario).
const wancDumpEmpty = `<Tbl name="WANC" RowCount="0">
</Tbl>`

func TestParseWANC(t *testing.T) {
	rows := parseWANC(wancDumpBoth)
	if len(rows) != 2 {
		t.Fatalf("parsed %d rows, want 2", len(rows))
	}
	if rows[0].vlanID != 4034 || rows[0].strServList != "TR069" || rows[0].wancType != 1 {
		t.Errorf("row 0 fields: %+v", rows[0])
	}
	if rows[1].vlanID != 4031 || rows[1].strServList != "INTERNET" || rows[1].wancType != 0 || rows[1].connType != 4 {
		t.Errorf("row 1 fields: %+v", rows[1])
	}
	if rows[0].viewName != "IGD.WD1.WCD1.WCIP1" || rows[1].viewName != "IGD.WD1.WCD1.WCPPP1" {
		t.Errorf("view names wrong: %+v %+v", rows[0], rows[1])
	}
}

func TestHasChecks(t *testing.T) {
	both := parseWANC(wancDumpBoth)
	if !has4034TR069(both) || !has4031Bridge(both) {
		t.Fatal("both flags should be true on wancDumpBoth")
	}
	only := parseWANC(wancDump4034Only)
	if !has4034TR069(only) {
		t.Fatal("4034 should be present on wancDump4034Only")
	}
	if has4031Bridge(only) {
		t.Fatal("4031 should be missing on wancDump4034Only")
	}
	empty := parseWANC(wancDumpEmpty)
	if has4034TR069(empty) || has4031Bridge(empty) {
		t.Fatal("both flags should be false on empty dump")
	}
}

func TestNextIndices(t *testing.T) {
	both := parseWANC(wancDumpBoth)
	ix := nextIndices(both)
	if ix.wancIndex != 3 {
		t.Errorf("wancIndex = %d, want 3", ix.wancIndex)
	}
	if ix.wcip != 2 {
		t.Errorf("wcip = %d, want 2", ix.wcip)
	}
	if ix.wcppp != 2 {
		t.Errorf("wcppp = %d, want 2", ix.wcppp)
	}

	empty := parseWANC(wancDumpEmpty)
	ix2 := nextIndices(empty)
	if ix2.wancIndex != 1 || ix2.wcip != 1 || ix2.wcppp != 1 {
		t.Errorf("empty next = %+v, want all 1", ix2)
	}
}

func TestPortMaskLabel(t *testing.T) {
	cases := map[int]string{
		15: "LAN1/LAN2/LAN3/LAN4",
		1:  "LAN1",
		8:  "LAN4",
		9:  "LAN1/LAN4",
		0:  "无",
	}
	for m, want := range cases {
		if got := portMaskLabel(m); got != want {
			t.Errorf("portMaskLabel(%d) = %q, want %q", m, got, want)
		}
	}
}
