package zone

import (
	"errors"
	"math"
	"os"
	"testing"

	"github.com/YuujiKamura/AISLIDE/internal/manifest"
	"github.com/YuujiKamura/AISLIDE/internal/model"
)

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

// akitsuZone: 人の赤線 36（h1b5d0a27ea77b246_1_0、東西の長い線）の区間。
func akitsuZone(t *testing.T) (*model.Deck, Frame, []LL, [4]float64) {
	t.Helper()
	d := akitsu(t)
	f, err := FrameOf(d)
	if err != nil {
		t.Fatal(err)
	}
	e, err := d.Get("h1b5d0a27ea77b246_1_0")
	if err != nil || e.Index != 36 {
		t.Fatalf("line 36: %v %+v", err, e)
	}
	x1, y1, x2, y2 := e.LineEnds()
	return d, f, []LL{f.LL(x1, y1), f.LL(x2, y2)}, [4]float64{x1, y1, x2, y2}
}

func TestLengthMatchesMetersBetween(t *testing.T) {
	d, f, path, xy := akitsuZone(t)
	want, err := d.MetersBetween(xy[0], xy[1], xy[2], xy[3])
	if err != nil {
		t.Fatal(err)
	}
	got := Length(f, path)
	if !near(got, want, 0.01) || got < 1000 || got > 2500 {
		t.Fatalf("length %.2fm, MetersBetween %.2fm", got, want)
	}
	// 3 点に割っても長さは同じ（直線上の中間点）。
	mx, my := PointAt(f, path, got/3)
	p3 := []LL{path[0], f.LL(mx, my), path[1]}
	if !near(Length(f, p3), got, 0.01) {
		t.Fatalf("split length %.2f vs %.2f", Length(f, p3), got)
	}
}

func TestPointAndTangent(t *testing.T) {
	_, f, path, xy := akitsuZone(t)
	L := Length(f, path)
	x, y := PointAt(f, path, L/2)
	if !near(x, (xy[0]+xy[2])/2, 1e-6) || !near(y, (xy[1]+xy[3])/2, 1e-6) {
		t.Fatalf("mid (%.3f,%.3f)", x, y)
	}
	if x, y := PointAt(f, path, -5); !near(x, xy[0], 1e-6) || !near(y, xy[1], 1e-6) {
		t.Fatal("s<0 must clamp to start")
	}
	if x, y := PointAt(f, path, L+5); !near(x, xy[2], 1e-6) || !near(y, xy[3], 1e-6) {
		t.Fatal("s>L must clamp to end")
	}
	ux, uy := TangentAt(f, path, L/2)
	l := math.Hypot(xy[2]-xy[0], xy[3]-xy[1])
	if !near(ux, (xy[2]-xy[0])/l, 1e-9) || !near(uy, (xy[3]-xy[1])/l, 1e-9) {
		t.Fatalf("tangent (%.4f,%.4f)", ux, uy)
	}
}

func TestEndNames(t *testing.T) {
	_, _, path, _ := akitsuZone(t)
	// 線 36 は西（左下）→東（右上）。
	ends := EndNames(path)
	if ends["W"] != 0 || ends["E"] != 1 || len(ends) != 2 {
		t.Fatalf("ends %v", ends)
	}
	if path[1][1] <= path[0][1] {
		t.Fatal("E end must have the larger longitude")
	}
	rev := []LL{path[1], path[0]}
	if e := EndNames(rev); e["E"] != 0 || e["W"] != 1 {
		t.Fatalf("reversed ends %v", e)
	}
	ns := []LL{{32.770, 130.78}, {32.768, 130.7801}}
	if e := EndNames(ns); e["N"] != 0 || e["S"] != 1 || len(e) != 2 {
		t.Fatalf("N-S ends %v", e)
	}
}

