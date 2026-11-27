package cp

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/YuujiKamura/AISLIDE/internal/manifest"
	"github.com/YuujiKamura/AISLIDE/internal/model"
	"github.com/YuujiKamura/AISLIDE/internal/project"
	"github.com/YuujiKamura/AISLIDE/internal/zone"
)

// deckBackend: 実 fixture（akitsu）をパースしたデッキを保持し、Apply された ops を適用する。
type deckBackend struct {
	deck    *model.Deck
	applied [][]model.Op
}

func (b *deckBackend) Load(ctx context.Context, presID string) ([]byte, error) {
	return []byte("{}"), nil
}
func (b *deckBackend) Thumbnail(ctx context.Context, presID, pageID, path string) error {
	return nil
}

func (b *deckBackend) Apply(ctx context.Context, presID, pageID string, ops []model.Op, requiredRev string, d *model.Deck) (string, error) {
	b.applied = append(b.applied, ops)
	for _, op := range ops {
		i := -1
		for k := range b.deck.Elements {
			if b.deck.Elements[k].ObjectID == op.TargetID {
				i = k
			}
		}
		switch op.Kind {
		case model.OpCreateShape, model.OpDuplicate:
			e := model.Element{ObjectID: op.ObjectID, Kind: "shape", Box: op.Box, Tag: op.Tag}
			if op.Shape != nil {
				e.Text = op.Shape.Text
			}
			b.deck.Elements = append(b.deck.Elements, e)
		case model.OpCreateLine:
			b.deck.Elements = append(b.deck.Elements, model.Element{ObjectID: op.ObjectID, Kind: "line", Tag: op.Tag,
				Box:  model.Box{X: math.Min(op.X1, op.X2), Y: math.Min(op.Y1, op.Y2), W: math.Abs(op.X2 - op.X1), H: math.Abs(op.Y2 - op.Y1)},
				Flip: [2]bool{op.X2 < op.X1, op.Y2 < op.Y1}})
		case model.OpTransform:
			b.deck.Elements[i].Box = op.Box
		case model.OpSetText:
			b.deck.Elements[i].Text = op.Text
		case model.OpDelete:
			b.deck.Elements = append(b.deck.Elements[:i], b.deck.Elements[i+1:]...)
		}
	}
	return "r", nil
}

// memStore: manifest I/O（別担当）の代わり。WriteOp の時点で保存したものとみなす。
type memStore struct {
	m      *manifest.Manifest
	writes int
}

const opFakeManifest model.OpKind = "fake_manifest"

func (s *memStore) Load(d *model.Deck) (*manifest.Manifest, error) {
	if s.m == nil {
		return nil, nil
	}
	var cp manifest.Manifest
	b, _ := json.Marshal(s.m)
	_ = json.Unmarshal(b, &cp)
	return &cp, nil
}

func (s *memStore) WriteOp(d *model.Deck, m *manifest.Manifest) (model.Op, error) {
	s.m = m
	s.writes++
	return model.Op{Kind: opFakeManifest}, nil
}

// miniRun: project.Run（別担当）の契約どおりの最小実装。
func miniRun(m *manifest.Manifest, d *model.Deck, ps []project.Projector) ([]model.Op, []project.Drift, error) {
	var ops []model.Op
	var drifts []project.Drift
	want := map[string]bool{}
	for _, p := range ps {
		specs, err := p.Project(m, d)
		if err != nil {
			return nil, nil, err
		}
		for _, sp := range specs {
			want[sp.ID] = true
			e, err := d.Get(sp.ID)
			if err != nil {
				ops = append(ops, sp.Create...)
				continue
			}
			if !boxNear(e.Box, sp.Box, 0.1) {
				if sp.Movable {
					drifts = append(drifts, project.Drift{Unit: sp.Unit, Role: sp.Role, DX: e.Box.X - sp.Box.X, DY: e.Box.Y - sp.Box.Y})
				} else {
					ops = append(ops, model.Op{Kind: model.OpTransform, TargetID: sp.ID, Box: sp.Box})
				}
			}
			if sp.Text != "" && e.Text != sp.Text {
				ops = append(ops, model.Op{Kind: model.OpSetText, TargetID: sp.ID, Text: sp.Text})
			}
		}
		for i := range d.Elements {
			e := &d.Elements[i]
			if e.Tag != nil && e.Tag.Layer == project.LayerDerived {
				if u, _ := e.Tag.Params["unit"].(string); p.Owns(u) && !want[e.ObjectID] {
					ops = append(ops, model.Op{Kind: model.OpDelete, TargetID: e.ObjectID})
				}
			}
		}
	}
	return ops, drifts, nil
}

