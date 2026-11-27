package project

import (
	"errors"
	"math"
	"os"
	"regexp"
	"testing"

	"github.com/YuujiKamura/AISLIDE/internal/manifest"
	"github.com/YuujiKamura/AISLIDE/internal/model"
	"github.com/YuujiKamura/AISLIDE/internal/slides"
)

// apply: ops をデッキに適用したつもりの状態（Slides の結果を模す）。ToRequests で変換できることも確かめる。
func apply(t *testing.T, d *model.Deck, ops []model.Op) {
	t.Helper()
	if _, err := slides.ToRequests(ops, d.PageID, d); err != nil {
		t.Fatalf("ToRequests: %v", err)
	}
	find := func(id string) int {
		for i := range d.Elements {
			if d.Elements[i].ObjectID == id {
				return i
			}
		}
		t.Fatalf("apply: %s not found", id)
		return -1
	}
	for _, op := range ops {
		switch op.Kind {
		case model.OpCreateShape:
			d.Elements = append(d.Elements, model.Element{ObjectID: op.ObjectID, Kind: "shape", Box: op.Box,
				Text: op.Shape.Text, Shape: op.Shape, Tag: op.Tag, SizeEMU: [2]float64{op.Box.W * 36000, op.Box.H * 36000}})
		case model.OpCreateLine:
			b := model.Box{X: math.Min(op.X1, op.X2), Y: math.Min(op.Y1, op.Y2), W: math.Abs(op.X2 - op.X1), H: math.Abs(op.Y2 - op.Y1)}
			d.Elements = append(d.Elements, model.Element{ObjectID: op.ObjectID, Kind: "line", Box: b, Line: op.Line, Tag: op.Tag,
				Flip: [2]bool{op.X2 < op.X1, op.Y2 < op.Y1}, SizeEMU: [2]float64{b.W * 36000, b.H * 36000}})
		case model.OpTransform:
			d.Elements[find(op.TargetID)].Box = op.Box
		case model.OpSetText:
			if op.TargetID == d.NotesID {
				d.NotesText = op.Text
				continue
			}
			d.Elements[find(op.TargetID)].Text = op.Text
		case model.OpDelete:
			i := find(op.TargetID)
			d.Elements = append(d.Elements[:i], d.Elements[i+1:]...)
		default:
			t.Fatalf("apply: unexpected op %s", op.Kind)
		}
	}
	for i := range d.Elements {
		d.Elements[i].Index = i
	}
}

// fixedProj: テスト用の Projector（与えた Spec をそのまま返す）。
type fixedProj struct {
	specs  []Spec
	prefix string
}

func (p fixedProj) Name() string { return "fixed" }
func (p fixedProj) Owns(u string) bool {
	return len(u) >= len(p.prefix) && u[:len(p.prefix)] == p.prefix
}
func (p fixedProj) Project(*manifest.Manifest, *model.Deck) ([]Spec, error) { return p.specs, nil }

func boxSpec(unit, role string, b model.Box, movable bool) Spec {
	id := ID(unit, role)
	return Spec{ID: id, Unit: unit, Role: role, Box: b, Movable: movable,
		Create: []model.Op{{Kind: model.OpCreateShape, ObjectID: id, Box: b,
			Shape: &model.ShapeStyle{ShapeType: "RECTANGLE"}, Tag: DerivedTag(unit, role)}}}
}

func TestID(t *testing.T) {
	re := regexp.MustCompile(`^[a-zA-Z0-9_:-]{5,50}$`)
	a := ID("legend.③", "strike1")
	if a != ID("legend.③", "strike1") || !re.MatchString(a) || len(a) != 15 {
		t.Fatalf("ID = %q", a)
	}
	if a == ID("legend.③", "strike2") || a == ID("legend.④", "strike1") {
		t.Fatal("IDs must differ per unit/role")
	}
}

