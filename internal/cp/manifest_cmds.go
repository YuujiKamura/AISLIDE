package cp

// 意図（manifest、THEORY P1）と投影（internal/project）のコマンド。
//
//	manifest show | manifest init
//	legend adopt | legend off <key> | legend on <key> | legend mode draft|final
//	project | units
//
// 書込みはすべて「ノートの manifest 更新 + 全 Projector の差分」を 1 回の batchUpdate（requiredRev）で送り、
// journal に積む（undo で両方まとめて戻る）。派生層（derived）を書くのはここ（エンジン）だけ。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/YuujiKamura/AISLIDE/internal/manifest"
	"github.com/YuujiKamura/AISLIDE/internal/model"
	"github.com/YuujiKamura/AISLIDE/internal/project"
)

var _ = registerHandlers(map[string]handlerFunc{
	"manifest": (*Server).cmdManifest,
	"legend":   (*Server).cmdLegend,
	"project":  (*Server).cmdProject,
	"units":    (*Server).cmdUnits,
})

// readManifest: デッキのノートの manifest（無ければ nil）と残りのノート文字。
func (s *Server) readManifest() (*manifest.Manifest, string, string) {
	m, rest, err := manifest.FromDeck(s.deck)
	if err != nil {
		return nil, "", ng("manifest: " + err.Error())
	}
	return m, rest, ""
}

// commitIntent: mutate で manifest を書き換え（nil なら書き換えない＝再投影のみ）、
// ノート更新と投影差分を 1 batch で書く。manifest が無ければ空の v1 から始める。
func (s *Server) commitIntent(ctx context.Context, c *command, mutate func(m *manifest.Manifest) error) (int, []project.Drift, string) {
	m, rest, bad := s.readManifest()
	if bad != "" {
		return 0, nil, bad
	}
	var ops []model.Op
	if mutate != nil {
		if m == nil {
			m = manifest.New()
		}
		if err := mutate(m); err != nil {
			return 0, nil, ng(c.verb + ": " + err.Error())
		}
		op, changed, err := manifest.WriteOp(s.deck, m, rest)
		if err != nil {
			return 0, nil, ng("manifest: " + err.Error())
		}
		if changed {
			ops = append(ops, op)
		}
	}
	pops, drifts, err := project.Run(m, s.deck, project.Default())
	if err != nil {
		return 0, nil, ng(err.Error())
	}
	ops = append(ops, pops...)
	agent := c.agent()
	if r := s.write(ctx, ops, writeOpts{journalAs: &agent, force: c.kv["force"] == "yes"}); r != "" {
		return 0, nil, r
	}
	return len(ops), drifts, ""
}

func jsonLine(v any) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return strings.TrimRight(b.String(), "\n")
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (s *Server) cmdManifest(ctx context.Context, c *command) string {
	sub := ""
	if len(c.pos) > 0 {
		sub = c.pos[0]
	}
	switch sub {
	case "show":
		m, _, bad := s.readManifest()
		if bad != "" {
			return bad
		}
		if m == nil {
			return `ok {"manifest":false}`
		}
		type legendSum struct {
			Mode  string   `json:"mode"`
			Items int      `json:"items"`
			Off   []string `json:"off"`
		}
		sum := struct {
			Manifest bool       `json:"manifest"`
			Version  int        `json:"version"`
			Legend   *legendSum `json:"legend,omitempty"`
			Sites    []string   `json:"sites"`
			Zones    []string   `json:"zones"`
			Parts    []string   `json:"parts"`
			Sets     []string   `json:"sets"`
		}{Manifest: true, Version: m.Version, Sites: sortedKeys(m.Sites), Zones: sortedKeys(m.Zones),
			Parts: sortedKeys(m.Parts), Sets: sortedKeys(m.Sets)}
		if lg := m.Legend; lg != nil {
			ls := &legendSum{Mode: lg.Mode, Items: len(lg.Items), Off: []string{}}
			for _, it := range lg.Items {
				if !it.On {
					ls.Off = append(ls.Off, it.Key)
				}
			}
			sum.Legend = ls
		}
		return "ok " + jsonLine(sum)
	case "init":
		m, _, bad := s.readManifest()
		if bad != "" {
			return bad
		}
		if m != nil {
			return "ok manifest=exists ops=0"
		}
		n, _, bad := s.commitIntent(ctx, c, func(*manifest.Manifest) error { return nil })
		if bad != "" {
			return bad
		}
		return fmt.Sprintf("ok manifest=created ops=%d", n)
	}
	return ng("usage: manifest show|init")
}