func testID(unit, role string) string { return "kd_" + unit + "_" + role }

func newZoneServer(t *testing.T) (*Server, *deckBackend, *memStore) {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/akitsu_presentation.json")
	if err != nil {
		t.Fatal(err)
	}
	d, err := model.ParseDeck(raw, 1)
	if err != nil {
		t.Fatal(err)
	}
	b := &deckBackend{deck: d}
	s := New(b, nil)
	s.d.parseDeck = func([]byte, int) (*model.Deck, error) {
		c := *b.deck
		c.Elements = append([]model.Element(nil), b.deck.Elements...)
		return &c, nil
	}
	st := &memStore{}
	s.zx = zoneExt{store: st, run: miniRun,
		projectors: []project.Projector{project.Zones{IDFunc: testID}, project.Parts{IDFunc: testID}}}
	mustPrefix(t, s.Dispatch(bg, "open PRES"), "ok")
	return s, b, st
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
	s, b, st := newZoneServer(t)
	f, _ := zone.FrameOf(b.deck)
	base := *elem(t, b, "h1b5d0a27ea77b246_1_0")

	r := s.Dispatch(bg, "zone adopt 36 as Z1")
	mustPrefix(t, r, "ok zone adopt Z1 from=h1b5d0a27ea77b246_1_0 kind=closure len=")
	if !strings.Contains(r, "ends=EW") || strings.Contains(r, "stale") {
		t.Fatalf("adopt: %q", r)
	}
	z := st.m.Zones["Z1"]
	L0 := zone.Length(f, z.Path)
	if want, _ := b.deck.MetersBetween(base.LineEnds()); math.Abs(L0-want) > 0.01 {
		t.Fatalf("zone %.2fm vs line %.2fm", L0, want)
	}
	flag := elem(t, b, "kd_Z1_flag_text")
	if flag.Text != project.FlagText(L0) || !strings.HasPrefix(flag.Text, "L=") {
		t.Fatalf("flag %q", flag.Text)
	}
	if after := elem(t, b, "h1b5d0a27ea77b246_1_0"); after.Box != base.Box || after.Tag != nil {
		t.Fatal("the human base line must stay untouched")
	}
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
	z = st.m.Zones["Z1"]
	L1 := zone.Length(f, z.Path)
	if math.Abs(L1-L0-30) > 0.5 || z.Flag != [2]float64{5, -2} {
		t.Fatalf("len +%.2f flag override %v", L1-L0, z.Flag)
	}
	flag = elem(t, b, "kd_Z1_flag_text")
	mmPerM := f.MMPerMeterAlong(zone.TangentAt(f, z.Path, 0))
	if d := math.Hypot(flag.Box.X-moved.X, flag.Box.Y-moved.Y); math.Abs(d-15*mmPerM) > 0.05 || flag.Text != project.FlagText(L1) {
		t.Fatalf("flag must follow the midpoint (+15m) keeping the human offset: moved %.3fmm, %q", d, flag.Text)
	}
	elem(t, b, "kd_Z1_line") // 派生の赤線
	mustPrefix(t, s.Dispatch(bg, "zone extend Z1.E 30mm"), "ng zone extend: length")
	mustPrefix(t, s.Dispatch(bg, "zone extend Z1.N 30m"), "ng zone: end \"N\"")
	mustPrefix(t, s.Dispatch(bg, "zone extend Z1.W -99999m"), "ng zone: trim")

	// FR-10
	r = s.Dispatch(bg, "set apply Z1 rule=FR-10")
	if r != "ok set apply Z1 rule=FR-10 sets=Z1.E.予告,Z1.W.予告 parts=Z1.E.予告#1=④,Z1.W.予告#1=④ ops=4; base stale: Z1=h1b5d0a27ea77b246_1_0 (derived overlay drawn; base untouched)" {
		t.Fatalf("set apply: %q", r)
	}
	if len(st.m.Parts) != 2 {
		t.Fatalf("parts %v", st.m.Parts)
	}
	src := elem(t, b, "p1_i21")
	for _, end := range []string{"E", "W"} {
		u := "Z1." + end + ".予告#1"
		sg := elem(t, b, "kd_"+u+"_sign")
		ei := z.Ends[end]
		ex, ey := f.MM(z.Path[ei])
		ox, oy := zone.Outward(f, z.Path, ei)
		if along := (sg.Box.X+sg.Box.W/2-ex)*ox + (sg.Box.Y+sg.Box.H/2-ey)*oy; along < 9 {
			t.Fatalf("%s must sit outward beyond the end: %.2fmm", u, along)
		}
		if op := findCreate(b, "kd_"+u+"_sign"); op.SourceID != src.ObjectID {
			t.Fatalf("%s duplicates %s", u, op.SourceID)
		}
	}
	mustPrefix(t, s.Dispatch(bg, "set apply Z1 rule=FR-99"), "ng set: rule \"FR-99\" unknown")

	// nudge → Override、看板とバッジが付いてくる。
	sg0 := elem(t, b, "kd_Z1.E.予告#1_sign").Box
	mustPrefix(t, s.Dispatch(bg, "part nudge Z1.E.予告#1 3 -2mm"), "ok part nudge Z1.E.予告#1 override=3.0,-2.0")
	sg1 := elem(t, b, "kd_Z1.E.予告#1_sign").Box
	if math.Abs(sg1.X-sg0.X-3) > 1e-9 || math.Abs(sg1.Y-sg0.Y+2) > 1e-9 {
		t.Fatalf("nudge %+v -> %+v", sg0, sg1)
	}
	mustPrefix(t, s.Dispatch(bg, "part nudge nope#1 1 1"), "ng part: part nope#1")

	// set off → 部品が消える、on → 戻る（Override は保たれる）。
	mustPrefix(t, s.Dispatch(bg, "set Z1.E.予告 off"), "ok set Z1.E.予告 off parts=1")
	if _, err := b.deck.Get("kd_Z1.E.予告#1_sign"); err == nil {
		t.Fatal("set off must remove the derived sign")
	}
	elem(t, b, "kd_Z1.W.予告#1_sign")
	mustPrefix(t, s.Dispatch(bg, "set Z1.E.予告 on"), "ok set Z1.E.予告 on")
	if e := elem(t, b, "kd_Z1.E.予告#1_sign"); e.Box != sg1 {
		t.Fatalf("set on must restore at the nudged spot: %+v vs %+v", e.Box, sg1)
	}
	mustPrefix(t, s.Dispatch(bg, "set Z9.E.予告 off"), "ng set: set Z9.E.予告")

	// 区間をさらに延ばすと E の看板が付いてくる（Movable でも、意図側の移動には追従）。
	mustPrefix(t, s.Dispatch(bg, "zone extend Z1.E 20m"), "ok zone extend Z1.E +20.0m")
	sg2 := elem(t, b, "kd_Z1.E.予告#1_sign").Box
	if d := math.Hypot(sg2.X-sg1.X, sg2.Y-sg1.Y); math.Abs(d-20*mmPerM) > 0.05 {
		t.Fatalf("E sign moved %.3fmm, want %.3f", d, 20*mmPerM)
	}
	// 既存の part コマンドはそのまま。
	mustPrefix(t, s.Dispatch(bg, "part signs"), "ng part: agent=<id> required")
}

func TestZoneCommandsNeedStore(t *testing.T) {
	s, _, _ := newZoneServer(t)
	s.zx.store = nil
	mustPrefix(t, s.Dispatch(bg, "zone adopt 36 as Z1"), "ng zone: manifest store not wired")
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
