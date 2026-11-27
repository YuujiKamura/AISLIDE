package cp

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/YuujiKamura/AISLIDE/internal/geo"
	"github.com/YuujiKamura/AISLIDE/internal/model"
	"github.com/YuujiKamura/AISLIDE/internal/parts"
)

const (
	LayerBase   = "base"
	LayerMap    = "map"
	LayerCursor = "_cursor"
	CursorID    = "kc__cursor_pick"
)

var presURLRe = regexp.MustCompile(`/presentation/d/([A-Za-z0-9_-]+)`)

func presIDFrom(s string) string {
	if m := presURLRe.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return s
}

func (s *Server) cmdOpen(ctx context.Context, c *command) string {
	if len(c.pos) < 1 {
		return ng("usage: open <presId or URL>")
	}
	id := presIDFrom(c.pos[0])
	if err := s.load(ctx, id); err != nil {
		return ng("open: " + err.Error())
	}
	gr := "no"
	if _, _, err := s.d.mapBG(s.deck); err == nil {
		gr = "yes"
	}
	return fmt.Sprintf("ok rev=%s n=%d georef=%s", s.deck.Rev, len(s.deck.Elements), gr)
}

func (s *Server) cmdState(ctx context.Context, c *command) string {
	layers := map[string]bool{}
	for i := range s.deck.Elements {
		layers[s.elementLayer(&s.deck.Elements[i])] = true
	}
	pick := "-"
	if s.pickID != "" {
		if e, err := s.d.get(s.deck, s.pickID); err == nil {
			pick = strconv.Itoa(e.Index)
		}
	}
	return fmt.Sprintf("ok pres=%s rev=%s layers=%d pick=%s", s.presID, s.deck.Rev, len(layers), pick)
}

// isAgentLayer: 部品レイヤ（base / map / _cursor 以外）。
func isAgentLayer(l string) bool { return l != LayerBase && l != LayerMap && l != LayerCursor }

func (s *Server) cmdSync(ctx context.Context, c *command) string {
	prev := s.deck
	if err := s.reload(ctx); err != nil {
		return ng("sync: " + err.Error())
	}
	return fmt.Sprintf("ok rev=%s changed=%d", s.deck.Rev, s.countMoved(prev, s.deck))
}

// countMoved: prev にあった部品（agent レイヤ）のうち、位置・寸法が変わったか消えた数。
func (s *Server) countMoved(prev, cur *model.Deck) int {
	if prev == nil {
		return 0
	}
	now := map[string]model.Box{}
	for _, e := range cur.Elements {
		now[e.ObjectID] = e.Box
	}
	n := 0
	for i := range prev.Elements {
		e := &prev.Elements[i]
		if !isAgentLayer(s.elementLayer(e)) {
			continue
		}
		b, ok := now[e.ObjectID]
		if !ok || !boxNear(b, e.Box, 0.05) {
			n++
		}
	}
	return n
}

func boxNear(a, b model.Box, eps float64) bool {
	return math.Abs(a.X-b.X) <= eps && math.Abs(a.Y-b.Y) <= eps &&
		math.Abs(a.W-b.W) <= eps && math.Abs(a.H-b.H) <= eps
}

func kindOf(e *model.Element) string {
	if e.Tag != nil && e.Tag.Kind != "" {
		return e.Tag.Kind
	}
	return e.Kind
}

func geoOf(t *model.GeorefTag) geo.Georef {
	return geo.Georef{Z: t.Z, CenterLat: t.Center[0], CenterLon: t.Center[1], PxW: t.Px[0], PxH: t.Px[1]}
}

func gbox(b model.Box) geo.Box { return geo.Box{X: b.X, Y: b.Y, W: b.W, H: b.H} }

