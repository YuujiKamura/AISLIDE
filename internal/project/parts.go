package project

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/YuujiKamura/AISLIDE/internal/manifest"
	"github.com/YuujiKamura/AISLIDE/internal/model"
	"github.com/YuujiKamura/AISLIDE/internal/parts"
	"github.com/YuujiKamura/AISLIDE/internal/zone"
)

// PartSignWidthMM: アンカー相対で置く看板の幅（凡例画像の縦横比を保つ）。
const PartSignWidthMM = 10.0

// IsPartUnit: Parts が責任を持つ unit（"Z1.E.予告#1" のように # を含むか、"P1" のように P で始まる）。
func IsPartUnit(u string) bool { return strings.Contains(u, "#") || strings.HasPrefix(u, "P") }

// SetOf: 部品 unit が属する配置セット名（"Z1.E.予告#1" → "Z1.E.予告"）。# が無ければ ""。
func SetOf(unit string) string {
	if i := strings.LastIndexByte(unit, '#'); i > 0 {
		return unit[:i]
	}
	return ""
}

// Parts: manifest.Parts をアンカー相対位置（zone.Resolve）に投影する（THEORY P2, P4, P8）。
//   - Part.On=false、または属するセット（SetOf）が Sets で off の部品は投影しない（→ Run が派生要素を消す）。
//   - sign: 凡例の看板画像（parts.FindLegendSign）を幅 10mm で複製（role "sign", Movable）＋
//     右肩の番号バッジ（role "badge"、看板のあるべき位置に追従）。
//   - guard: 誘導員（青丸＋A/B、role "guard", Movable）。Key が空なら "A"。
type Parts struct {
	IDFunc func(unit, role string) string // nil なら ID
}

func (Parts) Name() string          { return "parts" }
func (Parts) Owns(unit string) bool { return IsPartUnit(unit) }

func (p Parts) id(u, r string) string {
	if p.IDFunc != nil {
		return p.IDFunc(u, r)
	}
	return ID(u, r)
}

// Active: 部品が投影対象か（自身が On かつセットが off でない）。
func Active(m *manifest.Manifest, unit string) bool {
	pt, ok := m.Parts[unit]
	if !ok || !pt.On {
		return false
	}
	if s, ok := m.Sets[SetOf(unit)]; ok && !s.On {
		return false
	}
	return true
}

// LegendRune: "④" または "4" → '④'。
func LegendRune(key string) (rune, error) {
	key = strings.TrimSpace(key)
	if n, err := strconv.Atoi(key); err == nil {
		if n < 1 || n > 20 {
			return 0, fmt.Errorf("key=%s: 1..20", key)
		}
		return rune('①' + n - 1), nil
	}
	r, size := utf8.DecodeRuneInString(key)
	if size == 0 || size != len(key) || r < '①' || r > '⑳' {
		return 0, fmt.Errorf("key=%q: want ①..⑳", key)
	}
	return r, nil
}

// BadgeNumber: 看板バッジの番号。manifest の凡例が final モードなら on の項目の並び順で詰めた番号
// （THEORY P3/P8）、それ以外は元の番号（④ → 4）。
func BadgeNumber(m *manifest.Manifest, key rune) int {
	orig := int(key-'①') + 1
	if m.Legend == nil || m.Legend.Mode != "final" {
		return orig
	}
	n := 0
	for _, it := range m.Legend.Items {
		if !it.On {
			continue
		}
		n++
		if r, err := LegendRune(it.Key); err == nil && r == key {
			return n
		}
	}
	return orig
}

func circleOp(id string, cx, cy, dia float64, st model.ShapeStyle, tag *model.Tag) model.Op {
	st.ShapeType = "ELLIPSE"
	return model.Op{Kind: model.OpCreateShape, ObjectID: id, Box: model.Box{X: cx - dia/2, Y: cy - dia/2, W: dia, H: dia}, Shape: &st, Tag: tag}
}

