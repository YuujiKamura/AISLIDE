package cp

// 区間・配置セット・部品の意図（manifest）を書き換えるコマンド（THEORY P2, P4, P5, P7, P8）。
//
//	zone adopt <i|objectId> as <Zname> [kind=closure|detour] [agent=<id>]
//	zone extend <Z.end> <±len>m [agent=<id>]          例: zone extend Z1.E 30m / zone extend Z1.E -10m
//	set apply <Zname> rule=FR-10 [agent=<id>]
//	set <Z.end.set> on|off [agent=<id>]                 例: set Z1.E.予告 off
//	part nudge <unit> <dx> <dy> [agent=<id>]            紙面 mm を Override に足す
//
// どのコマンドも 1 回の batchUpdate で「manifest の書込み Op ＋ 投影の差分 Op」を書く（undo は 1 単位）。
// 手順: (1) 現在の manifest で投影し、人が動かした Movable 要素の Drift を Override に記録（P4）、
// (2) 意図を 1 つ変える、(3) 投影し直す。Movable 要素は Run が上書きしないので、(1) で人の分を
// 取り込んだ上で、あるべき位置（アンカー＋Override）へ OpTransform で付いてこさせる。
//
// 人の線（base）は読み取り専用。zone adopt は人の線の幾何を区間として記録するだけで線には触らない。
// zone extend で区間が人の線と食い違うと、zones 投影が派生の赤線（role "line"）を重ね、応答に
// "base stale: Z1=<objectId>" を出す（古い人の線を消す・引き直すのは人の判断）。

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/YuujiKamura/AISLIDE/internal/manifest"
	"github.com/YuujiKamura/AISLIDE/internal/model"
	"github.com/YuujiKamura/AISLIDE/internal/project"
	"github.com/YuujiKamura/AISLIDE/internal/zone"
)

// manifestStore: デッキ上の manifest（スピーカーノート）の読み書き。実装は manifest I/O 側が提供し、
// マージ時に Server.zx.store へ差す。Load は manifest が無ければ (nil, nil) を返してよい。
type manifestStore interface {
	Load(d *model.Deck) (*manifest.Manifest, error)
	WriteOp(d *model.Deck, m *manifest.Manifest) (model.Op, error)
}

// projectRunner: project.Run と同じ型（テストで差し替える）。
type projectRunner func(m *manifest.Manifest, d *model.Deck, ps []project.Projector) ([]model.Op, []project.Drift, error)

// zoneExt: zone / set / part nudge コマンドの依存。ゼロ値なら run=project.Run、
// projectors={zones, parts}。store が nil のときコマンドは ng。
type zoneExt struct {
	store      manifestStore
	run        projectRunner
	projectors []project.Projector
}

// registerZoneCmds: dispatcher（cp.go の init）から呼ぶ登録口。
func registerZoneCmds(h map[string]func(*Server, context.Context, *command) string) {
	h["zone"] = (*Server).cmdZone
	h["set"] = (*Server).cmdSet
	if prev := h["part"]; prev != nil {
		h["part"] = func(s *Server, ctx context.Context, c *command) string {
			if len(c.pos) > 0 && c.pos[0] == "nudge" {
				return s.cmdPartNudge(ctx, c)
			}
			return prev(s, ctx, c)
		}
	}
}

func (s *Server) zoneEnv() zoneExt {
	e := s.zx
	if e.run == nil {
		e.run = project.Run
	}
	if e.projectors == nil {
		e.projectors = []project.Projector{project.Zones{}, project.Parts{}}
	}
	return e
}

