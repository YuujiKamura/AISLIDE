package cp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/YuujiKamura/AISLIDE/internal/geo"
	"github.com/YuujiKamura/AISLIDE/internal/model"
	"github.com/YuujiKamura/AISLIDE/internal/parts"
	"github.com/YuujiKamura/AISLIDE/internal/slides"
)

// fakeBackend: Deck をそのまま「スライド」として保持し、Apply された ops を記録・適用する。
type fakeBackend struct {
	state   *model.Deck
	rev     int
	applied [][]model.Op
	thumbs  []string
}

func (f *fakeBackend) Load(ctx context.Context, presID string) ([]byte, error) {
	f.state.PresID = presID
	return json.Marshal(f.state)
}

// humanEdit: ブラウザで人が編集した（revision が進む）ことを模す。
func (f *fakeBackend) humanEdit(fn func(d *model.Deck)) {
	if fn != nil {
		fn(f.state)
	}
	f.rev++
	f.state.Rev = "r" + strconv.Itoa(f.rev)
}

func (f *fakeBackend) Apply(ctx context.Context, presID, pageID string, ops []model.Op, requiredRev string, d *model.Deck) (string, error) {
	if requiredRev != "" && requiredRev != f.state.Rev {
		return "", fmt.Errorf("batchUpdate: %w", slides.ErrStale)
	}
	f.applied = append(f.applied, ops)
	for _, op := range ops {
		i := f.find(op.TargetID)
		switch op.Kind {
		case model.OpCreateShape, model.OpDuplicate:
			f.state.Elements = append(f.state.Elements, model.Element{ObjectID: op.ObjectID, Kind: "shape", Box: op.Box, Tag: op.Tag, Text: textOf(op)})
		case model.OpCreateLine:
			f.state.Elements = append(f.state.Elements, model.Element{ObjectID: op.ObjectID, Kind: "line", Tag: op.Tag})
		case model.OpTransform:
			f.state.Elements[i].Box = op.Box
		case model.OpSetText:
			f.state.Elements[i].Text = op.Text
		case model.OpSetTag:
			f.state.Elements[i].Tag = op.Tag
		case model.OpDelete:
			f.state.Elements = append(f.state.Elements[:i], f.state.Elements[i+1:]...)
		}
	}
	f.humanEdit(nil)
	return f.state.Rev, nil
}

func textOf(op model.Op) string {
	if op.Shape != nil {
		return op.Shape.Text
	}
	return ""
}

func (f *fakeBackend) find(id string) int {
	for i := range f.state.Elements {
		if f.state.Elements[i].ObjectID == id {
			return i
		}
	}
	return -1
}

func (f *fakeBackend) Thumbnail(ctx context.Context, presID, pageID, path string) error {
	f.thumbs = append(f.thumbs, path)
	return nil
}

func (f *fakeBackend) last() []model.Op {
	if len(f.applied) == 0 {
		return nil
	}
	return f.applied[len(f.applied)-1]
}

type memJournal struct{ st map[string][][]model.Op }

func (j *memJournal) Push(presID, agent string, inv []model.Op) error {
	k := presID + "/" + agent
	j.st[k] = append(j.st[k], inv)
	return nil
}

func (j *memJournal) Pop(presID, agent string) ([]model.Op, error) {
	k := presID + "/" + agent
	s := j.st[k]
	if len(s) == 0 {
		return nil, errors.New("empty")
	}
	j.st[k] = s[:len(s)-1]
	return s[len(s)-1], nil
}

func (j *memJournal) depth() int {
	n := 0
	for _, s := range j.st {
		n += len(s)
	}
	return n
}