func (s *Server) cmdLegend(ctx context.Context, c *command) string {
	if len(c.pos) < 1 {
		return ng("usage: legend adopt | legend off|on <key> | legend mode draft|final")
	}
	switch sub := c.pos[0]; sub {
	case "adopt":
		var lg *manifest.Legend
		n, _, bad := s.commitIntent(ctx, c, func(m *manifest.Manifest) error {
			var err error
			lg, err = project.AdoptLegend(s.deck, m.Legend)
			m.Legend = lg
			return err
		})
		if bad != "" {
			return bad
		}
		keys := ""
		for _, it := range lg.Items {
			keys += it.Key
		}
		return fmt.Sprintf("ok legend items=%d keys=%s mode=%s ops=%d", len(lg.Items), keys, lg.Mode, n)
	case "off", "on":
		if len(c.pos) < 2 {
			return ng("usage: legend " + sub + " <key ①..⑳>")
		}
		key, err := project.NormalizeKey(c.pos[1])
		if err != nil {
			return ng("legend: " + err.Error())
		}
		n, _, bad := s.commitIntent(ctx, c, func(m *manifest.Manifest) error { return project.SetLegendOn(m, key, sub == "on") })
		if bad != "" {
			return bad
		}
		return fmt.Sprintf("ok legend %s %s ops=%d", key, sub, n)
	case "mode":
		if len(c.pos) < 2 {
			return ng("usage: legend mode draft|final")
		}
		mode := c.pos[1]
		n, _, bad := s.commitIntent(ctx, c, func(m *manifest.Manifest) error { return project.SetLegendMode(m, mode) })
		if bad != "" {
			return bad
		}
		return fmt.Sprintf("ok legend mode=%s ops=%d", mode, n)
	}
	return ng("usage: legend adopt | legend off|on <key> | legend mode draft|final")
}

// cmdProject: manifest は変えずに全 Projector を走らせ直す（人が派生物をいじった分を戻す）。
// Movable な派生要素のずれは戻さず drift として報告する。
func (s *Server) cmdProject(ctx context.Context, c *command) string {
	n, drifts, bad := s.commitIntent(ctx, c, nil)
	if bad != "" {
		return bad
	}
	out := fmt.Sprintf("ok ops=%d drift=%d", n, len(drifts))
	for _, d := range drifts {
		out += fmt.Sprintf("; %s/%s dx=%.1f dy=%.1f", d.Unit, d.Role, d.DX, d.DY)
	}
	return out
}

// cmdUnits: manifest の名前付き単位（THEORY P5）の一覧。
func (s *Server) cmdUnits(ctx context.Context, c *command) string {
	m, _, bad := s.readManifest()
	if bad != "" {
		return bad
	}
	if m == nil {
		return "ok n=0 manifest=none"
	}
	var items []string
	if lg := m.Legend; lg != nil {
		items = append(items, "legend mode="+lg.Mode)
		for _, it := range lg.Items {
			st := "off"
			if it.On {
				st = "on"
			}
			items = append(items, fmt.Sprintf("%s %s %s", project.LegendUnit(it.Key), st, it.Label))
		}
	}
	for _, k := range sortedKeys(m.Sites) {
		items = append(items, fmt.Sprintf("%s site n=%d", k, m.Sites[k].N))
	}
	for _, k := range sortedKeys(m.Zones) {
		items = append(items, fmt.Sprintf("%s zone %s", k, m.Zones[k].Kind))
	}
	for _, k := range sortedKeys(m.Sets) {
		st := "off"
		if m.Sets[k].On {
			st = "on"
		}
		items = append(items, fmt.Sprintf("%s set %s", k, st))
	}
	for _, k := range sortedKeys(m.Parts) {
		items = append(items, fmt.Sprintf("%s part %s anchor=%s", k, m.Parts[k].Kind, m.Parts[k].Anchor))
	}
	if len(items) == 0 {
		return "ok n=0"
	}
	return fmt.Sprintf("ok n=%d; %s", len(items), strings.Join(items, "; "))
}