// manifestEdit: (1) drift 記録 → (2) mutate → (3) 投影 → manifest Op と差分 Op を 1 回で書く。
// mutate は応答の本文（"ok " の後）を返す。
func (s *Server) manifestEdit(ctx context.Context, c *command, mutate func(m *manifest.Manifest) (string, error)) string {
	env := s.zoneEnv()
	if env.store == nil {
		return ng(c.verb + ": manifest store not wired")
	}
	m, err := env.store.Load(s.deck)
	if err != nil {
		return ng(c.verb + ": manifest: " + err.Error())
	}
	if m == nil {
		m = &manifest.Manifest{Version: 1}
	}
	// (1) 人の手直しを先に意図へ取り込む（区間を変えた後に測ると、アンカーの移動と混ざる）。
	_, drifts, err := env.run(m, s.deck, env.projectors)
	if err != nil {
		return ng(c.verb + ": project (before): " + err.Error())
	}
	recorded := recordDrifts(m, drifts, env.projectors)
	// (2)
	msg, err := mutate(m)
	if err != nil {
		return ng(c.verb + ": " + err.Error())
	}
	// (3)
	ops, _, err := env.run(m, s.deck, env.projectors)
	if err != nil {
		return ng(c.verb + ": project: " + err.Error())
	}
	follow, err := followMovables(m, s.deck, env.projectors, ops)
	if err != nil {
		return ng(c.verb + ": project: " + err.Error())
	}
	mop, err := env.store.WriteOp(s.deck, m)
	if err != nil {
		return ng(c.verb + ": manifest write: " + err.Error())
	}
	all := append(append([]model.Op{mop}, ops...), follow...)
	agent := c.agent()
	if r := s.write(ctx, all, writeOpts{journalAs: &agent, force: c.kv["force"] == "yes"}); r != "" {
		return r
	}
	out := "ok " + msg + fmt.Sprintf(" ops=%d", len(all)-1)
	if recorded > 0 {
		out += fmt.Sprintf(" drift=%d", recorded)
	}
	if st := project.StaleBases(m, s.deck); len(st) > 0 {
		out += "; base stale: " + strings.Join(st, ",") + " (derived overlay drawn; base untouched)"
	}
	return out
}

// recordDrifts: Drift を、その unit を持つ DriftRecorder に記録した数。
func recordDrifts(m *manifest.Manifest, ds []project.Drift, ps []project.Projector) int {
	n := 0
	for _, dr := range ds {
		for _, p := range ps {
			if r, ok := p.(project.DriftRecorder); ok && p.Owns(dr.Unit) && r.RecordDrift(m, dr) {
				n++
				break
			}
		}
	}
	return n
}

// followMovables: Movable な派生要素があるべき位置と 0.1mm 超ずれていれば OpTransform（ops が既に
// 同じ要素を動かしていれば出さない）。Drift は (1) で Override に取り込み済みなので、ここでのずれは
// アンカー側の変化（区間の延長など）によるもの。
func followMovables(m *manifest.Manifest, d *model.Deck, ps []project.Projector, ops []model.Op) ([]model.Op, error) {
	moved := map[string]bool{}
	for _, op := range ops {
		if op.Kind == model.OpTransform {
			moved[op.TargetID] = true
		}
	}
	var out []model.Op
	for _, p := range ps {
		specs, err := p.Project(m, d)
		if err != nil {
			return nil, err
		}
		for _, sp := range specs {
			if !sp.Movable || moved[sp.ID] {
				continue
			}
			e, err := d.Get(sp.ID)
			if err != nil || boxNear(e.Box, sp.Box, 0.1) {
				continue
			}
			out = append(out, model.Op{Kind: model.OpTransform, TargetID: sp.ID, Box: sp.Box})
			moved[sp.ID] = true
		}
	}
	return out, nil
}

var zoneNameRe = regexp.MustCompile(`^Z[A-Za-z0-9_]{1,15}$`)

func (s *Server) cmdZone(ctx context.Context, c *command) string {
	if len(c.pos) < 1 {
		return ng("usage: zone adopt <i> as <Zname> [kind=closure] | zone extend <Z.end> <±len>m")
	}
	switch c.pos[0] {
	case "adopt":
		return s.cmdZoneAdopt(ctx, c)
	case "extend":
		return s.cmdZoneExtend(ctx, c)
	}
	return ng("zone: unknown subcommand " + c.pos[0] + " (adopt | extend)")
}