// fakeDeps: model/parts/journal/geo の契約（stub のコメント）どおりの最小実装。
func fakeDeps() deps {
	d := deps{}
	d.parseDeck = func(raw []byte, slideNo int) (*model.Deck, error) {
		var dk model.Deck
		if err := json.Unmarshal(raw, &dk); err != nil {
			return nil, err
		}
		for i := range dk.Elements {
			dk.Elements[i].Index = i
		}
		return &dk, nil
	}
	d.get = func(dk *model.Deck, ref string) (*model.Element, error) {
		if n, err := strconv.Atoi(ref); err == nil {
			if n >= 0 && n < len(dk.Elements) {
				return &dk.Elements[n], nil
			}
			return nil, model.ErrNotFound
		}
		for i := range dk.Elements {
			if dk.Elements[i].ObjectID == ref {
				return &dk.Elements[i], nil
			}
		}
		return nil, model.ErrNotFound
	}
	d.layer = func(e *model.Element) string {
		if e.Tag == nil {
			return "base"
		}
		return e.Tag.Layer
	}
	d.claim = func(dk *model.Deck, layer, agent string) error {
		if !isAgentLayer(layer) || layer == "" {
			return fmt.Errorf("reserved layer %s", layer)
		}
		if o := dk.Owners[layer]; o != "" && o != agent {
			return model.ErrNotOwner
		}
		if dk.Owners == nil {
			dk.Owners = map[string]string{}
		}
		dk.Owners[layer] = agent
		return nil
	}
	d.canWrite = func(dk *model.Deck, e *model.Element, agent string) error {
		if e.Tag == nil || !isAgentLayer(e.Tag.Layer) {
			return model.ErrReadOnly
		}
		o := dk.Owners[e.Tag.Layer]
		if o == "" {
			o = e.Tag.Owner
		}
		if o != agent {
			return model.ErrNotOwner
		}
		return nil
	}
	d.mapBG = func(dk *model.Deck) (*model.Element, *model.GeorefTag, error) {
		for i := range dk.Elements {
			if t := dk.Elements[i].Tag; t != nil && t.Kind == "map_bg" && t.Georef != nil {
				return &dk.Elements[i], t.Georef, nil
			}
		}
		return nil, nil, model.ErrNotFound
	}
	// 線形の fake 投影: 1 度 = 1e5 px。
	d.toMM = func(g geo.Georef, img geo.Box, lat, lon float64) (float64, float64) {
		s := img.W / float64(g.PxW)
		return img.X + img.W/2 + (lon-g.CenterLon)*1e5*s, img.Y + img.H/2 - (lat-g.CenterLat)*1e5*s
	}
	d.toLatLon = func(g geo.Georef, img geo.Box, x, y float64) (float64, float64) {
		s := img.W / float64(g.PxW)
		return g.CenterLat - (y-img.Y-img.H/2)/(1e5*s), g.CenterLon + (x-img.X-img.W/2)/(1e5*s)
	}
	d.resolvePoint = func(dk *model.Deck, s string) (float64, float64, error) {
		if v, ok := strings.CutPrefix(s, "mm:"); ok {
			a, b, _ := strings.Cut(v, ",")
			x, err1 := strconv.ParseFloat(a, 64)
			y, err2 := strconv.ParseFloat(b, 64)
			if err1 != nil || err2 != nil {
				return 0, 0, errors.New("bad mm point")
			}
			return x, y, nil
		}
		if n, ok := strings.CutPrefix(s, "site:"); ok {
			bg, gt, err := d.mapBG(dk)
			if err != nil {
				return 0, 0, err
			}
			ll, ok := gt.Sites[n]
			if !ok {
				return 0, 0, model.ErrNotFound
			}
			x, y := d.toMM(geoOf(gt), gbox(bg.Box), ll[0], ll[1])
			return x, y, nil
		}
		return 0, 0, errors.New("unsupported point in fake")
	}
	d.resolveLength = func(dk *model.Deck, s string) (float64, error) {
		if v, ok := strings.CutSuffix(s, "mm"); ok {
			return strconv.ParseFloat(v, 64)
		}
		if v, ok := strings.CutSuffix(s, "m"); ok {
			f, err := strconv.ParseFloat(v, 64)
			return f * 2, err // fake 縮尺 2mm/m
		}
		return 0, errors.New("want m or mm")
	}
	d.build = func(kind string, args map[string]string, ctx parts.Ctx) ([]model.Op, error) {
		if kind != "note" {
			return nil, errors.New("unknown kind " + kind)
		}
		x, y, err := d.resolvePoint(ctx.Deck, args["at"])
		if err != nil {
			return nil, err
		}
		return []model.Op{{Kind: model.OpCreateShape, ObjectID: ctx.NewID(ctx.Layer), Box: model.Box{X: x, Y: y, W: 20, H: 8},
			Shape: &model.ShapeStyle{ShapeType: "RECTANGLE", Text: args["t"]},
			Tag:   &model.Tag{Layer: ctx.Layer, Owner: ctx.Agent, Kind: kind}}}, nil
	}
	d.kinds = func() []string { return []string{"site", "sign", "note"} }
	d.inverse = func(ops []model.Op, before *model.Deck) ([]model.Op, error) {
		var inv []model.Op
		for _, op := range ops {
			e, _ := d.get(before, op.TargetID)
			var r model.Op
			switch op.Kind {
			case model.OpCreateShape, model.OpCreateLine, model.OpDuplicate:
				r = model.Op{Kind: model.OpDelete, TargetID: op.ObjectID}
			case model.OpTransform:
				r = model.Op{Kind: model.OpTransform, TargetID: op.TargetID, Box: e.Box}
			case model.OpSetText:
				r = model.Op{Kind: model.OpSetText, TargetID: op.TargetID, Text: e.Text}
			case model.OpSetTag:
				r = model.Op{Kind: model.OpSetTag, TargetID: op.TargetID, Tag: e.Tag}
			case model.OpDelete:
				if e.Kind == "image" {
					return nil, errors.New("cannot recreate image")
				}
				r = model.Op{Kind: model.OpCreateShape, ObjectID: e.ObjectID, Box: e.Box, Tag: e.Tag,
					Shape: &model.ShapeStyle{ShapeType: "RECTANGLE", Text: e.Text}}
			default:
				return nil, errors.New("no inverse for " + string(op.Kind))
			}
			inv = append([]model.Op{r}, inv...)
		}
		return inv, nil
	}
	n := 0
	d.newID = func(layer string) string { n++; return fmt.Sprintf("kc_%s_%08x", layer, n) }
	d.isStale = func(err error) bool { return errors.Is(err, slides.ErrStale) }
	return d
}

