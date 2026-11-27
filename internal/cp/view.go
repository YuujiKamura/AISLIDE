package cp

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/YuujiKamura/AISLIDE/internal/model"
)

// 地図枠（紙面 mm）。背景写真はこの枠に見える範囲を合わせ、枠外は白マスクで隠す。
var MapFrame = model.Box{X: 10, Y: 16, W: 400, H: 204}

const (
	DefaultMargin = 1.3
	MinPrintDPI   = 120.0
)

var maskSides = []string{"top", "bot", "lft", "rgt"}

// maskBoxes: 枠の外側 4 枚（ページ全体を覆う）。
func maskBoxes(pw, ph float64) map[string]model.Box {
	f := MapFrame
	return map[string]model.Box{
		"top": {X: 0, Y: 0, W: pw, H: f.Y},
		"bot": {X: 0, Y: f.Y + f.H, W: pw, H: ph - (f.Y + f.H)},
		"lft": {X: 0, Y: f.Y, W: f.X, H: f.H},
		"rgt": {X: f.X + f.W, Y: f.Y, W: pw - (f.X + f.W), H: f.H},
	}
}

// fitBox: 現在の背景 bg 上の点 (cx,cy)（紙面 mm）を枠の中心に、現在 mm で span 幅を枠幅に合わせた新しい bg の Box。
func fitBox(bg model.Box, cx, cy, span float64) model.Box {
	k := MapFrame.W / span
	nb := model.Box{W: bg.W * k, H: bg.H * k}
	u := (cx - bg.X) / bg.W
	v := (cy - bg.Y) / bg.H
	nb.X = MapFrame.X + MapFrame.W/2 - u*nb.W
	nb.Y = MapFrame.Y + MapFrame.H/2 - v*nb.H
	return nb
}

func (s *Server) cmdView(ctx context.Context, c *command) string {
	if len(c.pos) < 1 {
		return ng("usage: view fit sites=1,2,3 [margin=1.3] | view center <pt> width=<len>")
	}
	bg, gt, err := s.d.mapBG(s.deck)
	if err != nil {
		return ng("view: no georef background: " + err.Error())
	}
	if bg.Box.W <= 0 || bg.Box.H <= 0 {
		return ng("view: background has no size")
	}
	margin := DefaultMargin
	if m := c.kv["margin"]; m != "" {
		if margin, err = strconv.ParseFloat(m, 64); err != nil || margin <= 0 {
			return ng("view: bad margin")
		}
	}
	var cx, cy, span float64
	switch c.pos[0] {
	case "fit":
		list := c.kv["sites"]
		if list == "" {
			return ng("usage: view fit sites=1,2,3 [margin=1.3]")
		}
		minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
		for _, n := range strings.Split(list, ",") {
			x, y, err := s.d.resolvePoint(s.deck, "site:"+strings.TrimSpace(n))
			if err != nil {
				return ng("view: site " + n + ": " + err.Error())
			}
			minX, maxX = math.Min(minX, x), math.Max(maxX, x)
			minY, maxY = math.Min(minY, y), math.Max(maxY, y)
		}
		cx, cy = (minX+maxX)/2, (minY+maxY)/2
		span = math.Max(maxX-minX, (maxY-minY)*MapFrame.W/MapFrame.H) * margin
		if span <= 1e-6 {
			return ng("view: sites span is zero; use view center <pt> width=<len>")
		}
	case "center":
		if len(c.pos) < 2 || c.kv["width"] == "" {
			return ng("usage: view center <pt> width=<len>")
		}
		if cx, cy, err = s.d.resolvePoint(s.deck, c.pos[1]); err != nil {
			return ng("view: " + err.Error())
		}
		if span, err = s.d.resolveLength(s.deck, c.kv["width"]); err != nil {
			return ng("view width: " + err.Error())
		}
		if c.kv["margin"] != "" {
			span *= margin
		}
		if span <= 1e-6 {
			return ng("view: width must be > 0")
		}
	default:
		return ng("view: want fit or center")
	}

	nb := fitBox(bg.Box, cx, cy, span)
	ops := []model.Op{{Kind: model.OpTransform, TargetID: bg.ObjectID, Box: nb}}
	pw, ph := s.deck.PageW, s.deck.PageH
	if pw <= 0 || ph <= 0 {
		pw, ph = 420, 297
	}
	boxes := maskBoxes(pw, ph)
	var maskIDs []string
	for _, side := range maskSides {
		id := "kc_map_mask_" + side
		if _, err := s.d.get(s.deck, id); err != nil {
			if _, err := s.d.get(s.deck, "kl_mask_"+side); err == nil {
				id = "kl_mask_" + side // 旧試作のマスクを流用
			} else {
				id = ""
			}
		}
		if id != "" {
			ops = append(ops, model.Op{Kind: model.OpTransform, TargetID: id, Box: boxes[side]})
		} else {
			id = "kc_map_mask_" + side
			ops = append(ops, model.Op{Kind: model.OpCreateShape, ObjectID: id, Box: boxes[side],
				Shape: &model.ShapeStyle{ShapeType: "RECTANGLE", Fill: &model.RGB{R: 1, G: 1, B: 1}},
				Tag:   &model.Tag{Layer: LayerMap, Kind: "map_mask"}})
		}
		maskIDs = append(maskIDs, id)
	}
	// マスクを最背面へ → 背景をさらに最背面へ：背景 < マスク < その他
	ops = append(ops,
		model.Op{Kind: model.OpZOrderBack, TargetIDs: maskIDs},
		model.Op{Kind: model.OpZOrderBack, TargetIDs: []string{bg.ObjectID}})

	agent := c.agent()
	if r := s.write(ctx, ops, writeOpts{journalAs: &agent}); r != "" {
		return r
	}
	dpi := float64(gt.Px[0]) / (nb.W / 25.4)
	out := fmt.Sprintf("ok bg=%.1f,%.1f,%.1f,%.1f dpi=%.0f", nb.X, nb.Y, nb.W, nb.H, dpi)
	if dpi < MinPrintDPI {
		out += fmt.Sprintf(" warn=dpi<%.0f", MinPrintDPI)
	}
	return out
}