func (s *Server) cmdZoneAdopt(ctx context.Context, c *command) string {
	if len(c.pos) != 4 || c.pos[2] != "as" {
		return ng("usage: zone adopt <i|objectId> as <Zname> [kind=closure|detour]")
	}
	e, bad := s.target(c.pos[1])
	if bad != "" {
		return bad
	}
	if e.Kind != "line" || s.elementLayer(e) != LayerBase {
		return ng(fmt.Sprintf("zone adopt: i=%d is %s/%s; want a human-drawn base line", e.Index, s.elementLayer(e), e.Kind))
	}
	name := c.pos[3]
	if !zoneNameRe.MatchString(name) {
		return ng("zone adopt: zone name must be Z<1..15 of [A-Za-z0-9_]> (e.g. Z1)")
	}
	kind := c.kv["kind"]
	if kind == "" {
		kind = "closure"
	}
	if kind != "closure" && kind != "detour" {
		return ng("zone adopt: kind=closure | detour")
	}
	f, err := zone.FrameOf(s.deck)
	if err != nil {
		return ng("zone adopt: " + err.Error())
	}
	x1, y1, x2, y2 := e.LineEnds()
	path := []zone.LL{f.LL(x1, y1), f.LL(x2, y2)}
	oid := e.ObjectID
	return s.manifestEdit(ctx, c, func(m *manifest.Manifest) (string, error) {
		if _, ok := m.Zones[name]; ok {
			return "", fmt.Errorf("zone %s already exists", name)
		}
		for zn, z := range m.Zones {
			if z.From == oid {
				return "", fmt.Errorf("%s is already adopted as %s", oid, zn)
			}
		}
		if m.Zones == nil {
			m.Zones = map[string]manifest.Zone{}
		}
		z := manifest.Zone{Kind: kind, Path: path, Ends: zone.EndNames(path), From: oid}
		m.Zones[name] = z
		return fmt.Sprintf("zone adopt %s from=%s kind=%s len=%.1fm ends=%s", name, oid, kind,
			zone.Length(f, z.Path), endsString(z.Ends)), nil
	})
}

func endsString(ends map[string]int) string {
	var b strings.Builder
	for _, k := range []string{"E", "W", "N", "S"} {
		if _, ok := ends[k]; ok {
			b.WriteString(k)
		}
	}
	return b.String()
}

// parseMeters: "30m" | "+30m" | "-10m"（実 m のみ。mm は区間の長さには使えない）。
func parseMeters(s string) (float64, error) {
	if strings.HasSuffix(s, "mm") || !strings.HasSuffix(s, "m") {
		return 0, fmt.Errorf("length %q: real meters only (e.g. 30m, -10m)", s)
	}
	v, err := strconv.ParseFloat(strings.TrimSuffix(s, "m"), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, fmt.Errorf("length %q: bad number", s)
	}
	return v, nil
}

func (s *Server) cmdZoneExtend(ctx context.Context, c *command) string {
	if len(c.pos) != 3 {
		return ng("usage: zone extend <Z.end> <±len>m (e.g. zone extend Z1.E 30m)")
	}
	zn, end, ok := zone.SplitEnd(c.pos[1])
	if !ok {
		return ng("zone extend: want <Zname>.<E|W|N|S>, got " + c.pos[1])
	}
	dm, err := parseMeters(c.pos[2])
	if err != nil {
		return ng("zone extend: " + err.Error())
	}
	f, err := zone.FrameOf(s.deck)
	if err != nil {
		return ng("zone extend: " + err.Error())
	}
	return s.manifestEdit(ctx, c, func(m *manifest.Manifest) (string, error) {
		z, ok := m.Zones[zn]
		if !ok {
			return "", fmt.Errorf("zone %s: %w", zn, model.ErrNotFound)
		}
		ei, err := zone.EndIndex(z, end)
		if err != nil {
			return "", err
		}
		before := zone.Length(f, z.Path)
		np, err := zone.Extend(f, z.Path, ei != 0, dm)
		if err != nil {
			return "", err
		}
		z.Ends = zone.RemapEnds(z.Ends, len(z.Path), len(np))
		z.Path = np
		m.Zones[zn] = z
		after := zone.Length(f, np)
		return fmt.Sprintf("zone extend %s.%s %+.1fm len=%.1fm (was %.1fm)", zn, end, after-before, after, before), nil
	})
}

