package cp

import (
	"context"
	"net"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/YuujiKamura/AISLIDE/internal/model"
)

var bg = context.Background()

func mustPrefix(t *testing.T, got, prefix string) {
	t.Helper()
	if !strings.HasPrefix(got, prefix) {
		t.Fatalf("want prefix %q, got %q", prefix, got)
	}
}

func TestOpenAndState(t *testing.T) {
	s, _, _ := newTestServer(t)
	if s.presID != "PRES123" {
		t.Fatalf("presID from URL: %q", s.presID)
	}
	mustPrefix(t, s.Dispatch(bg, "state"), "ok pres=PRES123 rev=r0 layers=3 pick=-")
}

func TestNoDeck(t *testing.T) {
	s := New(&fakeBackend{state: fixture()}, nil)
	s.d = fakeDeps()
	mustPrefix(t, s.Dispatch(bg, "list"), "ng no deck")
	mustPrefix(t, s.Dispatch(bg, "bogus"), "ng unknown command")
}

// (a) base（人が描いた線）は読み取り専用。
func TestBaseIsReadOnly(t *testing.T) {
	s, fb, _ := newTestServer(t)
	for _, cmd := range []string{
		"move 1 5 0 agent=agent1",
		"moveto h1b5d0a27ea77b246_1_0 mm:10,10 agent=agent1",
		"delete 2 agent=agent1",
		"settext 1 hello agent=agent1",
		"delete 0 agent=agent1", // map 背景も agent からは不可
	} {
		r := s.Dispatch(bg, cmd)
		mustPrefix(t, r, "ng ")
		if !strings.Contains(r, "read-only") {
			t.Errorf("%s: want read-only, got %q", cmd, r)
		}
	}
	if len(fb.applied) != 0 {
		t.Fatalf("nothing must be applied, got %v", fb.applied)
	}
	mustPrefix(t, s.Dispatch(bg, "move 1 5 0"), "ng move: agent=<id> required")
}

// (b) 他 agent のレイヤには書けない。
func TestOtherAgentLayer(t *testing.T) {
	s, fb, _ := newTestServer(t)
	for _, cmd := range []string{
		"move 3 5 0 agent=agent2",
		"delete kc_notes_00000001 agent=agent2",
		"claim notes agent=agent2",
		"part notes note agent=agent2 at=mm:50,50 t=x",
		"clear notes agent=agent2",
	} {
		r := s.Dispatch(bg, cmd)
		mustPrefix(t, r, "ng ")
		if !strings.Contains(r, "another agent") {
			t.Errorf("%s: want not-owner, got %q", cmd, r)
		}
	}
	if len(fb.applied) != 0 {
		t.Fatalf("nothing must be applied, got %v", fb.applied)
	}
	// 本人は書ける
	mustPrefix(t, s.Dispatch(bg, "move 3 5 -2 agent=agent1"), "ok move i=3 id=kc_notes_00000001")
	if b := fb.state.Elements[3].Box; b.X != 205 || b.Y != 48 {
		t.Fatalf("moved box %+v", b)
	}
}

// (c) list は index を出し、空行で終わる。
func TestListIndexes(t *testing.T) {
	s, _, _ := newTestServer(t)
	r := s.Dispatch(bg, "list")
	if !strings.HasSuffix(r, "\n") {
		t.Fatalf("list must end with newline (empty line terminator after framing): %q", r)
	}
	lines := strings.Split(strings.TrimSuffix(r, "\n"), "\n")
	if lines[0] != "ok n=4" || len(lines) != 5 {
		t.Fatalf("list: %q", r)
	}
	for i, want := range []string{
		`^0 map map_bg p1_i4 x=0\.0 y=0\.0 w=420\.0 h=297\.0$`,
		`^1 base line h1b5d0a27ea77b246_1_0 x=100\.0 y=100\.0 w=50\.0 h=0\.0 @32\.\d+,130\.\d+$`,
		`^2 base line h1b5d0a27ea77b246_1_1 `,
		`^3 notes note kc_notes_00000001 x=200\.0 .* 「片側交互通行」$`,
	} {
		if !regexp.MustCompile(want).MatchString(lines[i+1]) {
			t.Errorf("line %d: %q !~ %s", i, lines[i+1], want)
		}
	}
	r = s.Dispatch(bg, "list notes")
	if r != "ok n=1\n"+lines[4]+"\n" {
		t.Fatalf("list notes: %q", r)
	}
	if r := s.Dispatch(bg, "list nothing"); r != "ok n=0\n" {
		t.Fatalf("empty list: %q", r)
	}
}

