package slides

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/YuujiKamura/AISLIDE/internal/model"
	sl "google.golang.org/api/slides/v1"
)

var update = flag.Bool("update", false, "rewrite golden files")

// testDeck: model.Deck の実装に依存しないよう struct literal で組む。
func testDeck() *model.Deck {
	return &model.Deck{
		PresID: "pres", Rev: "rev1", PageID: "p1", PageW: 420, PageH: 297,
		Elements: []model.Element{
			{Index: 0, ObjectID: "legend_img", Kind: "image", Box: model.Box{X: 10, Y: 10, W: 20, H: 10}, SizeEMU: [2]float64{720000, 360000}},
			{Index: 1, ObjectID: "kc_note_00000001", Kind: "shape", Box: model.Box{X: 50, Y: 50, W: 30, H: 8}, Text: "旧", SizeEMU: [2]float64{1080000, 288000},
				Tag: &model.Tag{Layer: "note", Owner: "a1", Kind: "note"}},
			{Index: 2, ObjectID: "kc_note_00000002", Kind: "shape", Box: model.Box{X: 50, Y: 70, W: 30, H: 8}, SizeEMU: [2]float64{1080000, 288000}},
			{Index: 3, ObjectID: "human_line", Kind: "line", Box: model.Box{X: 68.57, Y: 47.6, W: 311.08, H: 135.48}, Flip: [2]bool{false, true}, SizeEMU: [2]float64{3000000, 3000000}},
		},
	}
}

func goldenOps() []model.Op {
	red := model.RGB{R: 1}
	white := model.RGB{R: 1, G: 1, B: 1}
	return []model.Op{
		{Kind: model.OpCreateShape, ObjectID: "kc_sites_0000000a", Box: model.Box{X: 100, Y: 80, W: 8, H: 8},
			Shape: &model.ShapeStyle{ShapeType: "ELLIPSE", Fill: &red, Text: "2", FontPt: 10, FontColor: white, Bold: true, AlignCenter: true},
			Tag:   &model.Tag{Layer: "sites", Owner: "a1", Kind: "site", Params: map[string]any{"n": 2, "at": "32.7,130.7"}}},
		{Kind: model.OpCreateShape, ObjectID: "kc_note_0000000b", Box: model.Box{X: 10, Y: 10, W: 40, H: 6},
			Shape: &model.ShapeStyle{Outline: &red, OutlinePt: 1.5}},
		{Kind: model.OpCreateLine, ObjectID: "kc_closure_0000000c", X1: 200, Y1: 100, X2: 150, Y2: 130,
			Line: &model.LineStyle{Color: red, Weight: 4, Dash: "SOLID"}, Tag: &model.Tag{Layer: "closure", Kind: "closure"}},
		{Kind: model.OpDuplicate, ObjectID: "kc_signs_0000000d", SourceID: "legend_img", Box: model.Box{X: 300, Y: 200, W: 12, H: 6},
			Tag: &model.Tag{Layer: "signs", Owner: "a1", Kind: "sign", Params: map[string]any{"key": "①"}}},
		{Kind: model.OpTransform, TargetID: "kc_note_00000001", Box: model.Box{X: 60, Y: 55, W: 30, H: 8}},
		{Kind: model.OpSetText, TargetID: "kc_note_00000001", Text: "新しい注記"},
		{Kind: model.OpSetText, TargetID: "kc_note_00000002", Text: "空から"},
		{Kind: model.OpSetTag, TargetID: "human_line", Tag: &model.Tag{Layer: "detour", Owner: "a1", Kind: "detour"}},
		{Kind: model.OpZOrderBack, TargetIDs: []string{"kc_map_mask_1", "p1_i4"}},
		{Kind: model.OpDelete, TargetID: "kc_note_00000002"},
	}
}

func TestToRequestsGolden(t *testing.T) {
	reqs, err := ToRequests(goldenOps(), "p1", testDeck())
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(reqs, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := filepath.Join("testdata", "golden_requests.json")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update)", err)
	}
	if !bytes.Equal(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n")), got) {
		t.Errorf("requests differ from %s (run with -update and review the diff)\n%s", path, got)
	}
}