func TestRunDiff(t *testing.T) {
	d := &model.Deck{PageID: "p1", Elements: []model.Element{
		{ObjectID: "base_label", Kind: "shape", Text: "③100m先", SizeEMU: [2]float64{36000, 36000}, Box: model.Box{W: 1, H: 1}},
		// 他の Projector（zones）の派生要素: 触らない。
		{ObjectID: "kd_other", Kind: "line", Tag: DerivedTag("Z1", "flag_line")},
	}}
	keep := boxSpec("t.a", "box", model.Box{X: 10, Y: 10, W: 5, H: 5}, false)
	mov := boxSpec("t.b", "sign", model.Box{X: 30, Y: 10, W: 5, H: 5}, true)
	stale := boxSpec("t.c", "box", model.Box{X: 50, Y: 10, W: 5, H: 5}, false)
	label := Spec{ID: "base_label", Unit: "t.a", Role: "label", Text: "②100m先", Base: true}
	p := fixedProj{prefix: "t.", specs: []Spec{keep, mov, stale, label}}

	ops, drifts, err := Run(nil, d, []Projector{p})
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 4 || len(drifts) != 0 {
		t.Fatalf("first run: ops=%+v drifts=%v", ops, drifts)
	}
	if ops[3].Kind != model.OpSetText || ops[3].TargetID != "base_label" || ops[3].Text != "②100m先" {
		t.Fatalf("label op = %+v", ops[3])
	}
	apply(t, d, ops)
	if ops, _, _ := Run(nil, d, []Projector{p}); len(ops) != 0 {
		t.Fatalf("second run must be empty: %+v", ops)
	}

	// 人が動かした: 固定物は戻す、Movable は Drift。不要になった t.c は消す。他人の kd_other は残す。
	for i := range d.Elements {
		switch d.Elements[i].ObjectID {
		case keep.ID, mov.ID:
			d.Elements[i].Box.X += 3
			d.Elements[i].Box.Y -= 2
		}
	}
	p.specs = []Spec{keep, mov, label}
	ops, drifts, err = Run(nil, d, []Projector{p})
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 2 || ops[0].Kind != model.OpDelete || ops[0].TargetID != stale.ID ||
		ops[1].Kind != model.OpTransform || ops[1].TargetID != keep.ID || ops[1].Box != keep.Box {
		t.Fatalf("ops = %+v", ops)
	}
	if len(drifts) != 1 || drifts[0].Unit != "t.b" || math.Abs(drifts[0].DX-3) > 1e-9 || math.Abs(drifts[0].DY+2) > 1e-9 {
		t.Fatalf("drifts = %+v", drifts)
	}

	// base 要素が無い Spec は error。重複 ID も error。
	p.specs = []Spec{{ID: "nope", Unit: "t.x", Role: "label", Text: "x", Base: true}}
	if _, _, err := Run(nil, d, []Projector{p}); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("missing base: %v", err)
	}
	p.specs = []Spec{keep, keep}
	if _, _, err := Run(nil, d, []Projector{p}); err == nil {
		t.Fatal("duplicate spec id must fail")
	}
}

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

func label(t *testing.T, d *model.Deck, id string) string {
	t.Helper()
	e, err := d.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return e.Text
}

// project: 投影して適用し、もう一度投影すると 0 件であること（冪等）。
func project(t *testing.T, m *manifest.Manifest, d *model.Deck) []model.Op {
	t.Helper()
	ops, _, err := Run(m, d, Default())
	if err != nil {
		t.Fatal(err)
	}
	apply(t, d, ops)
	again, _, err := Run(m, d, Default())
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Fatalf("not idempotent: %+v", again)
	}
	return ops
}

func derived(d *model.Deck) map[string]*model.Element {
	out := map[string]*model.Element{}
	for i := range d.Elements {
		if UnitOf(&d.Elements[i]) != "" {
			out[d.Elements[i].ObjectID] = &d.Elements[i]
		}
	}
	return out
}

