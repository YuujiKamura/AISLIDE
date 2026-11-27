package cp

import (
	"context"
	"fmt"
	"strings"

	"github.com/YuujiKamura/AISLIDE/internal/model"
)

// drc v0。事実のみ（感想・助言は書かない）。
//
//	[FR-05] 置いた看板（kind=sign）の key に一致する凡例ラベル（base 要素の文字に key を含む）が無い
//	[FR-08] 文字/ラベルの箱がコーン（kind=cone/cone_line）か別のラベルと重なる（少なくとも一方が部品）
//	[D-01]  部品要素がページ外にはみ出す
func (s *Server) cmdDRC(ctx context.Context, c *command) string {
	facts := s.drc()
	if len(facts) == 0 {
		return "ok drc clean"
	}
	return "ok " + strings.Join(facts, "; ")
}

func isCone(e *model.Element) bool {
	return e.Tag != nil && (e.Tag.Kind == "cone" || e.Tag.Kind == "cone_line")
}

func overlaps(a, b model.Box) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

func (s *Server) drc() []string {
	els := s.deck.Elements
	var out []string
	layerOf := make([]string, len(els))
	for i := range els {
		layerOf[i] = s.elementLayer(&els[i])
	}
	isPart := func(i int) bool { return els[i].Tag != nil && isAgentLayer(layerOf[i]) }

	// FR-05
	seenKey := map[string]bool{}
	for i := range els {
		e := &els[i]
		if !isPart(i) || e.Tag.Kind != "sign" || e.Tag.Params == nil {
			continue
		}
		kv, ok := e.Tag.Params["key"]
		if !ok {
			continue
		}
		key := strings.TrimSpace(fmt.Sprint(kv))
		if key == "" || seenKey[key] {
			continue
		}
		seenKey[key] = true
		found := false
		for j := range els {
			if els[j].Tag == nil && strings.Contains(els[j].Text, key) {
				found = true
				break
			}
		}
		if !found {
			out = append(out, fmt.Sprintf("[FR-05] i=%d sign key=%s has no legend label", e.Index, key))
		}
	}

	// FR-08
	isLabel := func(i int) bool {
		l := layerOf[i]
		return l != LayerMap && l != LayerCursor && strings.TrimSpace(els[i].Text) != "" && !isCone(&els[i])
	}
	for i := range els {
		if !isLabel(i) {
			continue
		}
		for j := range els {
			if i == j || !(isPart(i) || isPart(j)) {
				continue
			}
			if !overlaps(els[i].Box, els[j].Box) {
				continue
			}
			switch {
			case isCone(&els[j]):
				out = append(out, fmt.Sprintf("[FR-08] i=%d label box overlaps cone i=%d", els[i].Index, els[j].Index))
			case isLabel(j) && i < j:
				out = append(out, fmt.Sprintf("[FR-08] i=%d label box overlaps label i=%d", els[i].Index, els[j].Index))
			}
		}
	}

	// D-01
	pw, ph := s.deck.PageW, s.deck.PageH
	if pw <= 0 || ph <= 0 {
		pw, ph = 420, 297
	}
	const eps = 0.01
	for i := range els {
		if !isPart(i) {
			continue
		}
		b := els[i].Box
		if b.X < -eps || b.Y < -eps || b.X+b.W > pw+eps || b.Y+b.H > ph+eps {
			out = append(out, fmt.Sprintf("[D-01] i=%d box x=%.1f y=%.1f w=%.1f h=%.1f outside page %.0fx%.0f",
				els[i].Index, b.X, b.Y, b.W, b.H, pw, ph))
		}
	}
	return out
}
