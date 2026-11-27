package cp

import (
	"context"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/YuujiKamura/AISLIDE/internal/manifest"
	"github.com/YuujiKamura/AISLIDE/internal/model"
	"github.com/YuujiKamura/AISLIDE/internal/project"
	"github.com/YuujiKamura/AISLIDE/internal/slides"
	"github.com/YuujiKamura/AISLIDE/internal/zone"
)

// deckBackend: 実 fixture（akitsu）をパースしたデッキを保持し、ops を Slides と同じ意味で適用する
// （ノートへの set_text、看板画像の複製を含む。適用前に slides.ToRequests が通ることも確かめる）。
type deckBackend struct {
	t       *testing.T
	deck    *model.Deck
	applied [][]model.Op
}

func (b *deckBackend) Load(ctx context.Context, presID string) ([]byte, error) {
	return []byte("{}"), nil
}
func (b *deckBackend) Thumbnail(ctx context.Context, presID, pageID, path string) error {
	return nil
}

// minLineEMU: 実 Slides は直線の size を下限 0.01mm で作る（slides.lineGeometry）。
const minLineEMU = 0.01 * model.EMUPerMM

func (b *deckBackend) Apply(ctx context.Context, presID, pageID string, ops []model.Op, requiredRev string, d *model.Deck) (string, error) {
	if _, err := slides.ToRequests(ops, pageID, d); err != nil {
		return "", err
	}
	b.applied = append(b.applied, ops)
	for _, op := range ops {
		i := -1
		for k := range b.deck.Elements {
			if b.deck.Elements[k].ObjectID == op.TargetID {
				i = k
			}
		}
		switch op.Kind {
		case model.OpCreateShape:
			sh := *op.Shape
			b.deck.Elements = append(b.deck.Elements, model.Element{ObjectID: op.ObjectID, Kind: "shape", Box: op.Box, Tag: op.Tag,
				Text: sh.Text, Shape: &sh, SizeEMU: [2]float64{op.Box.W * model.EMUPerMM, op.Box.H * model.EMUPerMM}})
		case model.OpDuplicate:
			b.deck.Elements = append(b.deck.Elements, model.Element{ObjectID: op.ObjectID, Kind: "image", Box: op.Box, Tag: op.Tag,
				SizeEMU: [2]float64{op.Box.W * model.EMUPerMM, op.Box.H * model.EMUPerMM}})
		case model.OpCreateLine:
			bx := model.Box{X: math.Min(op.X1, op.X2), Y: math.Min(op.Y1, op.Y2), W: math.Abs(op.X2 - op.X1), H: math.Abs(op.Y2 - op.Y1)}
			ls := *op.Line
			b.deck.Elements = append(b.deck.Elements, model.Element{ObjectID: op.ObjectID, Kind: "line", Tag: op.Tag, Box: bx, Line: &ls,
				Flip:    [2]bool{op.X2 < op.X1, op.Y2 < op.Y1},
				SizeEMU: [2]float64{math.Max(bx.W*model.EMUPerMM, minLineEMU), math.Max(bx.H*model.EMUPerMM, minLineEMU)}})
		case model.OpTransform:
			b.deck.Elements[i].Box = op.Box
		case model.OpSetText:
			if op.TargetID == b.deck.NotesID {
				b.deck.NotesText = op.Text
			} else {
				b.deck.Elements[i].Text = op.Text
			}
		case model.OpDelete:
			b.deck.Elements = append(b.deck.Elements[:i], b.deck.Elements[i+1:]...)
		default:
			b.t.Fatalf("deckBackend: unexpected op %s", op.Kind)
		}
	}
	for k := range b.deck.Elements {
		b.deck.Elements[k].Index = k
	}
	return "r", nil
}

// mf: デッキのノートに保存された manifest。
func mf(t *testing.T, b *deckBackend) *manifest.Manifest {
	t.Helper()
	m, _, err := manifest.FromDeck(b.deck)
	if err != nil || m == nil {
		t.Fatalf("manifest: %v", err)
	}
	return m
}

func newZoneServer(t *testing.T) (*Server, *deckBackend, *memJournal) {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/akitsu_presentation.json")
	if err != nil {
		t.Fatal(err)
	}
	d, err := model.ParseDeck(raw, 1)
	if err != nil {
		t.Fatal(err)
	}
	if d.NotesID == "" {
		t.Fatal("fixture has no speaker notes")
	}
	d.NotesText = "現場メモ"
	b := &deckBackend{t: t, deck: d}
	j := &memJournal{st: map[string][][]model.Op{}}
	s := New(b, j)
	s.d.parseDeck = func([]byte, int) (*model.Deck, error) {
		c := *b.deck
		c.Elements = append([]model.Element(nil), b.deck.Elements...)
		return &c, nil
	}
	mustPrefix(t, s.Dispatch(bg, "open PRES"), "ok")
	return s, b, j
}

