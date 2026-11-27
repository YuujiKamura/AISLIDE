package cp

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/YuujiKamura/AISLIDE/internal/manifest"
	"github.com/YuujiKamura/AISLIDE/internal/model"
	"github.com/YuujiKamura/AISLIDE/internal/project"
	"github.com/YuujiKamura/AISLIDE/internal/slides"
)

// simBackend: 実デッキ（testdata）を model.Deck として持ち、ops を Slides と同じ意味で適用する。
// 適用前に slides.ToRequests が通ること（ノート shape への set_text を含む）も確かめる。
type simBackend struct {
	t       *testing.T
	state   *model.Deck
	rev     int
	applied [][]model.Op
}

func (b *simBackend) Load(ctx context.Context, presID string) ([]byte, error) {
	return json.Marshal(b.state)
}

func (b *simBackend) Thumbnail(ctx context.Context, presID, pageID, path string) error { return nil }

func (b *simBackend) Apply(ctx context.Context, presID, pageID string, ops []model.Op, requiredRev string, d *model.Deck) (string, error) {
	if requiredRev != b.state.Rev {
		return "", fmt.Errorf("batchUpdate: %w", slides.ErrStale)
	}
	if _, err := slides.ToRequests(ops, pageID, d); err != nil {
		return "", err
	}
	st := b.state
	find := func(id string) int {
		for i := range st.Elements {
			if st.Elements[i].ObjectID == id {
				return i
			}
		}
		b.t.Fatalf("sim: %s not found", id)
		return -1
	}
	for _, op := range ops {
		switch op.Kind {
		case model.OpCreateShape:
			sh := *op.Shape
			st.Elements = append(st.Elements, model.Element{ObjectID: op.ObjectID, Kind: "shape", Box: op.Box, Text: sh.Text,
				Shape: &sh, Tag: op.Tag, SizeEMU: [2]float64{op.Box.W * model.EMUPerMM, op.Box.H * model.EMUPerMM}})
		case model.OpCreateLine:
			bx := model.Box{X: math.Min(op.X1, op.X2), Y: math.Min(op.Y1, op.Y2), W: math.Abs(op.X2 - op.X1), H: math.Abs(op.Y2 - op.Y1)}
			ls := *op.Line
			st.Elements = append(st.Elements, model.Element{ObjectID: op.ObjectID, Kind: "line", Box: bx, Line: &ls, Tag: op.Tag,
				Flip: [2]bool{op.X2 < op.X1, op.Y2 < op.Y1}, SizeEMU: [2]float64{bx.W * model.EMUPerMM, bx.H * model.EMUPerMM}})
		case model.OpTransform:
			st.Elements[find(op.TargetID)].Box = op.Box
		case model.OpSetText:
			if op.TargetID == st.NotesID {
				st.NotesText = op.Text
			} else {
				st.Elements[find(op.TargetID)].Text = op.Text
			}
		case model.OpSetTag:
			st.Elements[find(op.TargetID)].Tag = op.Tag
		case model.OpDelete:
			i := find(op.TargetID)
			st.Elements = append(st.Elements[:i], st.Elements[i+1:]...)
		}
	}
	for i := range st.Elements {
		st.Elements[i].Index = i
	}
	b.applied = append(b.applied, ops)
	b.rev++
	st.Rev = "r" + strconv.Itoa(b.rev)
	return st.Rev, nil
}

func newAkitsuServer(t *testing.T, notes string) (*Server, *simBackend, *memJournal) {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/akitsu_presentation.json")
	if err != nil {
		t.Fatal(err)
	}
	d, err := model.ParseDeck(raw, 1)
	if err != nil {
		t.Fatal(err)
	}
	d.NotesText = notes // 実デッキのノートは空。人のメモがある場合を模す。
	sb := &simBackend{t: t, state: d}
	j := &memJournal{st: map[string][][]model.Op{}}
	s := New(sb, j)
	s.d.parseDeck = func(raw []byte, slideNo int) (*model.Deck, error) {
		var dk model.Deck
		return &dk, json.Unmarshal(raw, &dk)
	}
	mustPrefix(t, s.Dispatch(bg, "open "+d.PresID), "ok")
	return s, sb, j
}