func TestExtendAndTrim(t *testing.T) {
	_, f, path, _ := akitsuZone(t)
	L := Length(f, path)
	ext, err := Extend(f, path, true, 30)
	if err != nil {
		t.Fatal(err)
	}
	if !near(Length(f, ext)-L, 30, 0.01) || ext[0] != path[0] || len(ext) != 2 {
		t.Fatalf("extend E: +%.3f", Length(f, ext)-L)
	}
	// 延長は端の向きのまま。
	ux, uy := TangentAt(f, path, L)
	vx, vy := TangentAt(f, ext, Length(f, ext))
	if !near(ux, vx, 1e-9) || !near(uy, vy, 1e-9) {
		t.Fatal("extension must keep the end direction")
	}
	w, err := Extend(f, path, false, 30)
	if err != nil || !near(Length(f, w)-L, 30, 0.01) || w[1] != path[1] {
		t.Fatalf("extend W: %v +%.3f", err, Length(f, w)-L)
	}
	tr, err := Extend(f, path, true, -10)
	if err != nil || !near(L-Length(f, tr), 10, 0.01) {
		t.Fatalf("trim: %v -%.3f", err, L-Length(f, tr))
	}
	if _, err := Extend(f, path, true, -L); !errors.Is(err, ErrTooShort) {
		t.Fatalf("over-trim: %v", err)
	}
	// 3 点の折れ線を終点側から最後の区間より長く詰めると、頂点が落ちる。
	mx, my := PointAt(f, path, L*0.8)
	p3 := []LL{path[0], f.LL(mx, my), path[1]}
	tr3, err := Extend(f, p3, true, -L*0.5)
	if err != nil || len(tr3) != 2 || !near(Length(f, tr3), L*0.5, 0.01) {
		t.Fatalf("trim across vertex: %v n=%d len=%.2f", err, len(tr3), Length(f, tr3))
	}
	if r := RemapEnds(map[string]int{"W": 0, "E": 2}, 3, 2); r["E"] != 1 || r["W"] != 0 {
		t.Fatalf("remap %v", r)
	}
}

func TestResolveAnchor(t *testing.T) {
	_, f, path, xy := akitsuZone(t)
	m := &manifest.Manifest{Version: 1,
		Zones: map[string]manifest.Zone{"Z1": {Kind: "closure", Path: path, Ends: EndNames(path)}},
		Sites: map[string]manifest.Site{"S2": {N: 2, At: path[0]}}}
	L := Length(f, path)
	ox, oy := Outward(f, path, 1) // E 端の外向き＝始点→終点の向き
	tx, ty := TangentAt(f, path, L)
	if !near(ox, tx, 1e-9) || !near(oy, ty, 1e-9) {
		t.Fatal("E outward")
	}
	// E 端から外側 50m: 端から 50m 離れ、区間の延長線上。
	x, y, err := Resolve(m, f, manifest.Part{Anchor: "Z1.E", OffsetM: 50})
	if err != nil {
		t.Fatal(err)
	}
	d := f.G.MetersBetween(f.Img, xy[2], xy[3], x, y)
	if !near(d, 50, 0.01) || (x-xy[2])*ox+(y-xy[3])*oy <= 0 {
		t.Fatalf("Z1.E+50m at (%.2f,%.2f), %.2fm from end", x, y, d)
	}
	// 左: E 端へ向かう車（西向き）の左＝南＝紙面の下側。
	xl, yl, _ := Resolve(m, f, manifest.Part{Anchor: "Z1.E", OffsetM: 50, Side: "L"})
	if !near(math.Hypot(xl-x, yl-y), SideOffsetMM, 1e-9) || yl <= y {
		t.Fatalf("side L (%.2f,%.2f) vs (%.2f,%.2f)", xl, yl, x, y)
	}
	xr, yr, _ := Resolve(m, f, manifest.Part{Anchor: "Z1.E", OffsetM: 50, Side: "R"})
	if !near(xr+xl, 2*x, 1e-9) || !near(yr+yl, 2*y, 1e-9) {
		t.Fatal("R must mirror L")
	}
	// W 端の左（東向きの車の左＝北＝上側）、外側は西。
	xw, yw, _ := Resolve(m, f, manifest.Part{Anchor: "Z1.W", OffsetM: 50, Side: "L"})
	if xw >= xy[0] {
		t.Fatalf("W+50m must be west of the W end: %.2f", xw)
	}
	_, yw0, _ := Resolve(m, f, manifest.Part{Anchor: "Z1.W", OffsetM: 50})
	if yw >= yw0 {
		t.Fatal("W side L must be up (north)")
	}
	// 内側 -100m は道なり。
	xi, yi, _ := Resolve(m, f, manifest.Part{Anchor: "Z1.E", OffsetM: -100})
	px, py := PointAt(f, path, L-100)
	if !near(xi, px, 1e-9) || !near(yi, py, 1e-9) {
		t.Fatal("inward offset")
	}
	// Override は最後に足す。
	xo, yo, _ := Resolve(m, f, manifest.Part{Anchor: "Z1.E", OffsetM: 50, Side: "L", Override: [2]float64{3, -2}})
	if !near(xo-xl, 3, 1e-9) || !near(yo-yl, -2, 1e-9) {
		t.Fatal("override")
	}
	// 施工箇所
	xs, ys, err := Resolve(m, f, manifest.Part{Anchor: "S2"})
	if err != nil || !near(xs, xy[0], 1e-6) || !near(ys, xy[1], 1e-6) {
		t.Fatalf("S2: %v", err)
	}
	for _, bad := range []manifest.Part{
		{Anchor: "Z9.E"}, {Anchor: "Z1.N"}, {Anchor: "S9"}, {Anchor: "S2", OffsetM: 5},
		{Anchor: "Z1.E", Side: "X"}, {Anchor: "nothing"},
	} {
		if _, _, err := Resolve(m, f, bad); err == nil {
			t.Errorf("%+v: want error", bad)
		}
	}
}