// apply: affine を局所点に適用（shear は使わない）。
func apply(tr *sl.AffineTransform, x, y float64) (float64, float64) {
	return tr.ScaleX*x + tr.ShearX*y + tr.TranslateX, tr.ShearY*x + tr.ScaleY*y + tr.TranslateY
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

// 局所直線 (0,0)→(W,H) が transform 後に (X1,Y1)→(X2,Y2) になり、外接矩形が min/|d| になること。
func TestLineGeometryAffine(t *testing.T) {
	cases := [][4]float64{
		{10, 20, 110, 70}, // 右下
		{110, 20, 10, 70}, // 左下（scaleX=-1）
		{10, 70, 110, 20}, // 右上（scaleY=-1）
		{110, 70, 10, 20}, // 左上（両方）
		{50, 10, 50, 90},  // 垂直（W=0.01mm）
		{5.5, 3.25, 1, 1}, // 端数
		{200, 100, 150, 130},
	}
	for _, c := range cases {
		x1, y1, x2, y2 := c[0], c[1], c[2], c[3]
		size, tr := lineGeometry(x1, y1, x2, y2)
		W, H := size.Width.Magnitude, size.Height.Magnitude
		if W <= 0 || H <= 0 {
			t.Fatalf("%v: non-positive size %v,%v", c, W, H)
		}
		sx, sy := apply(tr, 0, 0)
		ex, ey := apply(tr, W, H)
		if !near(sx, emu(x1)) || !near(sy, emu(y1)) {
			t.Errorf("%v: start -> (%v,%v) want (%v,%v)", c, sx/EMUPerMM, sy/EMUPerMM, x1, y1)
		}
		wantEX, wantEY := x2, y2
		if x1 == x2 {
			wantEX = x1 + minMM
		}
		if y1 == y2 {
			wantEY = y1 + minMM
		}
		if !near(ex, emu(wantEX)) || !near(ey, emu(wantEY)) {
			t.Errorf("%v: end -> (%v,%v) want (%v,%v)", c, ex/EMUPerMM, ey/EMUPerMM, wantEX, wantEY)
		}
		minX, minY := math.Min(sx, ex)/EMUPerMM, math.Min(sy, ey)/EMUPerMM
		if !near(minX, math.Min(x1, wantEX)) || !near(minY, math.Min(y1, wantEY)) {
			t.Errorf("%v: bbox origin (%v,%v)", c, minX, minY)
		}
	}
}

// Transform / Duplicate の ABSOLUTE transform が Box を正しく覆うこと（flip 含む）。
func TestAbsTransformCoversBox(t *testing.T) {
	b := model.Box{X: 30, Y: 40, W: 120, H: 60}
	for _, flip := range [][2]bool{{false, false}, {true, false}, {false, true}, {true, true}} {
		k := &known{sizeEMU: [2]float64{3000000, 3000000}, flip: flip}
		r, err := absTransform("x", k, b)
		if err != nil {
			t.Fatal(err)
		}
		tr := r.UpdatePageElementTransform.Transform
		if r.UpdatePageElementTransform.ApplyMode != "ABSOLUTE" {
			t.Fatal("mode")
		}
		ax, ay := apply(tr, 0, 0)
		bx, by := apply(tr, k.sizeEMU[0], k.sizeEMU[1])
		got := model.Box{
			X: math.Min(ax, bx) / EMUPerMM, Y: math.Min(ay, by) / EMUPerMM,
			W: math.Abs(bx-ax) / EMUPerMM, H: math.Abs(by-ay) / EMUPerMM,
		}
		if !near(got.X, b.X) || !near(got.Y, b.Y) || !near(got.W, b.W) || !near(got.H, b.H) {
			t.Errorf("flip=%v: box %+v want %+v", flip, got, b)
		}
		if (tr.ScaleX < 0) != flip[0] || (tr.ScaleY < 0) != flip[1] {
			t.Errorf("flip=%v: scale signs %v %v", flip, tr.ScaleX, tr.ScaleY)
		}
	}
}

func TestDuplicateScaleFromSource(t *testing.T) {
	reqs, err := ToRequests([]model.Op{{Kind: model.OpDuplicate, ObjectID: "kc_s_1", SourceID: "legend_img", Box: model.Box{X: 1, Y: 2, W: 12, H: 6}}}, "p1", testDeck())
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 2 {
		t.Fatalf("want 2 requests, got %d", len(reqs))
	}
	d := reqs[0].DuplicateObject
	if d == nil || d.ObjectId != "legend_img" || d.ObjectIds["legend_img"] != "kc_s_1" {
		t.Fatalf("duplicate %+v", d)
	}
	tr := reqs[1].UpdatePageElementTransform
	if tr.ObjectId != "kc_s_1" || !near(tr.Transform.ScaleX, 12*EMUPerMM/720000) || !near(tr.Transform.ScaleY, 6*EMUPerMM/360000) {
		t.Fatalf("transform %+v", tr.Transform)
	}
}

func TestSetTextSkipsDeleteWhenEmpty(t *testing.T) {
	ops := []model.Op{
		{Kind: model.OpSetText, TargetID: "kc_note_00000002", Text: "a"}, // 空 → Insert のみ
		{Kind: model.OpSetText, TargetID: "kc_note_00000002", Text: "b"}, // 直前で文字が入った → Delete+Insert
		{Kind: model.OpSetText, TargetID: "kc_note_00000001", Text: ""},  // 既存文字 → Delete のみ
	}
	reqs, err := ToRequests(ops, "p1", testDeck())
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, r := range reqs {
		switch {
		case r.DeleteText != nil:
			kinds = append(kinds, "del")
		case r.InsertText != nil:
			kinds = append(kinds, "ins:"+r.InsertText.Text)
		}
	}
	want := []string{"ins:a", "del", "ins:b", "del"}
	if len(kinds) != len(want) {
		t.Fatalf("got %v want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("got %v want %v", kinds, want)
		}
	}
}

func TestSameBatchReferences(t *testing.T) {
	// 同じバッチで作った図形を移動・文字変更できる。
	ops := []model.Op{
		{Kind: model.OpCreateShape, ObjectID: "kc_a_1", Box: model.Box{X: 0, Y: 0, W: 10, H: 5}, Shape: &model.ShapeStyle{}},
		{Kind: model.OpTransform, TargetID: "kc_a_1", Box: model.Box{X: 5, Y: 5, W: 20, H: 5}},
		{Kind: model.OpSetText, TargetID: "kc_a_1", Text: "x"},
	}
	reqs, err := ToRequests(ops, "p1", nil)
	if err != nil {
		t.Fatal(err)
	}
	last := reqs[len(reqs)-1]
	if last.InsertText == nil || reqs[len(reqs)-2].DeleteText != nil {
		t.Fatalf("unexpected tail %+v", reqs[len(reqs)-2:])
	}
	tr := reqs[2].UpdatePageElementTransform.Transform
	if !near(tr.ScaleX, 2) || !near(tr.ScaleY, 1) {
		t.Fatalf("scale %v %v", tr.ScaleX, tr.ScaleY)
	}
}

func TestShapeNilStylesNotRendered(t *testing.T) {
	reqs, err := ToRequests([]model.Op{{Kind: model.OpCreateShape, ObjectID: "kc_a_1", Box: model.Box{W: 1, H: 1}, Shape: &model.ShapeStyle{}}}, "p1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 2 {
		t.Fatalf("no text → CreateShape + UpdateShapeProperties only, got %d", len(reqs))
	}
	p := reqs[1].UpdateShapeProperties.ShapeProperties
	if p.ShapeBackgroundFill.PropertyState != "NOT_RENDERED" || p.Outline.PropertyState != "NOT_RENDERED" {
		t.Fatalf("%+v", p)
	}
	if reqs[0].CreateShape.ShapeType != "RECTANGLE" {
		t.Fatal("default shape type")
	}
}

func TestTagDescriptionNoHTMLEscape(t *testing.T) {
	s, err := TagDescription(&model.Tag{Layer: "note", Kind: "note", Params: map[string]any{"t": "<通行止>&"}})
	if err != nil {
		t.Fatal(err)
	}
	want := `kisei:{"layer":"note","kind":"note","params":{"t":"<通行止>&"}}`
	if s != want {
		t.Fatalf("got %s", s)
	}
}

func TestToRequestsErrors(t *testing.T) {
	bad := []model.Op{
		{Kind: model.OpTransform, TargetID: "nope"},
		{Kind: model.OpDuplicate, ObjectID: "x", SourceID: "nope"},
		{Kind: model.OpSetText, TargetID: "nope", Text: "a"},
		{Kind: model.OpCreateShape, ObjectID: "x"},
		{Kind: model.OpCreateLine, ObjectID: "x"},
		{Kind: model.OpSetTag, TargetID: "x"},
		{Kind: model.OpZOrderBack},
		{Kind: "bogus"},
	}
	for _, op := range bad {
		if _, err := ToRequests([]model.Op{op}, "p1", testDeck()); err == nil {
			t.Errorf("%s: want error", op.Kind)
		}
	}
	_, err := ToRequests([]model.Op{{Kind: model.OpTransform, TargetID: "nope"}}, "p1", testDeck())
	if !errors.Is(err, model.ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}