func (s *Server) cmdSet(ctx context.Context, c *command) string {
	if len(c.pos) >= 1 && c.pos[0] == "apply" {
		if len(c.pos) != 2 || c.kv["rule"] == "" {
			return ng("usage: set apply <Zname> rule=<" + strings.Join(zone.RuleIDs(), "|") + ">")
		}
		zn, rule := c.pos[1], c.kv["rule"]
		return s.manifestEdit(ctx, c, func(m *manifest.Manifest) (string, error) {
			lookup := func(label string) (string, error) { return project.LegendKeyByLabel(m, s.deck, label) }
			sets, err := zone.ApplyRule(m, zn, rule, lookup)
			if err != nil {
				return "", err
			}
			var units []string
			for _, st := range sets {
				for _, u := range sortedPartUnits(m, st) {
					units = append(units, u+"="+m.Parts[u].Key)
				}
			}
			return fmt.Sprintf("set apply %s rule=%s sets=%s parts=%s", zn, rule, strings.Join(sets, ","), strings.Join(units, ",")), nil
		})
	}
	if len(c.pos) != 2 || (c.pos[1] != "on" && c.pos[1] != "off") {
		return ng("usage: set <Z.end.set> on|off | set apply <Zname> rule=<id>")
	}
	name, on := c.pos[0], c.pos[1] == "on"
	return s.manifestEdit(ctx, c, func(m *manifest.Manifest) (string, error) {
		sw, ok := m.Sets[name]
		if !ok {
			return "", fmt.Errorf("set %s: %w", name, model.ErrNotFound)
		}
		sw.On = on
		m.Sets[name] = sw
		return fmt.Sprintf("set %s %s parts=%d", name, c.pos[1], len(sortedPartUnits(m, name))), nil
	})
}

func sortedPartUnits(m *manifest.Manifest, set string) []string {
	var us []string
	for u := range m.Parts {
		if project.SetOf(u) == set {
			us = append(us, u)
		}
	}
	sort.Strings(us)
	return us
}

// parseMM: "3" | "3mm" | "-2.5mm"（紙面 mm）。
func parseMM(s string) (float64, error) {
	v, err := strconv.ParseFloat(strings.TrimSuffix(s, "mm"), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, fmt.Errorf("%q: want paper mm (e.g. 3 or -2.5mm)", s)
	}
	return v, nil
}

func (s *Server) cmdPartNudge(ctx context.Context, c *command) string {
	if len(c.pos) != 4 {
		return ng("usage: part nudge <unit> <dx> <dy> (paper mm)")
	}
	unit := c.pos[1]
	dx, err := parseMM(c.pos[2])
	if err != nil {
		return ng("part nudge dx: " + err.Error())
	}
	dy, err := parseMM(c.pos[3])
	if err != nil {
		return ng("part nudge dy: " + err.Error())
	}
	return s.manifestEdit(ctx, c, func(m *manifest.Manifest) (string, error) {
		p, ok := m.Parts[unit]
		if !ok {
			return "", fmt.Errorf("part %s: %w", unit, model.ErrNotFound)
		}
		p.Override[0] += dx
		p.Override[1] += dy
		m.Parts[unit] = p
		return fmt.Sprintf("part nudge %s override=%.1f,%.1f", unit, p.Override[0], p.Override[1]), nil
	})
}