// fixture: 背景（georef）、人の線 2 本（base）、agent1 の note 1 個。
func fixture() *model.Deck {
	return &model.Deck{
		Rev: "r0", PageID: "p1", PageW: 420, PageH: 297,
		Elements: []model.Element{
			{ObjectID: "p1_i4", Kind: "image", Box: model.Box{X: 0, Y: 0, W: 420, H: 297},
				Tag: &model.Tag{Layer: "map", Kind: "map_bg", Georef: &model.GeorefTag{Z: 18, Center: [2]float64{32.77, 130.77}, Px: [2]int{1280, 905},
					Sites: map[string][2]float64{"1": {32.7701, 130.7699}, "2": {32.7699, 130.7703}}}}},
			{ObjectID: "h1b5d0a27ea77b246_1_0", Kind: "line", Box: model.Box{X: 100, Y: 100, W: 50, H: 0}},
			{ObjectID: "h1b5d0a27ea77b246_1_1", Kind: "line", Box: model.Box{X: 100, Y: 120, W: 50, H: 0}},
			{ObjectID: "kc_notes_00000001", Kind: "shape", Text: "片側交互通行", Box: model.Box{X: 200, Y: 50, W: 30, H: 8},
				Tag: &model.Tag{Layer: "notes", Owner: "agent1", Kind: "note"}},
		},
	}
}

func newTestServer(t *testing.T) (*Server, *fakeBackend, *memJournal) {
	t.Helper()
	fb := &fakeBackend{state: fixture()}
	j := &memJournal{st: map[string][][]model.Op{}}
	s := New(fb, j)
	s.d = fakeDeps()
	if r := s.Dispatch(context.Background(), "open https://docs.google.com/presentation/d/PRES123/edit"); !strings.HasPrefix(r, "ok") {
		t.Fatalf("open: %s", r)
	}
	if r := s.Dispatch(context.Background(), "claim notes agent=agent1"); !strings.HasPrefix(r, "ok") {
		t.Fatalf("claim: %s", r)
	}
	return s, fb, j
}