// (d) revision が変わっていたら ng stale、再読込される。
func TestStaleRevision(t *testing.T) {
	s, fb, j := newTestServer(t)
	fb.humanEdit(func(d *model.Deck) { d.Elements[3].Box.X = 300 }) // 人がブラウザで動かした
	r := s.Dispatch(bg, "move 3 5 0 agent=agent1")
	if r != "ng stale rev changed; re-run list" {
		t.Fatalf("got %q", r)
	}
	if len(fb.applied) != 0 || j.depth() != 0 {
		t.Fatal("stale write must not apply or journal")
	}
	if s.deck.Rev != fb.state.Rev || s.deck.Elements[3].Box.X != 300 {
		t.Fatalf("deck not reloaded: rev=%s x=%v", s.deck.Rev, s.deck.Elements[3].Box.X)
	}
	if s.deck.Owners["notes"] != "agent1" {
		t.Fatal("owners must survive reload")
	}
	mustPrefix(t, s.Dispatch(bg, "move 3 5 0 agent=agent1"), "ok move")
}

func TestSyncCountsHumanMoves(t *testing.T) {
	s, fb, _ := newTestServer(t)
	fb.humanEdit(func(d *model.Deck) { d.Elements[3].Box.Y = 70; d.Elements[1].Box.X = 0 })
	if r := s.Dispatch(bg, "sync"); r != "ok rev=r1 changed=1" {
		t.Fatalf("sync: %q", r)
	}
}

// (e) undo は記録した逆操作を適用する。
func TestUndoAppliesInverse(t *testing.T) {
	s, fb, j := newTestServer(t)
	orig := fb.state.Elements[3].Box
	mustPrefix(t, s.Dispatch(bg, "move 3 10m 0 agent=agent1"), "ok move") // fake 縮尺 2mm/m
	if fb.state.Elements[3].Box.X != orig.X+20 {
		t.Fatalf("move: %+v", fb.state.Elements[3].Box)
	}
	if j.depth() != 1 {
		t.Fatalf("journal depth %d", j.depth())
	}
	mustPrefix(t, s.Dispatch(bg, "undo agent=agent2"), "ng undo: nothing")
	mustPrefix(t, s.Dispatch(bg, "undo agent=agent1"), "ok undo ops=1")
	last := fb.last()
	if len(last) != 1 || last[0].Kind != model.OpTransform || last[0].TargetID != "kc_notes_00000001" || last[0].Box != orig {
		t.Fatalf("undo applied %+v", last)
	}
	if fb.state.Elements[3].Box != orig || j.depth() != 0 {
		t.Fatal("undo did not restore")
	}
	mustPrefix(t, s.Dispatch(bg, "undo agent=agent1"), "ng undo: nothing")
}

func TestPartSetTextDeleteUndo(t *testing.T) {
	s, fb, j := newTestServer(t)
	r := s.Dispatch(bg, `part signs note agent=agent2 at=mm:50,60 t="工事中 です"`)
	if r != "ok i=4 id=kc_signs_00000001" {
		t.Fatalf("part: %q", r)
	}
	if s.owners["signs"] != "agent2" {
		t.Fatal("part must claim the layer")
	}
	if fb.state.Elements[4].Text != "工事中 です" {
		t.Fatalf("text %q", fb.state.Elements[4].Text)
	}
	mustPrefix(t, s.Dispatch(bg, "settext 4 通行止め 終日 agent=agent2"), "ok settext i=4")
	if fb.last()[0].Text != "通行止め 終日" {
		t.Fatalf("settext %q", fb.last()[0].Text)
	}
	mustPrefix(t, s.Dispatch(bg, "delete 4 agent=agent2"), "ok delete i=4 id=kc_signs_00000001")
	if len(fb.state.Elements) != 4 || j.depth() != 3 {
		t.Fatalf("after delete n=%d journal=%d", len(fb.state.Elements), j.depth())
	}
	mustPrefix(t, s.Dispatch(bg, "undo agent=agent2"), "ok undo")
	if fb.last()[0].Kind != model.OpCreateShape || fb.last()[0].ObjectID != "kc_signs_00000001" {
		t.Fatalf("undo delete: %+v", fb.last())
	}
}