func elem(t *testing.T, b *deckBackend, id string) *model.Element {
	t.Helper()
	e, err := b.deck.Get(id)
	if err != nil {
		t.Fatalf("%s: %v", id, err)
	}
	return e
}

func TestZoneCommandsAkitsu(t *testing.T) {
	s, b, j := newZoneServer(t)
	f, _ := zone.FrameOf(b.deck)
	base := *elem(t, b, "h1b5d0a27ea77b246_1_0")

	r := s.Dispatch(bg, "zone adopt 36 as Z1")
	mustPrefix(t, r, "ok zone adopt Z1 from=h1b5d0a27ea77b246_1_0 kind=closure len=")
	if !strings.Contains(r, "ends=EW") || strings.Contains(r, "stale") {
		t.Fatalf("adopt: %q", r)
	}
	z := mf(t, b).Zones["Z1"]
	L0 := zone.Length(f, z.Path)
	if want, _ := b.deck.MetersBetween(base.LineEnds()); math.Abs(L0-want) > 0.01 {
		t.Fatalf("zone %.2fm vs line %.2fm", L0, want)
	}
	flag := elem(t, b, project.ID("Z1", "flag_text"))
	if flag.Text != project.FlagText(L0) || !strings.HasPrefix(flag.Text, "L=") {
		t.Fatalf("flag %q", flag.Text)
	}
	if after := elem(t, b, "h1b5d0a27ea77b246_1_0"); after.Box != base.Box || after.Tag != nil {
		t.Fatal("the human base line must stay untouched")
	}
	if !strings.HasPrefix(b.deck.NotesText, "現場メモ\n\n"+manifest.Marker+"\n") {
		t.Fatalf("notes must keep the human memo: %q", b.deck.NotesText)
	}
	mustIdempotent(t, s)
	mustPrefix(t, s.Dispatch(bg, "zone adopt 36 as Z2"), "ng zone: h1b5d0a27ea77b246_1_0 is already adopted as Z1")
	mustPrefix(t, s.Dispatch(bg, "zone adopt 5 as Z2"), "ng zone adopt: i=5 is")
	mustPrefix(t, s.Dispatch(bg, "zone adopt 37 as X2"), "ng zone adopt: zone name")

	// 人がブラウザで旗揚げの文字を動かした。
	flag.Box.X += 5
	flag.Box.Y -= 2
	moved := flag.Box
	mustPrefix(t, s.Dispatch(bg, "sync"), "ok")

	r = s.Dispatch(bg, "zone extend Z1.E 30m")
	mustPrefix(t, r, "ok zone extend Z1.E +30.0m len=")
	if !strings.Contains(r, "drift=1") || !strings.Contains(r, "base stale: Z1=h1b5d0a27ea77b246_1_0") {
		t.Fatalf("extend: %q", r)
	}
	z = mf(t, b).Zones["Z1"]
	L1 := zone.Length(f, z.Path)
	if math.Abs(L1-L0-30) > 0.5 || z.Flag != [2]float64{5, -2} {
		t.Fatalf("len +%.2f flag override %v", L1-L0, z.Flag)
	}
	flag = elem(t, b, project.ID("Z1", "flag_text"))
	mmPerM := f.MMPerMeterAlong(zone.TangentAt(f, z.Path, 0))
	if d := math.Hypot(flag.Box.X-moved.X, flag.Box.Y-moved.Y); math.Abs(d-15*mmPerM) > 0.05 || flag.Text != project.FlagText(L1) {
		t.Fatalf("flag must follow the midpoint (+15m) keeping the human offset: moved %.3fmm, %q", d, flag.Text)
	}
	elem(t, b, project.ID("Z1", "line")) // 派生の赤線
	mustIdempotent(t, s)
	mustPrefix(t, s.Dispatch(bg, "zone extend Z1.E 30mm"), "ng zone extend: length")
	mustPrefix(t, s.Dispatch(bg, "zone extend Z1.N 30m"), "ng zone: end \"N\"")
	mustPrefix(t, s.Dispatch(bg, "zone extend Z1.W -99999m"), "ng zone: trim")

	// FR-10
	r = s.Dispatch(bg, "set apply Z1 rule=FR-10")
	if r != "ok set apply Z1 rule=FR-10 sets=Z1.E.予告,Z1.W.予告 parts=Z1.E.予告#1=④,Z1.W.予告#1=④ ops=5; base stale: Z1=h1b5d0a27ea77b246_1_0 (derived overlay drawn; base untouched)" {
		t.Fatalf("set apply: %q", r)
	}
	if len(mf(t, b).Parts) != 2 {
		t.Fatalf("parts %v", mf(t, b).Parts)
	}
	src := elem(t, b, "p1_i21")
	for _, end := range []string{"E", "W"} {
		u := "Z1." + end + ".予告#1"
		sg := elem(t, b, project.ID(u, "sign"))
		ei := z.Ends[end]
		ex, ey := f.MM(z.Path[ei])
		ox, oy := zone.Outward(f, z.Path, ei)
		if along := (sg.Box.X+sg.Box.W/2-ex)*ox + (sg.Box.Y+sg.Box.H/2-ey)*oy; along < 9 {
			t.Fatalf("%s must sit outward beyond the end: %.2fmm", u, along)
		}
		if op := findCreate(b, project.ID(u, "sign")); op.SourceID != src.ObjectID {
			t.Fatalf("%s duplicates %s", u, op.SourceID)
		}
	}
	mustPrefix(t, s.Dispatch(bg, "set apply Z1 rule=FR-99"), "ng set: rule \"FR-99\" unknown")
	mustIdempotent(t, s)

	// nudge → Override、看板とバッジが付いてくる。
	sg0 := elem(t, b, project.ID("Z1.E.予告#1", "sign")).Box
	mustPrefix(t, s.Dispatch(bg, "part nudge Z1.E.予告#1 3 -2mm"), "ok part nudge Z1.E.予告#1 override=3.0,-2.0")
	sg1 := elem(t, b, project.ID("Z1.E.予告#1", "sign")).Box
	if math.Abs(sg1.X-sg0.X-3) > 1e-9 || math.Abs(sg1.Y-sg0.Y+2) > 1e-9 {
		t.Fatalf("nudge %+v -> %+v", sg0, sg1)
	}
	mustPrefix(t, s.Dispatch(bg, "part nudge nope#1 1 1"), "ng part: part nope#1")

	// set off → 部品が消える、on → 戻る（Override は保たれる）。
	mustPrefix(t, s.Dispatch(bg, "set Z1.E.予告 off"), "ok set Z1.E.予告 off parts=1")
	if _, err := b.deck.Get(project.ID("Z1.E.予告#1", "sign")); err == nil {
		t.Fatal("set off must remove the derived sign")
	}
	elem(t, b, project.ID("Z1.W.予告#1", "sign"))
	mustPrefix(t, s.Dispatch(bg, "set Z1.E.予告 on"), "ok set Z1.E.予告 on")
	if e := elem(t, b, project.ID("Z1.E.予告#1", "sign")); e.Box != sg1 {
		t.Fatalf("set on must restore at the nudged spot: %+v vs %+v", e.Box, sg1)
	}
	mustPrefix(t, s.Dispatch(bg, "set Z9.E.予告 off"), "ng set: set Z9.E.予告")
	mustIdempotent(t, s)

	// 区間をさらに延ばすと E の看板が付いてくる（Movable でも、意図側の移動には追従）。
	mustPrefix(t, s.Dispatch(bg, "zone extend Z1.E 20m"), "ok zone extend Z1.E +20.0m")
	sg2 := elem(t, b, project.ID("Z1.E.予告#1", "sign")).Box
	if d := math.Hypot(sg2.X-sg1.X, sg2.Y-sg1.Y); math.Abs(d-20*mmPerM) > 0.05 {
		t.Fatalf("E sign moved %.3fmm, want %.3f", d, 20*mmPerM)
	}
	mustIdempotent(t, s)
	// undo はノートと図形をまとめて 1 コマンド分戻す。
	mustPrefix(t, s.Dispatch(bg, "undo"), "ok undo")
	if e := elem(t, b, project.ID("Z1.E.予告#1", "sign")); e.Box != sg1 || math.Abs(zone.Length(f, mf(t, b).Zones["Z1"].Path)-L1) > 0.01 {
		t.Fatalf("undo extend: sign %+v", e.Box)
	}
	mustIdempotent(t, s)
	if j.depth() == 0 {
		t.Fatal("journal")
	}
	// 既存の part コマンドはそのまま。
	mustPrefix(t, s.Dispatch(bg, "part signs"), "ng part: agent=<id> required")
}

func TestZoneCommandsNeedNotes(t *testing.T) {
	s, b, _ := newZoneServer(t)
	b.deck.NotesID = ""
	mustPrefix(t, s.Dispatch(bg, "sync"), "ok")
	mustPrefix(t, s.Dispatch(bg, "zone adopt 36 as Z1"), "ng manifest: slide has no speaker notes")
	if len(b.applied) != 0 {
		t.Fatal("nothing must be written")
	}
	mustPrefix(t, s.Dispatch(bg, "zone bogus"), "ng zone: unknown subcommand")
}

func findCreate(b *deckBackend, id string) model.Op {
	for _, ops := range b.applied {
		for _, op := range ops {
			if op.ObjectID == id {
				return op
			}
		}
	}
	return model.Op{}
}