func opsOf(t *testing.T, resp string) int {
	t.Helper()
	i := strings.Index(resp, "ops=")
	if i < 0 {
		t.Fatalf("no ops= in %q", resp)
	}
	f := strings.Fields(resp[i+4:])
	n, err := strconv.Atoi(strings.TrimRight(f[0], ";"))
	if err != nil {
		t.Fatalf("ops in %q", resp)
	}
	return n
}

// mustIdempotent: 意図を変えずに `project` を繰り返しても 0 件。
func mustIdempotent(t *testing.T, s *Server) {
	t.Helper()
	for i := 0; i < 2; i++ {
		if r := s.Dispatch(bg, "project"); r != "ok ops=0 drift=0" {
			t.Fatalf("project #%d: %q", i, r)
		}
	}
}

func derivedOf(d *model.Deck) map[string]*model.Element {
	out := map[string]*model.Element{}
	for i := range d.Elements {
		if project.UnitOf(&d.Elements[i]) != "" {
			out[d.Elements[i].ObjectID] = &d.Elements[i]
		}
	}
	return out
}

func textOfID(t *testing.T, d *model.Deck, id string) string {
	t.Helper()
	e, err := d.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return e.Text
}

func TestManifestInitShowKeepsNotes(t *testing.T) {
	s, sb, _ := newAkitsuServer(t, "現場メモ: 夜間施工")
	mustPrefix(t, s.Dispatch(bg, "manifest show"), `ok {"manifest":false}`)
	mustPrefix(t, s.Dispatch(bg, "units"), "ok n=0 manifest=none")
	mustPrefix(t, s.Dispatch(bg, "manifest init"), "ok manifest=created ops=1")
	if !strings.HasPrefix(sb.state.NotesText, "現場メモ: 夜間施工\n\n"+manifest.Marker+"\n") {
		t.Fatalf("notes = %q", sb.state.NotesText)
	}
	mustPrefix(t, s.Dispatch(bg, "manifest init"), "ok manifest=exists ops=0")
	want := `ok {"manifest":true,"version":1,"sites":[],"zones":[],"parts":[],"sets":[]}`
	if r := s.Dispatch(bg, "manifest show"); r != want {
		t.Fatalf("show = %q", r)
	}
	mustIdempotent(t, s)
	mustPrefix(t, s.Dispatch(bg, "manifest bogus"), "ng usage")
}