func TestLegendLifecycleAkitsu(t *testing.T) {
	d := akitsu(t)
	lg, err := AdoptLegend(d, nil)
	if err != nil {
		t.Fatal(err)
	}
	keys := ""
	for _, it := range lg.Items {
		keys += it.Key
		if !it.On {
			t.Errorf("%s adopted off", it.Key)
		}
	}
	if keys != "①②③④⑤⑥⑦⑧" || lg.Mode != ModeDraft {
		t.Fatalf("adopt keys=%s mode=%s", keys, lg.Mode)
	}
	it3 := lg.Items[2]
	if it3.ImageID != "p1_i19" || it3.LabelID != "p1_i20" || it3.Label != "100m先工事中" {
		t.Fatalf("③ = %+v", it3)
	}
	m := &manifest.Manifest{Version: 1, Legend: lg}
	nBase := len(d.Elements)
	if ops := project(t, m, d); len(ops) != 0 {
		t.Fatalf("all on, draft: nothing to draw, got %+v", ops)
	}

	// off ③（draft）: ③の画像の箱に赤の平行斜線 2 本。ラベルはそのまま。
	if err := SetLegendOn(m, "③", false); err != nil {
		t.Fatal(err)
	}
	project(t, m, d)
	img3, _ := d.Get("p1_i19")
	dv := derived(d)
	if len(dv) != 2 {
		t.Fatalf("want 2 strike lines, got %d", len(dv))
	}
	for _, role := range []string{RoleStrike1, RoleStrike2} {
		e := dv[ID("legend.③", role)]
		if e == nil || e.Kind != "line" || e.Line.Color != StrikeColor || e.Line.Weight != StrikePt {
			t.Fatalf("%s = %+v", role, e)
		}
		ib := img3.Box
		if e.Box.X > ib.X || e.Box.X+e.Box.W < ib.X+ib.W || e.Box.Y > ib.Y+ib.H*0.2 || e.Box.Y+e.Box.H < ib.Y+ib.H*0.8 {
			t.Errorf("%s box %+v does not cross image %+v", role, e.Box, ib)
		}
		x1, y1, x2, y2 := e.LineEnds()
		if !(x2 > x1 && y2 < y1) {
			t.Errorf("%s must run bottom-left → top-right: (%.1f,%.1f)-(%.1f,%.1f)", role, x1, y1, x2, y2)
		}
	}
	if label(t, d, "p1_i20") != "③100m先工事中" || label(t, d, "p1_i22") != "④50m先工事中" {
		t.Fatal("draft must not renumber")
	}

	// final: 斜線は消え、③は白マスク、④以降は 1 つずつ詰まる。
	if err := SetLegendMode(m, ModeFinal); err != nil {
		t.Fatal(err)
	}
	project(t, m, d)
	dv = derived(d)
	mask := dv[ID("legend.③", RoleMask)]
	if len(dv) != 1 || mask == nil || mask.Shape.Fill == nil || *mask.Shape.Fill != MaskColor {
		t.Fatalf("want only ③ mask, got %v", dv)
	}
	lbl3, _ := d.Get("p1_i20")
	if mb, ib := mask.Box, img3.Box; mb.X > ib.X || mb.Y > ib.Y || mb.X+mb.W < lbl3.Box.X+lbl3.Box.W || mb.Y+mb.H < lbl3.Box.Y+lbl3.Box.H {
		t.Fatalf("mask %+v must cover image %+v and label %+v", mb, ib, lbl3.Box)
	}
	want := map[string]string{"p1_i16": "①車両通行止", "p1_i18": "②通り抜け不可", "p1_i22": "③50m先工事中",
		"p1_i24": "④徐行して下さい", "p1_i26": "⑤御協力お願い", "p1_i28": "⑥工事予告", "p1_i30": "⑦工事情報板"}
	for id, w := range want {
		if g := label(t, d, id); g != w {
			t.Errorf("final %s = %q want %q", id, g, w)
		}
	}

	// draft に戻す: マスクが消え、番号は元に戻り、斜線が復活。
	if err := SetLegendMode(m, ModeDraft); err != nil {
		t.Fatal(err)
	}
	project(t, m, d)
	if dv = derived(d); len(dv) != 2 || dv[ID("legend.③", RoleMask)] != nil {
		t.Fatalf("draft again: %v", dv)
	}
	if label(t, d, "p1_i22") != "④50m先工事中" || label(t, d, "p1_i30") != "⑧工事情報板" {
		t.Fatal("draft must restore original labels")
	}

	// on ③ に戻すと派生物はすべて消え、元のデッキと同じ要素数。
	if err := SetLegendOn(m, "3", true); err != nil {
		t.Fatal(err)
	}
	project(t, m, d)
	if len(d.Elements) != nBase || len(derived(d)) != 0 {
		t.Fatalf("all on: %d elements (base %d)", len(d.Elements), nBase)
	}
}

func TestLegendAdoptGuards(t *testing.T) {
	d := akitsu(t)
	prev := &manifest.Legend{Mode: ModeDraft, Items: []manifest.LegendItem{{Key: "⑤", On: false}}}
	lg, err := AdoptLegend(d, prev)
	if err != nil {
		t.Fatal(err)
	}
	if lg.Items[4].Key != "⑤" || lg.Items[4].On || !lg.Items[3].On {
		t.Fatalf("on/off must carry over by key: %+v", lg.Items)
	}
	if _, err := AdoptLegend(d, &manifest.Legend{Mode: ModeFinal}); !errors.Is(err, ErrFinalMode) {
		t.Fatalf("final: %v", err)
	}
	if _, err := AdoptLegend(&model.Deck{}, nil); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("empty deck: %v", err)
	}
	m := &manifest.Manifest{Version: 1, Legend: lg}
	if err := SetLegendOn(m, "⑨", false); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("unknown key: %v", err)
	}
	if err := SetLegendOn(m, "x", false); err == nil {
		t.Fatal("bad key must fail")
	}
	if err := SetLegendMode(m, "clean"); err == nil {
		t.Fatal("bad mode must fail")
	}
}
