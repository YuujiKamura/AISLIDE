package parts

import (
	"errors"
	"fmt"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"

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

func ctxFor(d *model.Deck) Ctx {
	n := 0
	return Ctx{Deck: d, Layer: "signs", Agent: "agent1", NewID: func(layer string) string {
		n++
		return fmt.Sprintf("kc_%s_%08x", layer, n)
	}}
}

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func center(b model.Box) (float64, float64) { return b.X + b.W/2, b.Y + b.H/2 }

func TestKinds(t *testing.T) {
	want := []string{"closure", "cone_line", "detour", "guard", "note", "sign", "site"}
	if got := Kinds(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Kinds = %v", got)
	}
	_, err := Build("arrow", map[string]string{}, ctxFor(akitsu(t)))
	if err == nil {
		t.Fatal("unknown kind accepted")
	}
	for _, k := range want {
		if !strings.Contains(err.Error(), k) {
			t.Errorf("unknown-kind error %q does not list %s", err, k)
		}
	}
}

func TestTagsAndIDs(t *testing.T) {
	d := akitsu(t)
	cases := []struct {
		kind string
		args string
	}{
		{"site", "n=2 at=site:2"},
		{"sign", "key=② at=site:2"},
		{"cone_line", "from=site:1 to=site:2"},
		{"guard", "k=A at=site:3"},
		{"closure", "path=site:1;site:2;site:3"},
		{"detour", "path=site:1;site:3"},
		{"note", `at=mm:100,100 t="片側交互通行"`},
	}
	for _, c := range cases {
		args := ParseArgs(c.args)
		ops, err := Build(c.kind, args, ctxFor(d))
		if err != nil {
			t.Fatalf("%s: %v", c.kind, err)
		}
		if len(ops) == 0 {
			t.Fatalf("%s: no ops", c.kind)
		}
		seen := map[string]bool{}
		for _, op := range ops {
			if op.Tag == nil || op.Tag.Layer != "signs" || op.Tag.Owner != "agent1" || op.Tag.Kind != c.kind {
				t.Errorf("%s: tag = %+v", c.kind, op.Tag)
				continue
			}
			for k, v := range args {
				if op.Tag.Params[k] != v {
					t.Errorf("%s: params[%s] = %v, want %s", c.kind, k, op.Tag.Params[k], v)
				}
			}
			if !strings.HasPrefix(op.ObjectID, "kc_signs_") || seen[op.ObjectID] {
				t.Errorf("%s: objectId %q", c.kind, op.ObjectID)
			}
			seen[op.ObjectID] = true
		}
	}
}

func TestBuildCtxErrors(t *testing.T) {
	d := akitsu(t)
	args := map[string]string{"n": "1", "at": "site:1"}
	for _, layer := range []string{"", "base", "map", "_cursor"} {
		c := ctxFor(d)
		c.Layer = layer
		if _, err := Build("site", args, c); err == nil {
			t.Errorf("layer %q accepted", layer)
		}
	}
	c := ctxFor(d)
	c.Agent = ""
	if _, err := Build("site", args, c); err == nil {
		t.Error("empty agent accepted")
	}
}

func TestSite(t *testing.T) {
	d := akitsu(t)
	sx, sy, _ := d.ResolvePoint("site:2")
	ops, err := Build("site", ParseArgs("n=2 at=site:2"), ctxFor(d))
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 {
		t.Fatalf("ops = %d", len(ops))
	}
	op := ops[0]
	s := op.Shape
	cx, cy := center(op.Box)
	if op.Kind != model.OpCreateShape || s.ShapeType != "ELLIPSE" || !near(op.Box.W, 10, 1e-9) || !near(op.Box.H, 10, 1e-9) ||
		!near(cx, sx, 1e-9) || !near(cy, sy, 1e-9) {
		t.Fatalf("site op = %+v", op)
	}
	if *s.Fill != ColorRed || *s.Outline != ColorWhite || s.OutlinePt != 1.5 || s.Text != "2" || s.FontPt != 14 ||
		!s.Bold || s.FontColor != ColorWhite || !s.AlignCenter {
		t.Fatalf("site style = %+v", s)
	}
	for _, bad := range []string{"n=② at=site:2", "n=a at=site:2", "n= at=site:2", "n=2", "n=2 at=site:9"} {
		if _, err := Build("site", ParseArgs(bad), ctxFor(d)); err == nil {
			t.Errorf("site %q accepted", bad)
		}
	}
}

func TestFindLegendSign(t *testing.T) {
	d := akitsu(t)
	want := map[string]string{
		"①": "p1_i15", "②": "p1_i17", "③": "p1_i19", "④": "p1_i21",
		"⑤": "p1_i23", "⑥": "p1_i25", "⑦": "p1_i27", "⑧": "p1_i29",
	}
	for key, id := range want {
		r, _, err := keyRune(key)
		if err != nil {
			t.Fatal(err)
		}
		e, err := FindLegendSign(d, r)
		if err != nil || e.ObjectID != id {
			t.Errorf("FindLegendSign(%s) = %v, %v; want %s", key, e, err, id)
		}
	}
	if _, err := FindLegendSign(d, '⑨'); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("⑨: %v", err)
	}
}