func TestLegendCommandsAkitsu(t *testing.T) {
	s, sb, j := newAkitsuServer(t, "")
	r := s.Dispatch(bg, "legend adopt agent=a1")
	mustPrefix(t, r, "ok legend items=8 keys=①②③④⑤⑥⑦⑧ mode=draft ops=1") // ノートだけ
	mustIdempotent(t, s)
	if r := s.Dispatch(bg, "units"); !strings.Contains(r, "legend.③ on 100m先工事中") || !strings.HasPrefix(r, "ok n=9; legend mode=draft") {
		t.Fatalf("units = %q", r)
	}

	// off ③: ノート更新 + 斜線 2 本を 1 batch で。
	nApplied := len(sb.applied)
	r = s.Dispatch(bg, "legend off ③ agent=a1")
	mustPrefix(t, r, "ok legend ③ off ops=3")
	if len(sb.applied) != nApplied+1 {
		t.Fatalf("must be one batchUpdate, got %d", len(sb.applied)-nApplied)
	}
	dv := derivedOf(sb.state)
	img3, _ := sb.state.Get("p1_i19")
	for _, role := range []string{project.RoleStrike1, project.RoleStrike2} {
		e := dv[project.ID("legend.③", role)]
		if e == nil || e.Kind != "line" {
			t.Fatalf("missing %s: %v", role, dv)
		}
		if cx, cy := e.Center(); math.Abs(cx-(img3.Box.X+img3.Box.W/2)) > 2 || math.Abs(cy-(img3.Box.Y+img3.Box.H/2)) > 8 {
			t.Errorf("%s not over ③ image: %+v vs %+v", role, e.Box, img3.Box)
		}
	}
	if len(dv) != 2 {
		t.Fatalf("derived = %d", len(dv))
	}
	mustIdempotent(t, s)
	mustPrefix(t, s.Dispatch(bg, "manifest show"), `ok {"manifest":true,"version":1,"legend":{"mode":"draft","items":8,"off":["③"]}`)

	// 派生層は agent が触れない。
	strike := project.ID("legend.③", project.RoleStrike1)
	for _, cmd := range []string{"delete " + strike + " agent=a1", "move " + strike + " 1 1 agent=a1", "claim derived agent=a1", "clear derived agent=a1"} {
		if r := s.Dispatch(bg, cmd); !strings.HasPrefix(r, "ng ") {
			t.Errorf("%s: %q", cmd, r)
		}
	}

	// 人が斜線をずらした → project で戻る。
	for i := range sb.state.Elements {
		if sb.state.Elements[i].ObjectID == strike {
			sb.state.Elements[i].Box.X += 5
		}
	}
	sb.rev++
	sb.state.Rev = "human"
	mustPrefix(t, s.Dispatch(bg, "sync"), "ok")
	mustPrefix(t, s.Dispatch(bg, "project"), "ok ops=1 drift=0")
	mustIdempotent(t, s)

	// final: 斜線 2 本削除 + マスク 1 + ラベル ④..⑧ の番号詰め 5 + ノート 1。
	r = s.Dispatch(bg, "legend mode final agent=a1")
	mustPrefix(t, r, "ok legend mode=final ops=9")
	dv = derivedOf(sb.state)
	if len(dv) != 1 || dv[project.ID("legend.③", project.RoleMask)] == nil {
		t.Fatalf("final derived = %v", dv)
	}
	for id, w := range map[string]string{"p1_i20": "③100m先工事中", "p1_i22": "③50m先工事中", "p1_i24": "④徐行して下さい", "p1_i30": "⑦工事情報板"} {
		if g := textOfID(t, sb.state, id); g != w {
			t.Errorf("final %s = %q want %q", id, g, w)
		}
	}
	mustIdempotent(t, s)
	mustPrefix(t, s.Dispatch(bg, "legend adopt agent=a1"), "ng legend: legend is in final mode")

	// draft に戻す: 元の番号、マスク無し、斜線復活。
	mustPrefix(t, s.Dispatch(bg, "legend mode draft agent=a1"), "ok legend mode=draft ops=9")
	if g := textOfID(t, sb.state, "p1_i22"); g != "④50m先工事中" {
		t.Fatalf("draft restore: %q", g)
	}
	if dv = derivedOf(sb.state); len(dv) != 2 {
		t.Fatalf("draft derived = %v", dv)
	}
	mustIdempotent(t, s)

	// undo（journal）: mode draft を戻すと final の見た目とノートに戻る。
	before := j.depth()
	mustPrefix(t, s.Dispatch(bg, "undo agent=a1"), "ok undo")
	if j.depth() != before-1 || textOfID(t, sb.state, "p1_i22") != "③50m先工事中" {
		t.Fatalf("undo: depth %d→%d label %q", before, j.depth(), textOfID(t, sb.state, "p1_i22"))
	}
	if m, _, _ := manifest.Parse(sb.state.NotesText); m.Legend.Mode != "final" {
		t.Fatalf("undo must restore manifest mode, got %s", m.Legend.Mode)
	}
	mustIdempotent(t, s)

	mustPrefix(t, s.Dispatch(bg, "legend off ⑨ agent=a1"), "ng legend: legend ⑨")
	mustPrefix(t, s.Dispatch(bg, "legend mode clean"), "ng legend")
	mustPrefix(t, s.Dispatch(bg, "legend"), "ng usage")
}

// manifest が無い状態で legend off は ng（何も書かない）。
func TestLegendOffWithoutAdopt(t *testing.T) {
	s, sb, _ := newAkitsuServer(t, "")
	mustPrefix(t, s.Dispatch(bg, "legend off 3"), "ng legend: no legend")
	if len(sb.applied) != 0 || sb.state.NotesText != "" {
		t.Fatal("nothing must be written")
	}
}

// 他人が先に書いた（revision が進んだ）なら ng stale、ノートも図も書かない。
func TestLegendStale(t *testing.T) {
	s, sb, _ := newAkitsuServer(t, "")
	sb.state.Rev = "someone-else"
	mustPrefix(t, s.Dispatch(bg, "legend adopt"), "ng stale")
	if len(sb.applied) != 0 {
		t.Fatal("stale must not apply")
	}
	mustPrefix(t, s.Dispatch(bg, "legend adopt"), "ok legend items=8")
}