func TestFR10(t *testing.T) {
	_, _, path, _ := akitsuZone(t)
	m := &manifest.Manifest{Version: 1, Zones: map[string]manifest.Zone{"Z1": {Path: path, Ends: EndNames(path)}}}
	m.Parts = map[string]manifest.Part{
		"Z1.E.予告#1": {Kind: "sign", Key: "③", Override: [2]float64{1, 2}, On: true},
		"Z1.E.予告#2": {Kind: "sign", Key: "⑤", On: true},
	}
	var asked string
	sets, err := ApplyRule(m, "Z1", "FR-10", func(l string) (string, error) { asked = l; return "④", nil })
	if err != nil {
		t.Fatal(err)
	}
	if asked != "50m先" || len(sets) != 2 || sets[0] != "Z1.E.予告" || sets[1] != "Z1.W.予告" {
		t.Fatalf("sets %v asked %q", sets, asked)
	}
	if len(m.Parts) != 2 {
		t.Fatalf("FR-10 needs exactly one sign per approach: %v", m.Parts)
	}
	e := m.Parts["Z1.E.予告#1"]
	if e.Key != "④" || e.Anchor != "Z1.E" || e.OffsetM != 50 || e.Side != "L" || !e.On || e.Override != [2]float64{1, 2} {
		t.Fatalf("E part %+v", e)
	}
	if w := m.Parts["Z1.W.予告#1"]; w.Anchor != "Z1.W" || w.Override != [2]float64{} {
		t.Fatalf("W part %+v", w)
	}
	if s := m.Sets["Z1.W.予告"]; s.Rule != "FR-10" || !s.On {
		t.Fatalf("set %+v", s)
	}
	if _, err := ApplyRule(m, "Z1", "FR-99", nil); err == nil {
		t.Fatal("unknown rule accepted")
	}
	if _, err := ApplyRule(m, "Z1", "FR-10", func(string) (string, error) { return "", model.ErrNotFound }); err == nil {
		t.Fatal("missing legend must be an error")
	}
}