func TestSign(t *testing.T) {
	d := akitsu(t)
	src, _ := d.Get("p1_i17")
	aspect := src.Box.H / src.Box.W
	cases := []struct {
		args string
		w    float64
	}{
		{"key=② at=mm:200,100", 12},
		{"key=2 at=mm:200,100", 12},
		{"key=② at=mm:200,100 w=20mm", 20},
	}
	for _, c := range cases {
		ops, err := Build("sign", ParseArgs(c.args), ctxFor(d))
		if err != nil {
			t.Fatalf("%s: %v", c.args, err)
		}
		if len(ops) != 2 {
			t.Fatalf("%s: ops = %d", c.args, len(ops))
		}
		dup, badge := ops[0], ops[1]
		cx, cy := center(dup.Box)
		if dup.Kind != model.OpDuplicate || dup.SourceID != "p1_i17" || !near(dup.Box.W, c.w, 1e-9) ||
			!near(dup.Box.H, c.w*aspect, 1e-9) || !near(cx, 200, 1e-9) || !near(cy, 100, 1e-9) {
			t.Errorf("%s: dup = %+v", c.args, dup)
		}
		bx, by := center(badge.Box)
		if badge.Kind != model.OpCreateShape || badge.Shape.ShapeType != "ELLIPSE" || badge.Shape.Text != "2" ||
			!near(badge.Box.W, 4, 1e-9) || !near(bx, dup.Box.X+dup.Box.W, 1e-9) || !near(by, dup.Box.Y, 1e-9) {
			t.Errorf("%s: badge = %+v", c.args, badge)
		}
	}
	for _, bad := range []string{"key=⑨ at=mm:1,1", "key=x at=mm:1,1", "key=②②  at=mm:1,1", "key=② at=mm:1,1 w=0mm", "key=②"} {
		if _, err := Build("sign", ParseArgs(bad), ctxFor(d)); err == nil {
			t.Errorf("sign %q accepted", bad)
		}
	}
}

