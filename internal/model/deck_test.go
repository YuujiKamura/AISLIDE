package model

import (
	"errors"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"
)

func loadAkitsu(t *testing.T) *Deck {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/akitsu_presentation.json")
	if err != nil {
		t.Fatal(err)
	}
	d, err := ParseDeck(raw, 1)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func TestParseDeckAkitsu(t *testing.T) {
	d := loadAkitsu(t)
	if len(d.Elements) != 40 {
		t.Fatalf("elements = %d, want 40", len(d.Elements))
	}
	if d.PageID != "p1" || d.Rev == "" || d.PresID == "" {
		t.Fatalf("ids: page=%q rev=%q pres=%q", d.PageID, d.Rev, d.PresID)
	}
	if !near(d.PageW, 420, 1e-9) || !near(d.PageH, 297, 1e-9) {
		t.Fatalf("page = %vx%v, want 420x297", d.PageW, d.PageH)
	}
	for i, e := range d.Elements {
		if e.Index != i {
			t.Fatalf("index %d != %d", e.Index, i)
		}
		if e.Box.W < 0 || e.Box.H < 0 {
			t.Fatalf("%s: negative box %+v", e.ObjectID, e.Box)
		}
	}
}

func TestMapBGLegacyGeoref(t *testing.T) {
	d := loadAkitsu(t)
	e, g, err := d.MapBG()
	if err != nil {
		t.Fatal(err)
	}
	if e.ObjectID != "p1_i4" || e.Kind != "image" || e.Layer() != LayerMap || e.Tag.Kind != "map_bg" {
		t.Fatalf("bg = %s kind=%s layer=%s", e.ObjectID, e.Kind, e.Layer())
	}
	if g.Z != 17 || g.Center != [2]float64{32.76958, 130.77837} || g.Px != [2]int{2048, 1024} || len(g.Sites) != 3 {
		t.Fatalf("georef = %+v", g)
	}
	// Box: translate 377999.75 / 611550 EMU, size 51200x25600 * scale。
	want := Box{377999.75 / 36000, 611550.0 / 36000, 51200 * 280.8984 / 36000, 25600 * 317.3271 / 36000}
	if !near(e.Box.X, want.X, 1e-9) || !near(e.Box.Y, want.Y, 1e-9) || !near(e.Box.W, want.W, 1e-9) || !near(e.Box.H, want.H, 1e-9) {
		t.Fatalf("bg box = %+v, want %+v", e.Box, want)
	}
}

func TestLayersOfFixture(t *testing.T) {
	d := loadAkitsu(t)
	d.Owners["signs"] = "agent1"
	lines := 0
	for i := range d.Elements {
		e := &d.Elements[i]
		switch {
		case strings.HasPrefix(e.ObjectID, "h1b5d0a27ea77b246_1_"):
			lines++
			if e.Kind != "line" || e.Layer() != LayerBase || e.Tag != nil {
				t.Errorf("%s: kind=%s layer=%s", e.ObjectID, e.Kind, e.Layer())
			}
			for _, agent := range []string{"agent1", "", "human"} {
				if err := d.CanWrite(e, agent); !errors.Is(err, ErrReadOnly) {
					t.Errorf("CanWrite(%s,%q) = %v, want ErrReadOnly", e.ObjectID, agent, err)
				}
			}
			if err := d.CanWriteSystem(e); !errors.Is(err, ErrReadOnly) {
				t.Errorf("CanWriteSystem(%s) = %v, want ErrReadOnly", e.ObjectID, err)
			}
		case strings.HasPrefix(e.ObjectID, "kl_mark_"):
			if e.Layer() != LayerBase || e.Kind != "shape" {
				t.Errorf("%s: layer=%s", e.ObjectID, e.Layer())
			}
		case strings.HasPrefix(e.ObjectID, LegacyMaskPrefix):
			if e.Layer() != LayerMap || e.Tag.Kind != "mask" {
				t.Errorf("%s: layer=%s", e.ObjectID, e.Layer())
			}
			if err := d.CanWrite(e, "agent1"); !errors.Is(err, ErrReserved) {
				t.Errorf("CanWrite(mask) = %v, want ErrReserved", err)
			}
			if err := d.CanWriteSystem(e); err != nil {
				t.Errorf("CanWriteSystem(mask) = %v", err)
			}
		}
	}
	if lines != 4 {
		t.Fatalf("human lines = %d, want 4", lines)
	}
}

func TestLineFlip(t *testing.T) {
	d := loadAkitsu(t)
	e, err := d.Get("h1b5d0a27ea77b246_1_0")
	if err != nil {
		t.Fatal(err)
	}
	// scaleY=-1.6257 → 上向きの線。Box は正、Flip[1]=true。
	wantH := 1.6257 * 3000000 / 36000
	if !e.Flip[1] || e.Flip[0] || !near(e.Box.H, wantH, 1e-9) || !near(e.Box.Y, 6590475.0/36000-wantH, 1e-9) {
		t.Fatalf("line box=%+v flip=%v", e.Box, e.Flip)
	}
	x1, y1, x2, y2 := e.LineEnds()
	if !near(x1, 2468600.0/36000, 1e-9) || !near(y1, 6590475.0/36000, 1e-9) || x2 <= x1 || y2 >= y1 {
		t.Fatalf("ends = (%v,%v)-(%v,%v)", x1, y1, x2, y2)
	}
	if e.Line == nil || e.Line.Color != (RGB{1, 0, 0}) || !near(e.Line.Weight, 3, 1e-9) || e.Line.Dash != "SOLID" {
		t.Fatalf("line style = %+v", e.Line)
	}
}

func TestTextAndStyle(t *testing.T) {
	d := loadAkitsu(t)
	cases := []struct{ ref, text string }{
		{"p1_i18", "②通り抜け不可"},
		{"kl_mark_7af978ea", "2"},
		{"p1_i4", ""},
	}
	for _, c := range cases {
		e, err := d.Get(c.ref)
		if err != nil {
			t.Fatal(err)
		}
		if e.Text != c.text {
			t.Errorf("%s text = %q, want %q", c.ref, e.Text, c.text)
		}
	}
	m, _ := d.Get("kl_mark_7af978ea")
	s := m.Shape
	if s == nil || s.ShapeType != "ELLIPSE" || s.Fill == nil || !near(s.Fill.R, 0.85098, 1e-4) || s.Outline == nil ||
		*s.Outline != (RGB{1, 1, 1}) || !near(s.OutlinePt, 1.5, 1e-9) || !s.Bold || s.FontPt != 13 || !s.AlignCenter {
		t.Fatalf("marker style = %+v", s)
	}
	mask, _ := d.Get("kl_mask_top")
	if mask.Shape.Outline != nil || mask.Shape.Fill == nil {
		t.Fatalf("mask style = %+v", mask.Shape)
	}
}

func TestGet(t *testing.T) {
	d := loadAkitsu(t)
	cases := []struct {
		ref  string
		want string // "" = ErrNotFound
	}{
		{"0", "p1_i4"},
		{"39", "h1b5d0a27ea77b246_1_3"},
		{" 36 ", "h1b5d0a27ea77b246_1_0"},
		{"p1_i18", "p1_i18"},
		{"40", ""},
		{"-1", ""},
		{"p1_i", ""},    // 部分一致はしない
		{"kl_mark", ""}, // 部分一致はしない
		{"通り抜け", ""},    // 文字検索はしない
		{"", ""},
	}
	for _, c := range cases {
		e, err := d.Get(c.ref)
		if c.want == "" {
			if !errors.Is(err, ErrNotFound) {
				t.Errorf("Get(%q) = %v, %v; want ErrNotFound", c.ref, e, err)
			}
			continue
		}
		if err != nil || e.ObjectID != c.want {
			t.Errorf("Get(%q) = %v, %v; want %s", c.ref, e, err, c.want)
		}
	}
}

func TestClaimAndCanWrite(t *testing.T) {
	d := &Deck{Owners: map[string]string{}, Elements: []Element{
		{Index: 0, ObjectID: "kc_signs_00000001", Tag: &Tag{Layer: "signs", Owner: "agent1", Kind: "sign"}},
		{Index: 1, ObjectID: "kc_cones_00000001", Tag: &Tag{Layer: "cones", Owner: "agent2", Kind: "cone_line"}},
		{Index: 2, ObjectID: "kc__cursor_00000001", Tag: &Tag{Layer: LayerCursor, Kind: "cursor"}},
	}}
	claims := []struct {
		layer, agent string
		want         error
	}{
		{"base", "agent1", ErrReserved},
		{"_cursor", "agent1", ErrReserved},
		{"map", "agent1", ErrReserved},
		{"signs", "agent1", nil},
		{"signs", "agent1", nil},         // 冪等
		{"signs", "agent2", ErrNotOwner}, // Owners
		{"cones", "agent1", ErrNotOwner}, // タグ owner（未 claim でも保護）
		{"notes", "agent2", nil},
	}
	for _, c := range claims {
		err := d.Claim(c.layer, c.agent)
		if c.want == nil && err != nil || c.want != nil && !errors.Is(err, c.want) {
			t.Errorf("Claim(%s,%s) = %v, want %v", c.layer, c.agent, err, c.want)
		}
	}
	for _, bad := range []string{"", "a b", "x/y", "_x", strings.Repeat("a", 33)} {
		if err := d.Claim(bad, "agent1"); err == nil {
			t.Errorf("Claim(%q) accepted", bad)
		}
	}
	if err := d.Claim("zzz", ""); err == nil {
		t.Error("Claim without agent accepted")
	}

	writes := []struct {
		i     int
		agent string
		want  error
	}{
		{0, "agent1", nil},
		{0, "agent2", ErrNotOwner},
		{0, "", ErrNotOwner},
		{1, "agent2", nil}, // タグ owner
		{1, "agent1", ErrNotOwner},
		{2, "agent1", ErrReserved},
	}
	for _, c := range writes {
		err := d.CanWrite(&d.Elements[c.i], c.agent)
		if c.want == nil && err != nil || c.want != nil && !errors.Is(err, c.want) {
			t.Errorf("CanWrite(%d,%s) = %v, want %v", c.i, c.agent, err, c.want)
		}
	}
	if err := d.CanWriteSystem(&d.Elements[2]); err != nil {
		t.Errorf("CanWriteSystem(cursor) = %v", err)
	}
	if err := d.CanWriteSystem(&d.Elements[0]); !errors.Is(err, ErrNotOwner) {
		t.Errorf("CanWriteSystem(agent layer) = %v", err)
	}
}

func TestTagParsing(t *testing.T) {
	cases := []struct {
		desc      string
		layer     string // "" = nil Tag
		kind      string
		hasGeoref bool
	}{
		{`kisei:{"layer":"signs","owner":"a","kind":"sign","params":{"key":"②"}}`, "signs", "sign", false},
		{`kisei:{"layer":"map","kind":"map_bg","georef":{"z":18,"center":[1,2],"px":[10,20]}}`, "map", "map_bg", true},
		{`kisei_live:{"z": 17, "center": [32.7, 130.7], "px": [2048, 1024]}`, "map", "map_bg", true},
		{`kisei:{broken`, "", "", false},
		{`kisei:{"kind":"x"}`, "", "", false}, // layer 無し
		{`guardman.png`, "", "", false},
		{``, "", "", false},
	}
	for _, c := range cases {
		tg := parseTag(c.desc)
		if c.layer == "" {
			if tg != nil {
				t.Errorf("%q: tag = %+v, want nil", c.desc, tg)
			}
			continue
		}
		if tg == nil || tg.Layer != c.layer || tg.Kind != c.kind || (tg.Georef != nil) != c.hasGeoref {
			t.Errorf("%q: tag = %+v", c.desc, tg)
		}
	}
	s, err := EncodeTag(&Tag{Layer: "signs", Owner: "a", Kind: "sign"})
	if err != nil || s != `kisei:{"layer":"signs","owner":"a","kind":"sign"}` {
		t.Fatalf("EncodeTag = %q, %v", s, err)
	}
	if back := parseTag(s); back == nil || back.Owner != "a" {
		t.Fatalf("EncodeTag round trip = %+v", back)
	}
}

func TestResolvePoint(t *testing.T) {
	d := loadAkitsu(t)
	bg, g, _ := d.MapBG()
	// 地図枠 = マスクの内側（x 10..410, y 16..220）。
	frame := Box{10, 16, 400, 204}
	in := func(x, y float64) bool {
		return x > frame.X && x < frame.X+frame.W && y > frame.Y && y < frame.Y+frame.H
	}
	cx, cy := bg.Center()
	cases := []struct {
		pt     string
		x, y   float64 // NaN = 地図枠内であることだけ確認
		hasErr bool
	}{
		{"mm:120.5,88", 120.5, 88, false},
		{" mm: 1 , 2 ", 1, 2, false},
		{"32.76958,130.77837", cx, cy, false},
		{"site:2", math.NaN(), 0, false},
		{"site:1", math.NaN(), 0, false},
		{"site:3", math.NaN(), 0, false},
		{"site:9", 0, 0, true},
		{"mm:1", 0, 0, true},
		{"abc", 0, 0, true},
		{"95,130", 0, 0, true},
	}
	for _, c := range cases {
		x, y, err := d.ResolvePoint(c.pt)
		if c.hasErr {
			if err == nil {
				t.Errorf("ResolvePoint(%q) = %v,%v; want error", c.pt, x, y)
			}
			continue
		}
		if err != nil {
			t.Errorf("ResolvePoint(%q): %v", c.pt, err)
			continue
		}
		if math.IsNaN(c.x) {
			if !in(x, y) {
				t.Errorf("ResolvePoint(%q) = (%v,%v) outside map frame", c.pt, x, y)
			}
			continue
		}
		if !near(x, c.x, 1e-6) || !near(y, c.y, 1e-6) {
			t.Errorf("ResolvePoint(%q) = (%v,%v), want (%v,%v)", c.pt, x, y, c.x, c.y)
		}
	}
	// site:2 は georef の座標と同じ点。
	x, y, _ := d.ResolvePoint("site:2")
	s2 := g.Sites["2"]
	x2, y2, _ := d.ResolvePoint(formatLatLon(s2[0], s2[1]))
	if !near(x, x2, 1e-9) || !near(y, y2, 1e-9) {
		t.Fatalf("site:2 (%v,%v) != latlon (%v,%v)", x, y, x2, y2)
	}
	// 往復: mm → latlon → mm。
	lat, lon, err := d.ToLatLon(x, y)
	if err != nil || !near(lat, s2[0], 1e-9) || !near(lon, s2[1], 1e-9) {
		t.Fatalf("ToLatLon = %v,%v,%v", lat, lon, err)
	}

	// georef の無いデッキでは lat,lon / site は error、mm は可。
	empty := &Deck{}
	if _, _, err := empty.ResolvePoint("site:1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("no georef site: %v", err)
	}
	if _, _, err := empty.ResolvePoint("32.7,130.7"); !errors.Is(err, ErrNotFound) {
		t.Errorf("no georef latlon: %v", err)
	}
	if _, _, err := empty.ResolvePoint("mm:3,4"); err != nil {
		t.Errorf("no georef mm: %v", err)
	}
}

func formatLatLon(lat, lon float64) string {
	return strconv.FormatFloat(lat, 'g', -1, 64) + "," + strconv.FormatFloat(lon, 'g', -1, 64)
}

func TestResolveLength(t *testing.T) {
	d := loadAkitsu(t)
	bg, g, _ := d.MapBG()
	mpp := 156543.03392 * math.Cos(g.Center[0]*math.Pi/180) / math.Exp2(float64(g.Z))
	perM := bg.Box.W / float64(g.Px[0]) / mpp
	cases := []struct {
		s      string
		want   float64
		hasErr bool
	}{
		{"12mm", 12, false},
		{" 12.5mm ", 12.5, false},
		{"40m", 40 * perM, false},
		{"0m", 0, false},
		{"40", 0, true},
		{"m", 0, true},
		{"xmm", 0, true},
	}
	for _, c := range cases {
		v, err := d.ResolveLength(c.s)
		if c.hasErr {
			if err == nil {
				t.Errorf("ResolveLength(%q) = %v; want error", c.s, v)
			}
			continue
		}
		if err != nil || !near(v, c.want, 1e-9) {
			t.Errorf("ResolveLength(%q) = %v, %v; want %v", c.s, v, err, c.want)
		}
	}
	// 40m は紙面で 7〜8mm 程度（z17・横 2048px を 400mm に載せた縮尺）。
	if v, _ := d.ResolveLength("40m"); v < 7 || v > 8.5 {
		t.Fatalf("40m = %vmm", v)
	}
	if _, err := (&Deck{}).ResolveLength("40m"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no georef 40m: %v", err)
	}
}

func TestParseDeckErrors(t *testing.T) {
	if _, err := ParseDeck([]byte(`{`), 1); err == nil {
		t.Error("bad json accepted")
	}
	if _, err := ParseDeck([]byte(`{"slides":[]}`), 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("no slide: %v", err)
	}
	raw, _ := os.ReadFile("../../testdata/akitsu_presentation.json")
	if _, err := ParseDeck(raw, 2); !errors.Is(err, ErrNotFound) {
		t.Errorf("slide 2: %v", err)
	}
}