func TestAdoptAndClear(t *testing.T) {
	s, fb, _ := newTestServer(t)
	mustPrefix(t, s.Dispatch(bg, "adopt 1 lines agent=agent1"), "ok adopt i=1 id=h1b5d0a27ea77b246_1_0 layer=lines")
	if tg := fb.state.Elements[1].Tag; tg == nil || tg.Layer != "lines" || tg.Owner != "agent1" {
		t.Fatalf("tag %+v", tg)
	}
	mustPrefix(t, s.Dispatch(bg, "adopt 1 lines agent=agent1"), "ng adopt: i=1 already tagged")
	mustPrefix(t, s.Dispatch(bg, "move 1 1 1 agent=agent1"), "ok move")
	mustPrefix(t, s.Dispatch(bg, "clear base agent=agent1"), "ng clear")
	if r := s.Dispatch(bg, "clear lines agent=agent1"); r != "ok deleted=1" {
		t.Fatalf("clear: %q", r)
	}
	if r := s.Dispatch(bg, "clear lines agent=agent1"); r != "ok deleted=0" {
		t.Fatalf("clear empty: %q", r)
	}
}

func TestPickNotJournaled(t *testing.T) {
	s, fb, j := newTestServer(t)
	mustPrefix(t, s.Dispatch(bg, "pick 3"), "ok pick i=3 id=kc_notes_00000001")
	op := fb.last()[0]
	if op.Kind != model.OpCreateShape || op.ObjectID != CursorID || op.Tag.Layer != LayerCursor ||
		op.Box != (model.Box{X: 198, Y: 48, W: 34, H: 12}) || *op.Shape.Outline != (model.RGB{R: 0, G: 0.35, B: 1}) || op.Shape.OutlinePt != 1.5 || op.Shape.Fill != nil {
		t.Fatalf("cursor op %+v %+v", op, op.Shape)
	}
	mustPrefix(t, s.Dispatch(bg, "state"), "ok pres=PRES123 rev=r1 layers=4 pick=3")
	mustPrefix(t, s.Dispatch(bg, "pick 1"), "ok pick i=1")
	if op := fb.last()[0]; op.Kind != model.OpTransform || op.TargetID != CursorID {
		t.Fatalf("re-pick must move the cursor: %+v", op)
	}
	mustPrefix(t, s.Dispatch(bg, "unpick"), "ok unpick")
	if op := fb.last()[0]; op.Kind != model.OpDelete || op.TargetID != CursorID {
		t.Fatalf("unpick: %+v", op)
	}
	if j.depth() != 0 {
		t.Fatal("pick must not be journaled")
	}
	mustPrefix(t, s.Dispatch(bg, "state"), "ok pres=PRES123 rev=r3 layers=3 pick=-")
}

func TestPickAt(t *testing.T) {
	s, _, _ := newTestServer(t)
	if r := s.Dispatch(bg, "pickat mm:120,101"); r != "ok i=1 id=h1b5d0a27ea77b246_1_0 d=1.0mm" {
		t.Fatalf("pickat: %q", r)
	}
	mustPrefix(t, s.Dispatch(bg, "pickat mm:120,110"), "ng pickat: nothing within 3.0mm")
	mustPrefix(t, s.Dispatch(bg, "pickat mm:120,110 tol=12mm"), "ok i=1")
}