func TestConeLine(t *testing.T) {
	d := akitsu(t)
	m40, _ := d.ResolveLength("40m")
	x0, y0 := 100.0, 100.0
	horiz := fmt.Sprintf("from=mm:%g,%g to=mm:%g,%g", x0, y0, x0+m40, y0)
	cases := []struct {
		args  string
		cones int // 0 = error
	}{
		{horiz, 11},               // 40m / 4m = 10 区間
		{horiz + " pitch=4m", 11}, //
		{horiz + " pitch=5m", 9},  // 8 区間
		{horiz + " pitch=3m", 14}, // 13 区間（3.08m）
		{horiz + " pitch=3", 14},  // 単位省略は m
		{horiz + " pitch=2.9m", 0},
		{horiz + " pitch=5.1m", 0},
		{horiz + " pitch=4mm", 0},
		{"from=mm:100,100 to=mm:100,100", 0},
		{"from=mm:100,100", 0},
	}
	for _, c := range cases {
		ops, err := Build("cone_line", ParseArgs(c.args), ctxFor(d))
		if c.cones == 0 {
			if err == nil {
				t.Errorf("%s: accepted (%d ops)", c.args, len(ops))
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", c.args, err)
		}
		if len(ops) != c.cones {
			t.Errorf("%s: cones = %d, want %d", c.args, len(ops), c.cones)
			continue
		}
		fx, _ := center(ops[0].Box)
		lx, _ := center(ops[len(ops)-1].Box)
		if !near(fx, x0, 1e-9) || !near(lx, x0+m40, 1e-9) {
			t.Errorf("%s: ends %v..%v", c.args, fx, lx)
		}
		for _, op := range ops {
			s := op.Shape
			if s.ShapeType != "ELLIPSE" || !near(op.Box.W, 2.2, 1e-9) || *s.Fill != ColorOrange || s.Outline != nil {
				t.Fatalf("cone = %+v %+v", op, s)
			}
		}
	}

	// 実デッキの site:1 → site:2: 実間隔が 3..5m。
	ops, err := Build("cone_line", ParseArgs("from=site:1 to=site:2"), ctxFor(d))
	if err != nil {
		t.Fatal(err)
	}
	ax, ay := center(ops[0].Box)
	bx, by := center(ops[1].Box)
	gap, _ := d.MetersBetween(ax, ay, bx, by)
	if gap < ConePitchMinM || gap > ConePitchMaxM {
		t.Fatalf("site1-2 cone gap = %vm", gap)
	}
	// 秋津の site:1→2 は約 320m（緯度差 0.00114°, 経度差 0.00312°）。
	ex, ey := center(ops[len(ops)-1].Box)
	total, _ := d.MetersBetween(ax, ay, ex, ey)
	if total < 300 || total > 345 {
		t.Fatalf("site1-2 length = %vm", total)
	}

	// georef の無いデッキでは実長が測れないので ng。
	if _, err := Build("cone_line", ParseArgs("from=mm:0,0 to=mm:100,0"), ctxFor(&model.Deck{})); err == nil {
		t.Error("cone_line without georef accepted")
	}
}

func TestConeSegments(t *testing.T) {
	cases := []struct {
		l, p float64
		want int
	}{
		{40, 4, 10}, {40, 5, 8}, {40, 3, 13}, {2, 4, 1}, {5.5, 5, 2}, {7, 5, 2}, {10, 4, 3}, {16, 5, 4}, {100, 3, 33},
	}
	for _, c := range cases {
		got := ConeSegments(c.l, c.p)
		if got != c.want {
			t.Errorf("ConeSegments(%v,%v) = %d, want %d", c.l, c.p, got, c.want)
		}
		if g := c.l / float64(got); g > ConePitchMaxM {
			t.Errorf("ConeSegments(%v,%v): gap %v out of range", c.l, c.p, g)
		}
	}
}

func TestGuard(t *testing.T) {
	d := akitsu(t)
	for _, k := range []string{"A", "B", "a"} {
		ops, err := Build("guard", ParseArgs("k="+k+" at=mm:50,60"), ctxFor(d))
		if err != nil {
			t.Fatal(err)
		}
		op := ops[0]
		cx, cy := center(op.Box)
		if len(ops) != 1 || op.Shape.ShapeType != "ELLIPSE" || *op.Shape.Fill != ColorBlue || op.Shape.Text != strings.ToUpper(k) ||
			!near(op.Box.W, 5, 1e-9) || !near(cx, 50, 1e-9) || !near(cy, 60, 1e-9) {
			t.Errorf("guard %s = %+v %+v", k, op, op.Shape)
		}
	}
	if _, err := Build("guard", ParseArgs("k=C at=mm:1,1"), ctxFor(d)); err == nil {
		t.Error("guard k=C accepted")
	}
}

func TestClosureDetour(t *testing.T) {
	d := akitsu(t)
	cases := []struct {
		kind  string
		path  string
		lines int
		style model.LineStyle
	}{
		{"closure", "mm:0,0;mm:10,0;mm:10,10", 2, model.LineStyle{Color: ColorRed, Weight: 4, Dash: "SOLID"}},
		{"closure", "site:1;site:2", 1, model.LineStyle{Color: ColorRed, Weight: 4, Dash: "SOLID"}},
		{"detour", "mm:0,0;mm:10,0;mm:10,10;mm:0,10;", 3, model.LineStyle{Color: ColorBlue, Weight: 3, Dash: "DASH"}},
		{"detour", "mm:0,0", 0, model.LineStyle{}},
		{"closure", "mm:0,0;site:9", 0, model.LineStyle{}},
	}
	for _, c := range cases {
		ops, err := Build(c.kind, map[string]string{"path": c.path}, ctxFor(d))
		if c.lines == 0 {
			if err == nil {
				t.Errorf("%s %s accepted", c.kind, c.path)
			}
			continue
		}
		if err != nil || len(ops) != c.lines {
			t.Fatalf("%s %s: %d ops, %v", c.kind, c.path, len(ops), err)
		}
		for i, op := range ops {
			if op.Kind != model.OpCreateLine || *op.Line != c.style {
				t.Errorf("%s: op = %+v", c.kind, op)
			}
			if i > 0 && (op.X1 != ops[i-1].X2 || op.Y1 != ops[i-1].Y2) {
				t.Errorf("%s: segments not chained", c.kind)
			}
		}
	}
	ops, _ := Build("closure", map[string]string{"path": "mm:0,0;mm:10,0;mm:10,10"}, ctxFor(d))
	if ops[1].X1 != 10 || ops[1].Y1 != 0 || ops[1].X2 != 10 || ops[1].Y2 != 10 {
		t.Fatalf("closure seg 2 = %+v", ops[1])
	}
}

func TestNote(t *testing.T) {
	d := akitsu(t)
	ok20 := strings.Repeat("あ", 20)
	cases := []struct {
		args string
		ok   bool
	}{
		{`at=mm:100,100 t="片側交互通行 注意"`, true},
		{"at=mm:100,100 t=" + ok20, true},
		{"at=mm:100,100 t=" + ok20 + "い", false},
		{"at=mm:100,100", false},
		{"t=abc", false},
	}
	for _, c := range cases {
		args := ParseArgs(c.args)
		ops, err := Build("note", args, ctxFor(d))
		if !c.ok {
			if err == nil {
				t.Errorf("note %q accepted", c.args)
			}
			continue
		}
		if err != nil {
			t.Fatalf("note %q: %v", c.args, err)
		}
		s := ops[0].Shape
		cx, cy := center(ops[0].Box)
		if s.ShapeType != "RECTANGLE" || s.Fill != nil || s.Outline != nil || s.FontPt != 10 || s.Text != args["t"] ||
			!near(cx, 100, 1e-9) || !near(cy, 100, 1e-9) {
			t.Errorf("note = %+v %+v", ops[0], s)
		}
	}
}

func TestParseArgs(t *testing.T) {
	cases := []struct {
		in   string
		want map[string]string
	}{
		{"", map[string]string{}},
		{"n=2 at=site:2", map[string]string{"n": "2", "at": "site:2"}},
		{`t="a b"  k=v`, map[string]string{"t": "a b", "k": "v"}},
		{`t="say \"hi\" \\ ok"`, map[string]string{"t": `say "hi" \ ok`}},
		{`t="unterminated x`, map[string]string{"t": "unterminated x"}},
		{"word k=v other", map[string]string{"k": "v"}},
		{"k= x=1", map[string]string{"k": "", "x": "1"}},
		{"=v k=1", map[string]string{"k": "1"}},
		{"k=1 k=2", map[string]string{"k": "2"}},
		{"at=32.7695,130.7783 path=site:1;site:2", map[string]string{"at": "32.7695,130.7783", "path": "site:1;site:2"}},
		{"t=工事中　注意", map[string]string{"t": "工事中　注意"}}, // 全角空白は区切りにしない
		{"key=② w=12mm", map[string]string{"key": "②", "w": "12mm"}},
		{"a=b=c", map[string]string{"a": "b=c"}},
	}
	for _, c := range cases {
		if got := ParseArgs(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParseArgs(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