func (p Parts) Project(m *manifest.Manifest, d *model.Deck) ([]Spec, error) {
	if m == nil || len(m.Parts) == 0 {
		return nil, nil
	}
	var specs []Spec
	var f zone.Frame
	haveFrame := false
	for _, unit := range sortedKeys(m.Parts) {
		if !Active(m, unit) {
			continue
		}
		pt := m.Parts[unit]
		if !haveFrame {
			var err error
			if f, err = zone.FrameOf(d); err != nil {
				return nil, fmt.Errorf("parts: %w", err)
			}
			haveFrame = true
		}
		x, y, err := zone.Resolve(m, f, pt)
		if err != nil {
			return nil, fmt.Errorf("part %s: %w", unit, err)
		}
		white, red, blue := parts.ColorWhite, parts.ColorRed, parts.ColorBlue
		switch pt.Kind {
		case "sign":
			r, err := LegendRune(pt.Key)
			if err != nil {
				return nil, fmt.Errorf("part %s: %w", unit, err)
			}
			src, err := parts.FindLegendSign(d, r)
			if err != nil {
				return nil, fmt.Errorf("part %s: %w", unit, err)
			}
			w := PartSignWidthMM
			h := w * src.Box.H / src.Box.W
			box := model.Box{X: x - w/2, Y: y - h/2, W: w, H: h}
			sid, bid := p.id(unit, "sign"), p.id(unit, "badge")
			bx, by, dia := box.X+box.W, box.Y, parts.SignBadgeDiaMM
			num := strconv.Itoa(BadgeNumber(m, r))
			specs = append(specs,
				Spec{ID: sid, Unit: unit, Role: "sign", Box: box, Movable: true,
					Create: []model.Op{{Kind: model.OpDuplicate, ObjectID: sid, SourceID: src.ObjectID, Box: box, Tag: derivedTag(unit, "sign")}}},
				Spec{ID: bid, Unit: unit, Role: "badge", Box: model.Box{X: bx - dia/2, Y: by - dia/2, W: dia, H: dia}, Text: num,
					Create: []model.Op{circleOp(bid, bx, by, dia, model.ShapeStyle{Fill: &red, Outline: &white, OutlinePt: 0.75,
						Text: num, FontPt: parts.SignBadgeFontPt, FontColor: white, Bold: true, AlignCenter: true}, derivedTag(unit, "badge"))}})
		case "guard":
			k := strings.ToUpper(strings.TrimSpace(pt.Key))
			if k == "" {
				k = "A"
			}
			if k != "A" && k != "B" {
				return nil, fmt.Errorf("part %s: guard key %q: A or B", unit, pt.Key)
			}
			gid, dia := p.id(unit, "guard"), parts.GuardDiaMM
			specs = append(specs, Spec{ID: gid, Unit: unit, Role: "guard", Box: model.Box{X: x - dia/2, Y: y - dia/2, W: dia, H: dia},
				Text: k, Movable: true,
				Create: []model.Op{circleOp(gid, x, y, dia, model.ShapeStyle{Fill: &blue, Text: k, FontPt: parts.GuardFontPt,
					FontColor: white, Bold: true, AlignCenter: true}, derivedTag(unit, "guard"))}})
		default:
			return nil, fmt.Errorf("part %s: kind %q not supported (sign | guard)", unit, pt.Kind)
		}
	}
	return specs, nil
}

// RecordDrift: 看板・誘導員を人が動かした分を Part.Override に足す（P4）。
func (Parts) RecordDrift(m *manifest.Manifest, dr Drift) bool {
	if dr.Role != "sign" && dr.Role != "guard" {
		return false
	}
	pt, ok := m.Parts[dr.Unit]
	if !ok {
		return false
	}
	pt.Override[0] += dr.DX
	pt.Override[1] += dr.DY
	m.Parts[dr.Unit] = pt
	return true
}

// LegendKeyByLabel: 凡例の文言に labelPart を含む項目のキー（"④"）。manifest の凡例（Items の Label）を
// 優先し、無ければデッキの凡例ラベル（下帯 y>parts.LegendLabelMinY の shape、先頭が ①..⑳）を探す。
func LegendKeyByLabel(m *manifest.Manifest, d *model.Deck, labelPart string) (string, error) {
	if m != nil && m.Legend != nil {
		for _, it := range m.Legend.Items {
			if strings.Contains(it.Label, labelPart) {
				if _, err := LegendRune(it.Key); err == nil {
					return it.Key, nil
				}
			}
		}
	}
	for i := range d.Elements {
		e := &d.Elements[i]
		if e.Kind != "shape" || e.Box.Y <= parts.LegendLabelMinY || !strings.Contains(e.Text, labelPart) {
			continue
		}
		r, _ := utf8.DecodeRuneInString(e.Text)
		if r >= '①' && r <= '⑳' {
			return string(r), nil
		}
	}
	return "", fmt.Errorf("legend label containing %q: %w", labelPart, model.ErrNotFound)
}
