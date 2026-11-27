package journal

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
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

func TestInverse(t *testing.T) {
	d := akitsu(t)
	mark, _ := d.Get("kl_mark_7af978ea")
	label, _ := d.Get("p1_i18")
	hline, _ := d.Get("h1b5d0a27ea77b246_1_0")
	tag := &model.Tag{Layer: "signs", Owner: "a1", Kind: "site"}
	newBox := model.Box{X: 1, Y: 2, W: 3, H: 4}

	cases := []struct {
		name string
		ops  []model.Op
		want []model.Op
		err  error
	}{
		{
			name: "create -> delete (reverse order)",
			ops: []model.Op{
				{Kind: model.OpCreateShape, ObjectID: "kc_signs_1"},
				{Kind: model.OpCreateLine, ObjectID: "kc_signs_2"},
				{Kind: model.OpDuplicate, ObjectID: "kc_signs_3", SourceID: "p1_i17"},
			},
			want: []model.Op{
				{Kind: model.OpDelete, TargetID: "kc_signs_3"},
				{Kind: model.OpDelete, TargetID: "kc_signs_2"},
				{Kind: model.OpDelete, TargetID: "kc_signs_1"},
			},
		},
		{
			name: "transform restores original box",
			ops:  []model.Op{{Kind: model.OpTransform, TargetID: mark.ObjectID, Box: newBox}},
			want: []model.Op{{Kind: model.OpTransform, TargetID: mark.ObjectID, Box: mark.Box}},
		},
		{
			name: "two transforms: each restores the state before it",
			ops: []model.Op{
				{Kind: model.OpTransform, TargetID: mark.ObjectID, Box: newBox},
				{Kind: model.OpTransform, TargetID: mark.ObjectID, Box: model.Box{X: 9}},
			},
			want: []model.Op{
				{Kind: model.OpTransform, TargetID: mark.ObjectID, Box: newBox},
				{Kind: model.OpTransform, TargetID: mark.ObjectID, Box: mark.Box},
			},
		},
		{
			name: "set_text restores text",
			ops:  []model.Op{{Kind: model.OpSetText, TargetID: label.ObjectID, Text: "x"}},
			want: []model.Op{{Kind: model.OpSetText, TargetID: label.ObjectID, Text: "②通り抜け不可"}},
		},
		{
			name: "set_tag on base (adopt) restores nil tag",
			ops:  []model.Op{{Kind: model.OpSetTag, TargetID: mark.ObjectID, Tag: tag}},
			want: []model.Op{{Kind: model.OpSetTag, TargetID: mark.ObjectID, Tag: nil}},
		},
		{
			name: "set_tag on map_bg restores legacy georef tag",
			ops:  []model.Op{{Kind: model.OpSetTag, TargetID: "p1_i4", Tag: tag}},
			want: []model.Op{{Kind: model.OpSetTag, TargetID: "p1_i4", Tag: d.Elements[0].Tag}},
		},
		{
			name: "ops on objects created in the same batch need only the delete",
			ops: []model.Op{
				{Kind: model.OpCreateShape, ObjectID: "kc_signs_1"},
				{Kind: model.OpSetTag, TargetID: "kc_signs_1", Tag: tag},
				{Kind: model.OpTransform, TargetID: "kc_signs_1", Box: newBox},
				{Kind: model.OpZOrderBack, TargetIDs: []string{"kc_signs_1"}},
			},
			want: []model.Op{{Kind: model.OpDelete, TargetID: "kc_signs_1"}},
		},
		{
			name: "create then delete in one batch",
			ops: []model.Op{
				{Kind: model.OpCreateShape, ObjectID: "kc_signs_1"},
				{Kind: model.OpDelete, TargetID: "kc_signs_1"},
			},
			want: []model.Op{{Kind: model.OpDelete, TargetID: "kc_signs_1"}},
		},
		{
			name: "delete line is recreated with same id, ends and style",
			ops:  []model.Op{{Kind: model.OpDelete, TargetID: hline.ObjectID}},
			want: func() []model.Op {
				x1, y1, x2, y2 := hline.LineEnds()
				ls := *hline.Line
				return []model.Op{{Kind: model.OpCreateLine, ObjectID: hline.ObjectID, X1: x1, Y1: y1, X2: x2, Y2: y2, Line: &ls}}
			}(),
		},
		{
			name: "delete image is irreversible",
			ops:  []model.Op{{Kind: model.OpDelete, TargetID: "p1_i17"}},
			err:  ErrIrreversible,
		},
		{
			name: "unknown target",
			ops:  []model.Op{{Kind: model.OpTransform, TargetID: "nope"}},
			err:  model.ErrNotFound,
		},
		{
			name: "op after delete in same batch",
			ops: []model.Op{
				{Kind: model.OpDelete, TargetID: mark.ObjectID},
				{Kind: model.OpSetText, TargetID: mark.ObjectID, Text: "x"},
			},
			err: model.ErrNotFound,
		},
		{
			name: "create without id",
			ops:  []model.Op{{Kind: model.OpCreateShape}},
			err:  ErrIrreversible,
		},
	}
	for _, c := range cases {
		got, err := Inverse(c.ops, d)
		if c.err != nil {
			if !errors.Is(err, c.err) {
				t.Errorf("%s: err = %v, want %v", c.name, err, c.err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s:\n got  %+v\n want %+v", c.name, got, c.want)
		}
	}
}

func TestInverseDeleteShape(t *testing.T) {
	d := akitsu(t)
	mark, _ := d.Get("kl_mark_7af978ea")
	inv, err := Inverse([]model.Op{{Kind: model.OpDelete, TargetID: mark.ObjectID}}, d)
	if err != nil {
		t.Fatal(err)
	}
	op := inv[0]
	if op.Kind != model.OpCreateShape || op.ObjectID != mark.ObjectID || op.Box != mark.Box || op.Shape == nil ||
		op.Shape.ShapeType != "ELLIPSE" || op.Shape.Text != "2" || op.Shape.Fill == nil || op.Tag != nil {
		t.Fatalf("recreate = %+v %+v", op, op.Shape)
	}
}

func TestInverseDoesNotAliasDeck(t *testing.T) {
	d := akitsu(t)
	inv, err := Inverse([]model.Op{{Kind: model.OpSetTag, TargetID: "p1_i4", Tag: &model.Tag{Layer: "map", Kind: "map_bg"}}}, d)
	if err != nil {
		t.Fatal(err)
	}
	inv[0].Tag.Georef.Sites["9"] = [2]float64{1, 2}
	if _, ok := d.Elements[0].Tag.Georef.Sites["9"]; ok {
		t.Fatal("inverse tag aliases the deck's tag")
	}
}

func TestJournalPushPop(t *testing.T) {
	j := &Journal{Dir: filepath.Join(t.TempDir(), "j")}
	const pres = "1_-5-vyTFoAcimmRW3LsZLVpENdBBPXHI5N0IkeQJ-RE"
	op := func(id string) []model.Op {
		return []model.Op{{Kind: model.OpDelete, TargetID: id, Tag: &model.Tag{Layer: "x", Kind: "site", Params: map[string]any{"n": "2"}}}}
	}
	if _, err := j.Pop(pres, "a1"); !errors.Is(err, ErrEmpty) {
		t.Fatalf("empty pop = %v", err)
	}
	steps := []struct{ agent, id string }{{"a1", "k1"}, {"a2", "k2"}, {"a1", "k3"}}
	for _, s := range steps {
		if err := j.Push(pres, s.agent, op(s.id)); err != nil {
			t.Fatal(err)
		}
	}
	if err := j.Push(pres, "a1", nil); err != nil {
		t.Fatal(err)
	}
	pops := []struct {
		agent string
		want  string // "" = ErrEmpty
	}{
		{"a1", "k3"},
		{"a2", "k2"},
		{"a2", ""},
		{"a3", ""},
		{"", "k1"}, // agent 無指定は全体で直近
		{"a1", ""},
		{"", ""},
	}
	for _, p := range pops {
		// Journal を作り直しても状態はファイルから復元される。
		j2 := &Journal{Dir: j.Dir}
		got, err := j2.Pop(pres, p.agent)
		if p.want == "" {
			if !errors.Is(err, ErrEmpty) {
				t.Errorf("Pop(%q) = %v, %v; want ErrEmpty", p.agent, got, err)
			}
			continue
		}
		if err != nil || len(got) != 1 || got[0].TargetID != p.want || got[0].Tag.Params["n"] != "2" {
			t.Errorf("Pop(%q) = %+v, %v; want %s", p.agent, got, err, p.want)
		}
	}
	if _, err := os.Stat(filepath.Join(j.Dir, pres+".jsonl")); err != nil {
		t.Fatal(err)
	}
}

func TestJournalBadPresID(t *testing.T) {
	j := &Journal{Dir: t.TempDir()}
	for _, id := range []string{"", "../x", `a\b`, "a/b"} {
		if err := j.Push(id, "a", []model.Op{{Kind: model.OpDelete, TargetID: "x"}}); err == nil {
			t.Errorf("Push(%q) accepted", id)
		}
		if _, err := j.Pop(id, "a"); err == nil || errors.Is(err, ErrEmpty) {
			t.Errorf("Pop(%q) = %v", id, err)
		}
	}
}
