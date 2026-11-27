package project

import (
	"math"
	"os"
	"regexp"
	"testing"

	"github.com/YuujiKamura/AISLIDE/internal/manifest"
	"github.com/YuujiKamura/AISLIDE/internal/model"
	"github.com/YuujiKamura/AISLIDE/internal/zone"
)

// testID: ID（別担当で実装中）の代わりの決定的な ID。
func testID(unit, role string) string { return "kd_" + unit + "_" + role }

func akitsu(t *testing.T) *model.Deck {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/akitsu_presentation.json")
	if err != nil {
		t.Fatal(err)
	}
	d, err := model.ParseDeck(raw, 1)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

// adopted: 線 36 を Z1 として adopt した manifest（zone adopt と同じ手順）。
func adopted(t *testing.T) (*model.Deck, zone.Frame, *manifest.Manifest) {
	t.Helper()
	d := akitsu(t)
	f, err := zone.FrameOf(d)
	if err != nil {
		t.Fatal(err)
	}
	e, _ := d.Get("36")
	x1, y1, x2, y2 := e.LineEnds()
	path := [][2]float64{f.LL(x1, y1), f.LL(x2, y2)}
	m := &manifest.Manifest{Version: 1, Zones: map[string]manifest.Zone{
		"Z1": {Kind: "closure", Path: path, Ends: zone.EndNames(path), From: e.ObjectID}}}
	return d, f, m
}

func byRole(specs []Spec) map[string]Spec {
	out := map[string]Spec{}
	for _, s := range specs {
		out[s.Unit+"/"+s.Role] = s
	}
	return out
}

func TestZoneFlag(t *testing.T) {
	d, f, m := adopted(t)
	p := Zones{IDFunc: testID}
	specs, err := p.Project(m, d)
	if err != nil {
		t.Fatal(err)
	}
	r := byRole(specs)
	if len(specs) != 3 {
		t.Fatalf("adopted zone (path == base line) needs only the flag, got %d specs: %+v", len(specs), specs)
	}
	L := zone.Length(f, m.Zones["Z1"].Path)
	txt := r["Z1/flag_text"]
	if !regexp.MustCompile(`^L=\d+m$`).MatchString(txt.Text) || txt.Text != FlagText(L) {
		t.Fatalf("flag text %q (L=%.1f)", txt.Text, L)
	}
	if !txt.Movable || txt.ID != "kd_Z1_flag_text" || txt.Create[0].Shape.FontPt != 10 || !txt.Create[0].Shape.Bold {
		t.Fatalf("flag_text spec %+v", txt)
	}
	if tg := txt.Create[0].Tag; tg.Layer != LayerDerived || tg.Params["unit"] != "Z1" || tg.Params["role"] != "flag_text" {
		t.Fatalf("tag %+v", tg)
	}
	lead, under := r["Z1/flag_leader"].Create[0], r["Z1/flag_under"].Create[0]
	// 引出しは中点の近く（上側へ 1mm）から 60° 右上に 12mm 以上（傾いた区間では伸ばす）、下線は水平に 22mm、文字は下線の上。
	mx, my := zone.PointAt(f, m.Zones["Z1"].Path, L/2)
	if math.Hypot(lead.X1-mx, lead.Y1-my) > FlagGapMM+1e-9 {
		t.Fatal("leader must start at the zone midpoint")
	}
	dx, dy := lead.X2-lead.X1, lead.Y2-lead.Y1
	if math.Hypot(dx, dy) < FlagLeaderMM-1e-9 || math.Hypot(dx, dy) > 30 || dx <= 0 || !near(math.Atan2(-dy, dx)*180/math.Pi, 60, 1e-9) {
		t.Fatalf("leader (%.2f,%.2f)", dx, dy)
	}
	if under.X1 != lead.X2 || under.Y1 != lead.Y2 || under.Y2 != under.Y1 || !near(under.X2-under.X1, FlagUnderMM, 1e-9) {
		t.Fatalf("underline %+v", under)
	}
	if !near(txt.Box.Y+txt.Box.H, under.Y1, 1e-9) || txt.Box.X != under.X1 {
		t.Fatalf("text box %+v must sit on the underline", txt.Box)
	}
	// 文字枠は区間線に載らない: 枠の 4 隅がすべて区間線の上側（法線の正の側）。
	e, _ := d.Get("36")
	x1, y1, x2, y2 := e.LineEnds()
	for _, c := range [][2]float64{{txt.Box.X, txt.Box.Y}, {txt.Box.X + txt.Box.W, txt.Box.Y},
		{txt.Box.X, txt.Box.Y + txt.Box.H}, {txt.Box.X + txt.Box.W, txt.Box.Y + txt.Box.H}} {
		// 紙面 y 下向き: 上側は cross < 0。すき間 FlagClearMM 以上。
		l := math.Hypot(x2-x1, y2-y1)
		if dist := -((x2-x1)*(c[1]-y1) - (y2-y1)*(c[0]-x1)) / l; dist < FlagClearMM-1e-9 {
			t.Fatalf("flag corner %v is on/below the zone line", c)
		}
	}
	if !p.Owns("Z1") || p.Owns("Z1.E") || p.Owns("Z1.E.予告#1") {
		t.Fatal("Owns")
	}
	if st := StaleBases(m, d); len(st) != 0 {
		t.Fatalf("not stale yet: %v", st)
	}
}

func TestZoneExtendMovesFlagAndOverlays(t *testing.T) {
	d, f, m := adopted(t)
	p := Zones{IDFunc: testID}
	before, _ := p.Project(m, d)
	z := m.Zones["Z1"]
	L0 := zone.Length(f, z.Path)
	np, err := zone.Extend(f, z.Path, z.Ends["E"] != 0, 30)
	if err != nil {
		t.Fatal(err)
	}
	z.Path = np
	m.Zones["Z1"] = z
	L1 := zone.Length(f, np)
	if !near(L1-L0, 30, 0.5) {
		t.Fatalf("extend +%.2f", L1-L0)
	}
	after, err := p.Project(m, d)
	if err != nil {
		t.Fatal(err)
	}
	rb, ra := byRole(before), byRole(after)
	if ra["Z1/flag_text"].Text != FlagText(L1) || ra["Z1/flag_text"].Text == rb["Z1/flag_text"].Text {
		t.Fatalf("flag text %q -> %q", rb["Z1/flag_text"].Text, ra["Z1/flag_text"].Text)
	}
	// 中点は 15m 分東へ（紙面上 ≈ 15m × 縮尺）。
	mmPerM := f.MMPerMeterAlong(zone.TangentAt(f, np, 0))
	moved := math.Hypot(ra["Z1/flag_text"].Box.X-rb["Z1/flag_text"].Box.X, ra["Z1/flag_text"].Box.Y-rb["Z1/flag_text"].Box.Y)
	if !near(moved, 15*mmPerM, 0.01) {
		t.Fatalf("flag moved %.3fmm, want %.3f", moved, 15*mmPerM)
	}
	// 人の線と食い違うので派生の赤線が 1 本出て、base stale を報告する。
	line, ok := ra["Z1/line"]
	if !ok || len(after) != 4 {
		t.Fatalf("want overlay line, got %d specs", len(after))
	}
	op := line.Create[0]
	if op.Kind != model.OpCreateLine || op.Line.Color.R < 0.8 || op.Line.Weight != 4 {
		t.Fatalf("overlay %+v", op)
	}
	ex, ey := f.MM(np[1])
	if !near(op.X2, ex, 1e-9) || !near(op.Y2, ey, 1e-9) {
		t.Fatal("overlay must end at the new E end")
	}
	if st := StaleBases(m, d); len(st) != 1 || st[0] != "Z1=h1b5d0a27ea77b246_1_0" {
		t.Fatalf("stale %v", st)
	}
}

func TestZoneFlagDrift(t *testing.T) {
	d, _, m := adopted(t)
	p := Zones{IDFunc: testID}
	before := byRole(mustProject(t, p, m, d))
	if !p.RecordDrift(m, Drift{Unit: "Z1", Role: "flag_text", DX: 4, DY: -3}) {
		t.Fatal("flag_text drift not recorded")
	}
	if p.RecordDrift(m, Drift{Unit: "Z1", Role: "flag_leader", DX: 1}) || p.RecordDrift(m, Drift{Unit: "Z9", Role: "flag_text"}) {
		t.Fatal("only flag_text of an existing zone records drift")
	}
	if m.Zones["Z1"].Flag != [2]float64{4, -3} {
		t.Fatalf("Zone.Flag %v", m.Zones["Z1"].Flag)
	}
	after := byRole(mustProject(t, p, m, d))
	bt, at := before["Z1/flag_text"].Box, after["Z1/flag_text"].Box
	if !near(at.X-bt.X, 4, 1e-9) || !near(at.Y-bt.Y, -3, 1e-9) {
		t.Fatal("text must move by the override")
	}
	bl, al := before["Z1/flag_leader"].Create[0], after["Z1/flag_leader"].Create[0]
	if al.X1 != bl.X1 || al.Y1 != bl.Y1 || !near(al.X2-bl.X2, 4, 1e-9) {
		t.Fatal("leader start stays on the zone, its end follows the text")
	}
}

func mustProject(t *testing.T, p Projector, m *manifest.Manifest, d *model.Deck) []Spec {
	t.Helper()
	s, err := p.Project(m, d)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
