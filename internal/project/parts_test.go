package project

import (
	"math"
	"testing"

	"github.com/YuujiKamura/AISLIDE/internal/manifest"
	"github.com/YuujiKamura/AISLIDE/internal/model"
	"github.com/YuujiKamura/AISLIDE/internal/zone"
)

// fr10: 線 36 の Z1 に set apply FR-10 を当てた manifest（凡例キーはデッキの「50m先」ラベルから）。
func fr10(t *testing.T) (*model.Deck, zone.Frame, *manifest.Manifest) {
	t.Helper()
	d, f, m := adopted(t)
	if _, err := zone.ApplyRule(m, "Z1", "FR-10", func(l string) (string, error) { return LegendKeyByLabel(m, d, l) }); err != nil {
		t.Fatal(err)
	}
	return d, f, m
}

func TestLegendKeyByLabel(t *testing.T) {
	d := akitsu(t)
	if k, err := LegendKeyByLabel(nil, d, "50m先"); err != nil || k != "④" {
		t.Fatalf("50m先 -> %q %v", k, err)
	}
	if k, _ := LegendKeyByLabel(nil, d, "100m先"); k != "③" {
		t.Fatalf("100m先 -> %q", k)
	}
	m := &manifest.Manifest{Legend: &manifest.Legend{Items: []manifest.LegendItem{{Key: "⑨", Label: "50m先工事中"}}}}
	if k, _ := LegendKeyByLabel(m, d, "50m先"); k != "⑨" {
		t.Fatal("manifest legend must win")
	}
	if _, err := LegendKeyByLabel(nil, d, "200m先"); err == nil {
		t.Fatal("absent label accepted")
	}
}

func TestPartsFR10Signs(t *testing.T) {
	d, f, m := fr10(t)
	p := Parts{IDFunc: testID}
	specs := mustProject(t, p, m, d)
	r := byRole(specs)
	if len(m.Parts) != 2 || len(specs) != 4 {
		t.Fatalf("FR-10: want 2 parts (E, W) -> 4 specs, got %d parts %d specs", len(m.Parts), len(specs))
	}
	src, _ := d.Get("p1_i21") // ④50m先工事中 の看板画像
	z := m.Zones["Z1"]
	for _, end := range []string{"E", "W"} {
		u := "Z1." + end + ".予告#1"
		if m.Parts[u].Key != "④" {
			t.Fatalf("%s key %q", u, m.Parts[u].Key)
		}
		sg, bd := r[u+"/sign"], r[u+"/badge"]
		op := sg.Create[0]
		if op.Kind != model.OpDuplicate || op.SourceID != src.ObjectID || !sg.Movable || bd.Movable {
			t.Fatalf("%s sign %+v", u, op)
		}
		if !near(sg.Box.W, PartSignWidthMM, 1e-9) || !near(sg.Box.H/sg.Box.W, src.Box.H/src.Box.W, 1e-9) {
			t.Fatalf("%s sign size %+v", u, sg.Box)
		}
		if bd.Text != "4" || !near(bd.Box.X+bd.Box.W/2, sg.Box.X+sg.Box.W, 1e-9) {
			t.Fatalf("%s badge %+v", u, bd)
		}
		// 端から外側（接近側）に、道なり 50m ＋左 6mm。
		ei := z.Ends[end]
		ex, ey := f.MM(z.Path[ei])
		ox, oy := zone.Outward(f, z.Path, ei)
		cx, cy := sg.Box.X+sg.Box.W/2, sg.Box.Y+sg.Box.H/2
		along := (cx-ex)*ox + (cy-ey)*oy
		if want := 50 * f.MMPerMeterAlong(ox, oy); !near(along, want, 1e-6) {
			t.Fatalf("%s: %.3fmm outward, want %.3f", u, along, want)
		}
		if side := (cx-ex)*(-oy) + (cy-ey)*ox; !near(side, zone.SideOffsetMM, 1e-6) {
			t.Fatalf("%s: side offset %.3f", u, side)
		}
	}
	// E は東へ、W は西へ。
	if r["Z1.E.予告#1/sign"].Box.X <= r["Z1.W.予告#1/sign"].Box.X+100 {
		t.Fatal("E sign must be far east of W sign")
	}
}