func TestViewFit(t *testing.T) {
	s, fb, j := newTestServer(t)
	r := s.Dispatch(bg, "view fit sites=1,2")
	mustPrefix(t, r, "ok bg=")
	if !strings.Contains(r, "dpi=") {
		t.Fatalf("view: %q", r)
	}
	ops := fb.last()
	if len(ops) != 7 {
		t.Fatalf("ops %d: %+v", len(ops), ops)
	}
	if ops[0].Kind != model.OpTransform || ops[0].TargetID != "p1_i4" {
		t.Fatalf("bg op %+v", ops[0])
	}
	want := map[string]model.Box{
		"kc_map_mask_top": {X: 0, Y: 0, W: 420, H: 16},
		"kc_map_mask_bot": {X: 0, Y: 220, W: 420, H: 77},
		"kc_map_mask_lft": {X: 0, Y: 16, W: 10, H: 204},
		"kc_map_mask_rgt": {X: 410, Y: 16, W: 10, H: 204},
	}
	for _, op := range ops[1:5] {
		if op.Kind != model.OpCreateShape || op.Box != want[op.ObjectID] || op.Tag.Layer != LayerMap || *op.Shape.Fill != (model.RGB{R: 1, G: 1, B: 1}) {
			t.Fatalf("mask op %+v", op)
		}
	}
	if ops[5].Kind != model.OpZOrderBack || len(ops[5].TargetIDs) != 4 || ops[6].Kind != model.OpZOrderBack || ops[6].TargetIDs[0] != "p1_i4" {
		t.Fatalf("zorder %+v %+v", ops[5], ops[6])
	}
	// 両 site の中心が枠の中心に来る
	nb := ops[0].Box
	gt := fb.state.Elements[0].Tag.Georef
	d := fakeDeps()
	var cx, cy float64
	for _, n := range []string{"1", "2"} {
		x, y := d.toMM(geoOf(gt), gbox(nb), gt.Sites[n][0], gt.Sites[n][1])
		cx, cy = cx+x/2, cy+y/2
	}
	if abs(cx-210) > 1e-6 || abs(cy-118) > 1e-6 {
		t.Fatalf("center %.3f,%.3f", cx, cy)
	}
	if j.depth() != 1 {
		t.Fatal("view must be undoable")
	}
	// 2 回目は既存マスクを動かすだけ
	mustPrefix(t, s.Dispatch(bg, "view center mm:210,118 width=100mm"), "ok bg=")
	for _, op := range fb.last()[1:5] {
		if op.Kind != model.OpTransform {
			t.Fatalf("second view must transform masks: %+v", op)
		}
	}
	mustPrefix(t, s.Dispatch(bg, "view fit sites=1"), "ng view: sites span is zero")
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func TestDRC(t *testing.T) {
	s, fb, _ := newTestServer(t)
	mustPrefix(t, s.Dispatch(bg, "drc"), "ok drc clean")
	fb.humanEdit(func(d *model.Deck) {
		d.Elements = append(d.Elements,
			model.Element{ObjectID: "legend1", Kind: "shape", Text: "① 工事中", Box: model.Box{X: 300, Y: 250, W: 40, H: 10}},
			model.Element{ObjectID: "kc_signs_a", Kind: "image", Box: model.Box{X: 50, Y: 50, W: 12, H: 12},
				Tag: &model.Tag{Layer: "signs", Owner: "agent2", Kind: "sign", Params: map[string]any{"key": "①"}}},
			model.Element{ObjectID: "kc_signs_b", Kind: "image", Box: model.Box{X: 70, Y: 50, W: 12, H: 12},
				Tag: &model.Tag{Layer: "signs", Owner: "agent2", Kind: "sign", Params: map[string]any{"key": "⑨"}}},
			model.Element{ObjectID: "kc_cones_a", Kind: "shape", Box: model.Box{X: 210, Y: 52, W: 3, H: 3},
				Tag: &model.Tag{Layer: "cones", Owner: "agent3", Kind: "cone_line"}},
			model.Element{ObjectID: "kc_notes_b", Kind: "shape", Text: "迂回", Box: model.Box{X: 410, Y: 290, W: 20, H: 8},
				Tag: &model.Tag{Layer: "notes", Owner: "agent1", Kind: "note"}},
		)
	})
	mustPrefix(t, s.Dispatch(bg, "sync"), "ok")
	r := s.Dispatch(bg, "drc")
	want := "ok [FR-05] i=6 sign key=⑨ has no legend label; [FR-08] i=3 label box overlaps cone i=7; " +
		"[D-01] i=8 box x=410.0 y=290.0 w=20.0 h=8.0 outside page 420x297"
	if r != want {
		t.Fatalf("drc:\n got %q\nwant %q", r, want)
	}
}

func TestGeorefAndCaptureAndKinds(t *testing.T) {
	s, fb, _ := newTestServer(t)
	mustPrefix(t, s.Dispatch(bg, "georef image=1 z=17 center=1,2 px=10x10"), "ng georef: i=1 is line")
	r := s.Dispatch(bg, "georef image=0 z=17 center=32.7695,130.7783 px=1280x905 site1=32.77,130.77 site2=32.771,130.771")
	if r != "ok georef image=0 z=17 center=32.769500,130.778300 px=1280x905 sites=2" {
		t.Fatalf("georef: %q", r)
	}
	g := fb.state.Elements[0].Tag.Georef
	if g.Z != 17 || g.Px != [2]int{1280, 905} || g.Sites["2"] != [2]float64{32.771, 130.771} {
		t.Fatalf("georef tag %+v", g)
	}
	mustPrefix(t, s.Dispatch(bg, "capture rel.png"), "ng capture: path must be absolute")
	abs := filepath.Join(t.TempDir(), "out.png")
	if r := s.Dispatch(bg, "capture "+abs); r != "ok saved="+abs || len(fb.thumbs) != 1 {
		t.Fatalf("capture: %q", r)
	}
	if r := s.Dispatch(bg, "kinds"); r != "ok site sign note" {
		t.Fatalf("kinds: %q", r)
	}
	r = s.Dispatch(bg, "layers")
	if r != "ok layers=3; base n=2 owner=-; map n=1 owner=-; notes n=1 owner=agent1" {
		t.Fatalf("layers: %q", r)
	}
}

func TestParseCommand(t *testing.T) {
	c, err := parseCommand("\uFEFFPart signs note agent=a t=\"a b\" at=mm:1,2 extra")
	if err != nil {
		t.Fatal(err)
	}
	if c.verb != "part" || c.kv["t"] != "a b" || c.kv["agent"] != "a" || strings.Join(c.pos, "|") != "signs|note|extra" {
		t.Fatalf("%+v", c)
	}
	if _, err := parseCommand(`settext 1 "oops`); err == nil {
		t.Fatal("unterminated quote must fail")
	}
}

// (f) TCP 往復（エフェメラルポート）。
func TestTCPRoundTrip(t *testing.T) {
	s, _, _ := newTestServer(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(bg)
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, ln) }()
	addr := ln.Addr().String()

	r, err := Send(addr, "state", 5*time.Second)
	if err != nil || !strings.HasPrefix(r, "ok pres=PRES123") {
		t.Fatalf("state: %q %v", r, err)
	}
	r, err = Send(addr, "list", 5*time.Second)
	if err != nil || !strings.HasPrefix(r, "ok n=4\n0 map") || strings.Count(r, "\n") != 4 || !strings.Contains(r, "「片側交互通行」") {
		t.Fatalf("list: %q %v", r, err)
	}
	r, _ = Send(addr, "move 1 1 1 agent=agent1", 5*time.Second)
	mustPrefix(t, r, "ng read-only")

	// 1 接続で複数行（AICAD クライアントと同じく行ごとに 1 応答）
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = conn.Write([]byte("kinds\r\nlist notes\nstate\n"))
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 1024)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for strings.Count(string(buf), "\n") < 5 {
		n, err := conn.Read(tmp)
		if err != nil {
			t.Fatalf("read: %v (%q)", err, buf)
		}
		buf = append(buf, tmp[:n]...)
	}
	got := strings.Split(string(buf), "\n")
	if got[0] != "ok site sign note" || got[1] != "ok n=1" || !strings.HasPrefix(got[2], "3 notes") || got[3] != "" || !strings.HasPrefix(got[4], "ok pres=") {
		t.Fatalf("pipelined: %q", got)
	}
	conn.Close()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not stop")
	}
}