func (s *Server) cmdList(ctx context.Context, c *command) string {
	filter := ""
	if len(c.pos) > 0 {
		filter = c.pos[0]
	}
	bg, gt, gerr := s.d.mapBG(s.deck)
	var lines []string
	for i := range s.deck.Elements {
		e := &s.deck.Elements[i]
		l := s.elementLayer(e)
		if filter != "" && l != filter {
			continue
		}
		line := fmt.Sprintf("%d %s %s %s x=%.1f y=%.1f w=%.1f h=%.1f",
			e.Index, l, kindOf(e), e.ObjectID, e.Box.X, e.Box.Y, e.Box.W, e.Box.H)
		if gerr == nil && l != LayerMap {
			lat, lon := s.d.toLatLon(geoOf(gt), gbox(bg.Box), e.Box.X+e.Box.W/2, e.Box.Y+e.Box.H/2)
			line += fmt.Sprintf(" @%.6f,%.6f", lat, lon)
		}
		if t := shortText(e.Text, 40); t != "" {
			line += " 「" + t + "」"
		}
		lines = append(lines, line)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "ok n=%d\n", len(lines))
	for _, l := range lines {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return b.String()
}

func shortText(t string, max int) string {
	t = strings.Join(strings.Fields(t), " ")
	r := []rune(t)
	if len(r) > max {
		return string(r[:max]) + "…"
	}
	return t
}

func (s *Server) cmdLayers(ctx context.Context, c *command) string {
	count := map[string]int{}
	for i := range s.deck.Elements {
		count[s.elementLayer(&s.deck.Elements[i])]++
	}
	for l := range s.owners {
		if _, ok := count[l]; !ok {
			count[l] = 0
		}
	}
	names := make([]string, 0, len(count))
	for l := range count {
		names = append(names, l)
	}
	sort.Strings(names)
	items := make([]string, 0, len(names))
	for _, l := range names {
		owner := s.owners[l]
		if owner == "" {
			owner = "-"
		}
		items = append(items, fmt.Sprintf("%s n=%d owner=%s", l, count[l], owner))
	}
	return fmt.Sprintf("ok layers=%d; %s", len(names), strings.Join(items, "; "))
}

func (s *Server) cmdKinds(ctx context.Context, c *command) string {
	return "ok " + strings.Join(s.d.kinds(), " ")
}

// ---- 指差し（journal に積まない） ----

func (s *Server) cmdPick(ctx context.Context, c *command) string {
	if len(c.pos) < 1 {
		return ng("usage: pick <i>")
	}
	e, err := s.d.get(s.deck, c.pos[0])
	if err != nil {
		return ng("pick: " + err.Error())
	}
	if e.ObjectID == CursorID {
		return ng("pick: cannot pick the cursor itself")
	}
	idx, oid := e.Index, e.ObjectID
	box := model.Box{X: e.Box.X - 2, Y: e.Box.Y - 2, W: e.Box.W + 4, H: e.Box.H + 4}
	var op model.Op
	if _, err := s.d.get(s.deck, CursorID); err == nil {
		op = model.Op{Kind: model.OpTransform, TargetID: CursorID, Box: box}
	} else {
		op = model.Op{Kind: model.OpCreateShape, ObjectID: CursorID, Box: box,
			Shape: &model.ShapeStyle{ShapeType: "RECTANGLE", Outline: &model.RGB{R: 0, G: 0.35, B: 1}, OutlinePt: 1.5},
			Tag:   &model.Tag{Layer: LayerCursor, Kind: "cursor"}}
	}
	if r := s.write(ctx, []model.Op{op}, writeOpts{}); r != "" {
		return r
	}
	s.pickID = oid
	return fmt.Sprintf("ok pick i=%d id=%s", idx, oid)
}

func (s *Server) cmdUnpick(ctx context.Context, c *command) string {
	s.pickID = ""
	if _, err := s.d.get(s.deck, CursorID); err != nil {
		return "ok unpick"
	}
	if r := s.write(ctx, []model.Op{{Kind: model.OpDelete, TargetID: CursorID}}, writeOpts{}); r != "" {
		return r
	}
	return "ok unpick"
}

func (s *Server) cmdPickAt(ctx context.Context, c *command) string {
	if len(c.pos) < 1 {
		return ng("usage: pickat <pt> [tol=3mm]")
	}
	x, y, err := s.d.resolvePoint(s.deck, c.pos[0])
	if err != nil {
		return ng("pickat: " + err.Error())
	}
	tolS := c.kv["tol"]
	if tolS == "" {
		tolS = "3mm"
	}
	tol, err := s.d.resolveLength(s.deck, tolS)
	if err != nil {
		return ng("pickat tol: " + err.Error())
	}
	best, bestD, bestA := -1, math.Inf(1), math.Inf(1)
	for i := range s.deck.Elements {
		e := &s.deck.Elements[i]
		l := s.elementLayer(e)
		if l == LayerCursor || l == LayerMap {
			continue
		}
		d := distToBox(e.Box, x, y)
		a := e.Box.W * e.Box.H
		if d <= tol && (d < bestD || (d == bestD && a < bestA)) {
			best, bestD, bestA = i, d, a
		}
	}
	if best < 0 {
		return ng(fmt.Sprintf("pickat: nothing within %.1fmm of %.1f,%.1f", tol, x, y))
	}
	e := &s.deck.Elements[best]
	return fmt.Sprintf("ok i=%d id=%s d=%.1fmm", e.Index, e.ObjectID, bestD)
}

func distToBox(b model.Box, x, y float64) float64 {
	dx := math.Max(math.Max(b.X-x, 0), x-(b.X+b.W))
	dy := math.Max(math.Max(b.Y-y, 0), y-(b.Y+b.H))
	return math.Hypot(dx, dy)
}

// ---- 作図・編集 ----

func needAgent(c *command) (string, string) {
	a := c.agent()
	if a == "" {
		return "", ng(c.verb + ": agent=<id> required")
	}
	return a, ""
}

func (s *Server) claim(layer, agent string) error {
	if err := s.d.claim(s.deck, layer, agent); err != nil {
		return err
	}
	if s.deck.Owners == nil {
		s.deck.Owners = map[string]string{}
	}
	s.deck.Owners[layer] = agent
	s.syncOwners()
	return nil
}

func (s *Server) cmdClaim(ctx context.Context, c *command) string {
	agent, bad := needAgent(c)
	if bad != "" {
		return bad
	}
	if len(c.pos) < 1 {
		return ng("usage: claim <layer> agent=<id>")
	}
	if err := s.claim(c.pos[0], agent); err != nil {
		return ng("claim: " + err.Error())
	}
	return fmt.Sprintf("ok claim layer=%s agent=%s", c.pos[0], agent)
}

func (s *Server) cmdPart(ctx context.Context, c *command) string {
	agent, bad := needAgent(c)
	if bad != "" {
		return bad
	}
	if len(c.pos) < 2 {
		return ng("usage: part <layer> <kind> agent=<id> k=v ...")
	}
	layer, kind := c.pos[0], c.pos[1]
	if err := s.claim(layer, agent); err != nil {
		return ng("part: " + err.Error())
	}
	ops, err := s.d.build(kind, c.argsExcept("agent", "force"), parts.Ctx{Deck: s.deck, Layer: layer, Agent: agent, NewID: s.d.newID})
	if err != nil {
		return ng("part: " + err.Error())
	}
	first := ""
	for _, op := range ops {
		if op.ObjectID != "" && (op.Kind == model.OpCreateShape || op.Kind == model.OpCreateLine || op.Kind == model.OpDuplicate) {
			first = op.ObjectID
			break
		}
	}
	if r := s.write(ctx, ops, writeOpts{checkAgent: &agent, journalAs: &agent, force: c.kv["force"] == "yes"}); r != "" {
		return r
	}
	if first == "" {
		return "ok i=- id=-"
	}
	e, err := s.d.get(s.deck, first)
	if err != nil {
		return fmt.Sprintf("ok i=- id=%s", first)
	}
	return fmt.Sprintf("ok i=%d id=%s", e.Index, e.ObjectID)
}

// signedLength: "-5" + unit "mm" → -5mm を ResolveLength で。
func (s *Server) signedLength(v, unit string) (float64, error) {
	sign := 1.0
	if strings.HasPrefix(v, "-") {
		sign, v = -1, v[1:]
	} else {
		v = strings.TrimPrefix(v, "+")
	}
	if _, err := strconv.ParseFloat(v, 64); err == nil {
		v += unit
	}
	mm, err := s.d.resolveLength(s.deck, v)
	return sign * mm, err
}

// target: 書込み対象の要素（index か objectId のみ）。
func (s *Server) target(ref string) (*model.Element, string) {
	e, err := s.d.get(s.deck, ref)
	if err != nil {
		return nil, ng(fmt.Sprintf("no element %s: %v", ref, err))
	}
	return e, ""
}

func (s *Server) editOK(e *model.Element, what string) string {
	return fmt.Sprintf("ok %s i=%d id=%s", what, e.Index, e.ObjectID)
}

// editAndReport: 書込み後の index を objectId から引き直して応答。
func (s *Server) editAndReport(ctx context.Context, c *command, agent string, ops []model.Op, oid, what string) string {
	if r := s.write(ctx, ops, writeOpts{checkAgent: &agent, journalAs: &agent, force: c.kv["force"] == "yes"}); r != "" {
		return r
	}
	if e, err := s.d.get(s.deck, oid); err == nil {
		return s.editOK(e, what)
	}
	return fmt.Sprintf("ok %s id=%s", what, oid)
}

func (s *Server) cmdMove(ctx context.Context, c *command) string {
	agent, bad := needAgent(c)
	if bad != "" {
		return bad
	}
	if len(c.pos) < 3 {
		return ng("usage: move <i> <dx> <dy> [m|mm] agent=<id>")
	}
	e, bad := s.target(c.pos[0])
	if bad != "" {
		return bad
	}
	unit := "mm"
	if len(c.pos) >= 4 {
		unit = c.pos[3]
		if unit != "m" && unit != "mm" {
			return ng("move: unit must be m or mm")
		}
	}
	dx, err := s.signedLength(c.pos[1], unit)
	if err != nil {
		return ng("move dx: " + err.Error())
	}
	dy, err := s.signedLength(c.pos[2], unit)
	if err != nil {
		return ng("move dy: " + err.Error())
	}
	b := e.Box
	b.X += dx
	b.Y += dy
	op := model.Op{Kind: model.OpTransform, TargetID: e.ObjectID, Box: b}
	return s.editAndReport(ctx, c, agent, []model.Op{op}, e.ObjectID, "move")
}

func (s *Server) cmdMoveTo(ctx context.Context, c *command) string {
	agent, bad := needAgent(c)
	if bad != "" {
		return bad
	}
	if len(c.pos) < 2 {
		return ng("usage: moveto <i> <pt> agent=<id>")
	}
	e, bad := s.target(c.pos[0])
	if bad != "" {
		return bad
	}
	x, y, err := s.d.resolvePoint(s.deck, c.pos[1])
	if err != nil {
		return ng("moveto: " + err.Error())
	}
	b := e.Box
	b.X, b.Y = x-b.W/2, y-b.H/2
	op := model.Op{Kind: model.OpTransform, TargetID: e.ObjectID, Box: b}
	return s.editAndReport(ctx, c, agent, []model.Op{op}, e.ObjectID, "moveto")
}

func (s *Server) cmdSetText(ctx context.Context, c *command) string {
	agent, bad := needAgent(c)
	if bad != "" {
		return bad
	}
	if len(c.toks) < 2 {
		return ng("usage: settext <i> <text> agent=<id>")
	}
	e, bad := s.target(c.toks[0].text)
	if bad != "" {
		return bad
	}
	var words []string
	for _, t := range c.toks[1:] {
		if t.isKV && (t.key == "agent" || t.key == "force") {
			continue
		}
		words = append(words, t.text)
	}
	op := model.Op{Kind: model.OpSetText, TargetID: e.ObjectID, Text: strings.Join(words, " ")}
	return s.editAndReport(ctx, c, agent, []model.Op{op}, e.ObjectID, "settext")
}

func (s *Server) cmdDelete(ctx context.Context, c *command) string {
	agent, bad := needAgent(c)
	if bad != "" {
		return bad
	}
	if len(c.pos) < 1 {
		return ng("usage: delete <i> agent=<id>")
	}
	e, bad := s.target(c.pos[0])
	if bad != "" {
		return bad
	}
	idx, oid := e.Index, e.ObjectID
	op := model.Op{Kind: model.OpDelete, TargetID: oid}
	if r := s.write(ctx, []model.Op{op}, writeOpts{checkAgent: &agent, journalAs: &agent, force: c.kv["force"] == "yes"}); r != "" {
		return r
	}
	if s.pickID == oid {
		s.pickID = ""
	}
	return fmt.Sprintf("ok delete i=%d id=%s", idx, oid)
}

func (s *Server) cmdAdopt(ctx context.Context, c *command) string {
	agent, bad := needAgent(c)
	if bad != "" {
		return bad
	}
	if len(c.pos) < 2 {
		return ng("usage: adopt <i> <layer> agent=<id>")
	}
	e, bad := s.target(c.pos[0])
	if bad != "" {
		return bad
	}
	if e.Tag != nil {
		return ng(fmt.Sprintf("adopt: i=%d already tagged layer=%s", e.Index, e.Tag.Layer))
	}
	layer := c.pos[1]
	if !isAgentLayer(layer) {
		return ng("adopt: layer " + layer + " is reserved")
	}
	if err := s.claim(layer, agent); err != nil {
		return ng("adopt: " + err.Error())
	}
	op := model.Op{Kind: model.OpSetTag, TargetID: e.ObjectID,
		Tag: &model.Tag{Layer: layer, Owner: agent, Kind: e.Kind, Params: map[string]any{"adopted": true}}}
	oid := e.ObjectID
	if r := s.write(ctx, []model.Op{op}, writeOpts{journalAs: &agent}); r != "" {
		return r
	}
	if e, err := s.d.get(s.deck, oid); err == nil {
		return fmt.Sprintf("ok adopt i=%d id=%s layer=%s", e.Index, oid, layer)
	}
	return fmt.Sprintf("ok adopt id=%s layer=%s", oid, layer)
}

func (s *Server) cmdClear(ctx context.Context, c *command) string {
	agent, bad := needAgent(c)
	if bad != "" {
		return bad
	}
	if len(c.pos) < 1 {
		return ng("usage: clear <layer> agent=<id>")
	}
	layer := c.pos[0]
	if !isAgentLayer(layer) {
		return ng("clear: layer " + layer + " is not an agent layer")
	}
	if o := s.owners[layer]; o != "" && o != agent {
		return ng(fmt.Sprintf("clear: %v: layer=%s owner=%s", model.ErrNotOwner, layer, o))
	}
	var ops []model.Op
	for i := range s.deck.Elements {
		e := &s.deck.Elements[i]
		if s.elementLayer(e) == layer {
			ops = append(ops, model.Op{Kind: model.OpDelete, TargetID: e.ObjectID})
		}
	}
	if len(ops) == 0 {
		return "ok deleted=0"
	}
	if r := s.write(ctx, ops, writeOpts{checkAgent: &agent, journalAs: &agent, force: c.kv["force"] == "yes"}); r != "" {
		return r
	}
	return fmt.Sprintf("ok deleted=%d", len(ops))
}

func (s *Server) cmdUndo(ctx context.Context, c *command) string {
	if s.journal == nil {
		return ng("undo: no journal")
	}
	agent := c.agent()
	inv, err := s.journal.Pop(s.presID, agent)
	if err != nil {
		return ng("undo: nothing to undo: " + err.Error())
	}
	if len(inv) == 0 {
		return ng("undo: nothing to undo")
	}
	if r := s.write(ctx, inv, writeOpts{}); r != "" {
		_ = s.journal.Push(s.presID, agent, inv) // 失敗したら戻しておく
		return r
	}
	return fmt.Sprintf("ok undo ops=%d", len(inv))
}

func (s *Server) cmdCapture(ctx context.Context, c *command) string {
	if len(c.pos) < 1 {
		return ng("usage: capture <abs.png>")
	}
	p := strings.Join(c.pos, " ")
	if !filepath.IsAbs(p) {
		return ng("capture: path must be absolute")
	}
	if !strings.EqualFold(filepath.Ext(p), ".png") {
		return ng("capture: path must end with .png")
	}
	if err := s.backend.Thumbnail(ctx, s.presID, s.deck.PageID, p); err != nil {
		return ng("capture: " + err.Error())
	}
	return "ok saved=" + p
}

func parseLatLon(s string) (lat, lon float64, err error) {
	a, b, ok := strings.Cut(s, ",")
	if !ok {
		return 0, 0, fmt.Errorf("want lat,lon: %q", s)
	}
	if lat, err = strconv.ParseFloat(strings.TrimSpace(a), 64); err != nil {
		return 0, 0, err
	}
	if lon, err = strconv.ParseFloat(strings.TrimSpace(b), 64); err != nil {
		return 0, 0, err
	}
	return lat, lon, nil
}

func (s *Server) cmdGeoref(ctx context.Context, c *command) string {
	ref, zs, cs, ps := c.kv["image"], c.kv["z"], c.kv["center"], c.kv["px"]
	if ref == "" || zs == "" || cs == "" || ps == "" {
		return ng("usage: georef image=<i> z=<z> center=<lat,lon> px=<w>x<h> [site1=lat,lon ...]")
	}
	e, bad := s.target(ref)
	if bad != "" {
		return bad
	}
	if e.Kind != "image" {
		return ng(fmt.Sprintf("georef: i=%d is %s, not image", e.Index, e.Kind))
	}
	z, err := strconv.Atoi(zs)
	if err != nil || z < 0 || z > 23 {
		return ng("georef: bad z")
	}
	lat, lon, err := parseLatLon(cs)
	if err != nil {
		return ng("georef center: " + err.Error())
	}
	ws, hs, ok := strings.Cut(strings.ToLower(ps), "x")
	w, err1 := strconv.Atoi(ws)
	h, err2 := strconv.Atoi(hs)
	if !ok || err1 != nil || err2 != nil || w <= 0 || h <= 0 {
		return ng("georef: px must be <w>x<h>")
	}
	gt := &model.GeorefTag{Z: z, Center: [2]float64{lat, lon}, Px: [2]int{w, h}}
	for k, v := range c.kv {
		if n, found := strings.CutPrefix(k, "site"); found && n != "" {
			if _, err := strconv.Atoi(n); err != nil {
				continue
			}
			sl, so, err := parseLatLon(v)
			if err != nil {
				return ng("georef " + k + ": " + err.Error())
			}
			if gt.Sites == nil {
				gt.Sites = map[string][2]float64{}
			}
			gt.Sites[n] = [2]float64{sl, so}
		}
	}
	tag := &model.Tag{Layer: LayerMap, Kind: "map_bg", Georef: gt}
	if e.Tag != nil {
		cp := *e.Tag
		cp.Layer, cp.Kind, cp.Georef = LayerMap, "map_bg", gt
		tag = &cp
	}
	agent := c.agent()
	idx := e.Index
	if r := s.write(ctx, []model.Op{{Kind: model.OpSetTag, TargetID: e.ObjectID, Tag: tag}}, writeOpts{journalAs: &agent}); r != "" {
		return r
	}
	return fmt.Sprintf("ok georef image=%d z=%d center=%.6f,%.6f px=%dx%d sites=%d", idx, z, lat, lon, w, h, len(gt.Sites))
}