func TestPartsFollowZoneExtend(t *testing.T) {
	d, f, m := fr10(t)
	p := Parts{IDFunc: testID}
	before := byRole(mustProject(t, p, m, d))
	z := m.Zones["Z1"]
	np, _ := zone.Extend(f, z.Path, true, 30)
	z.Path = np
	m.Zones["Z1"] = z
	after := byRole(mustProject(t, p, m, d))
	b, a := before["Z1.E.予告#1/sign"].Box, after["Z1.E.予告#1/sign"].Box
	ox, oy := zone.Outward(f, np, 1)
	if got := (a.X-b.X)*ox + (a.Y-b.Y)*oy; !near(got, 30*f.MMPerMeterAlong(ox, oy), 1e-6) {
		t.Fatalf("E sign moved %.3fmm along the road", got)
	}
	if bw, aw := before["Z1.W.予告#1/sign"].Box, after["Z1.W.予告#1/sign"].Box; !near(bw.X, aw.X, 1e-6) || !near(bw.Y, aw.Y, 1e-6) {
		t.Fatal("W sign must not move")
	}
}

func TestPartsSwitchesAndDrift(t *testing.T) {
	d, _, m := fr10(t)
	p := Parts{IDFunc: testID}
	base := byRole(mustProject(t, p, m, d))
	// set off → その部品は投影されない（Run が派生要素を消す）。
	sw := m.Sets["Z1.E.予告"]
	sw.On = false
	m.Sets["Z1.E.予告"] = sw
	r := byRole(mustProject(t, p, m, d))
	if _, ok := r["Z1.E.予告#1/sign"]; ok || len(r) != 2 {
		t.Fatalf("set off: %d specs", len(r))
	}
	sw.On = true
	m.Sets["Z1.E.予告"] = sw
	// 人のドラッグ（Drift）と nudge は同じ Override に足される。
	if !p.RecordDrift(m, Drift{Unit: "Z1.E.予告#1", Role: "sign", DX: 2, DY: -1.5}) {
		t.Fatal("sign drift not recorded")
	}
	if p.RecordDrift(m, Drift{Unit: "Z1.E.予告#1", Role: "badge", DX: 9}) || p.RecordDrift(m, Drift{Unit: "nope#1", Role: "sign"}) {
		t.Fatal("badge / unknown unit must not record")
	}
	pt := m.Parts["Z1.E.予告#1"]
	pt.Override[0] += 1 // part nudge 1 0
	m.Parts["Z1.E.予告#1"] = pt
	r = byRole(mustProject(t, p, m, d))
	b0, b1 := base["Z1.E.予告#1/sign"].Box, r["Z1.E.予告#1/sign"].Box
	if !near(b1.X-b0.X, 3, 1e-9) || !near(b1.Y-b0.Y, -1.5, 1e-9) {
		t.Fatalf("override shift (%.3f,%.3f)", b1.X-b0.X, b1.Y-b0.Y)
	}
	g0, g1 := base["Z1.E.予告#1/badge"].Box, r["Z1.E.予告#1/badge"].Box
	if !near(g1.X-g0.X, 3, 1e-9) || !near(g1.Y-g0.Y, -1.5, 1e-9) {
		t.Fatal("badge must follow the sign")
	}
	if !p.Owns("Z1.E.予告#1") || !p.Owns("P1") || p.Owns("Z1") {
		t.Fatal("Owns")
	}
}

func TestPartsGuardAndBadgeNumber(t *testing.T) {
	d, _, m := fr10(t)
	m.Parts["P1"] = manifest.Part{Kind: "guard", Key: "b", Anchor: "Z1.W", OffsetM: -5, On: true}
	m.Parts["P2"] = manifest.Part{Kind: "guard", Anchor: "Z1.W", On: false}
	m.Legend = &manifest.Legend{Mode: "final", Items: []manifest.LegendItem{
		{Key: "①", On: true}, {Key: "②", On: true}, {Key: "③", On: false}, {Key: "④", On: true}}}
	r := byRole(mustProject(t, Parts{IDFunc: testID}, m, d))
	if g := r["P1/guard"]; g.Text != "B" || !g.Movable || g.Create[0].Shape.ShapeType != "ELLIPSE" {
		t.Fatalf("guard %+v", g)
	}
	if _, ok := r["P2/guard"]; ok {
		t.Fatal("part with On=false must not be projected")
	}
	if b := r["Z1.E.予告#1/badge"]; b.Text != "3" {
		t.Fatalf("final legend renumbers ④ -> 3, got %q", b.Text)
	}
	m.Legend.Mode = "draft"
	if b := byRole(mustProject(t, Parts{IDFunc: testID}, m, d))["Z1.E.予告#1/badge"]; b.Text != "4" {
		t.Fatalf("draft keeps the original number, got %q", b.Text)
	}
	if math.IsNaN(r["P1/guard"].Box.X) {
		t.Fatal("NaN")
	}
}
