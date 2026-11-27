package project

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/YuujiKamura/AISLIDE/internal/manifest"
	"github.com/YuujiKamura/AISLIDE/internal/model"
	"github.com/YuujiKamura/AISLIDE/internal/parts"
)

// 凡例の取捨（THEORY P8）。
//
// 凡例の看板画像とラベルは base 要素のまま（作り直さない）。投影するのは:
//   - draft: off の項目の看板画像に赤の平行斜線 2 本（role strike1 / strike2）。ラベルは元の文字。
//   - final: off の項目の画像＋ラベルを白い矩形（role mask）で覆い、on の項目のラベル番号を
//     ①②③… と詰める（base ラベルへの OpSetText。元の文字は Key+Label として manifest にある）。
//     v1 では項目の位置は詰めないので、覆った所は空欄として残る。
//
// unit 名は "legend.<Key>"（例 "legend.③"）。
const (
	ModeDraft = "draft"
	ModeFinal = "final"

	RoleStrike1 = "strike1"
	RoleStrike2 = "strike2"
	RoleMask    = "mask"

	StrikePt     = 2.0
	strikePad    = 1.0  // 斜線は画像の箱をこれだけ広げた範囲に引く（mm）
	strikeOffset = 0.15 // 2 本の斜線の縦方向のずらし（箱の高さに対する比）
)

var (
	StrikeColor = model.RGB{R: 0.85}
	MaskColor   = model.RGB{R: 1, G: 1, B: 1}
)

// Legend: 凡例の Projector。
type Legend struct{}

func (Legend) Name() string { return "legend" }

func (Legend) Owns(unit string) bool { return unit == "legend" || strings.HasPrefix(unit, "legend.") }

// LegendUnit: 項目の unit 名。
func LegendUnit(key string) string { return "legend." + key }

// Circled: 1..20 → ①..⑳。
func Circled(n int) (string, error) {
	if n < 1 || n > 20 {
		return "", fmt.Errorf("legend number %d: 1..20", n)
	}
	return string(rune('①' + n - 1)), nil
}

// NormalizeKey: "③" または "3" → "③"。それ以外は error。
func NormalizeKey(key string) (string, error) {
	key = strings.TrimSpace(key)
	if n, err := strconv.Atoi(key); err == nil {
		return Circled(n)
	}
	r, size := utf8.DecodeRuneInString(key)
	if size == 0 || size != len(key) || r < '①' || r > '⑳' {
		return "", fmt.Errorf("legend key %q: want ①..⑳ (or 1..20)", key)
	}
	return key, nil
}

func leadingKey(text string) (string, bool) {
	r, size := utf8.DecodeRuneInString(text)
	if size == 0 || r < '①' || r > '⑳' {
		return "", false
	}
	return text[:size], true
}

func union(a, b model.Box) model.Box {
	x1, y1 := min(a.X, b.X), min(a.Y, b.Y)
	x2, y2 := max(a.X+a.W, b.X+b.W), max(a.Y+a.H, b.Y+b.H)
	return model.Box{X: x1, Y: y1, W: x2 - x1, H: y2 - y1}
}

func (Legend) Project(m *manifest.Manifest, d *model.Deck) ([]Spec, error) {
	lg := m.Legend
	if lg == nil {
		return nil, nil
	}
	if lg.Mode != ModeDraft && lg.Mode != ModeFinal {
		return nil, fmt.Errorf("legend mode %q: want draft|final", lg.Mode)
	}
	var specs []Spec
	rank := 0
	for _, it := range lg.Items {
		unit := LegendUnit(it.Key)
		img, err := d.Get(it.ImageID)
		if err != nil {
			return nil, fmt.Errorf("legend %s image: %w (re-run legend adopt)", it.Key, err)
		}
		lbl, err := d.Get(it.LabelID)
		if err != nil {
			return nil, fmt.Errorf("legend %s label: %w (re-run legend adopt)", it.Key, err)
		}
		text := it.Key + it.Label
		if it.On {
			rank++
			if lg.Mode == ModeFinal {
				c, err := Circled(rank)
				if err != nil {
					return nil, err
				}
				text = c + it.Label
			}
		}
		specs = append(specs, Spec{ID: lbl.ObjectID, Unit: unit, Role: "label", Text: text, Base: true})
		if it.On {
			continue
		}
		if lg.Mode == ModeDraft {
			specs = append(specs, strikeSpecs(unit, img.Box)...)
		} else {
			specs = append(specs, maskSpec(unit, union(img.Box, lbl.Box)))
		}
	}
	return specs, nil
}

// strikeSpecs: 画像の箱に左下→右上の平行斜線 2 本（人が手で引いた取消線に倣う）。
func strikeSpecs(unit string, img model.Box) []Spec {
	b := model.Box{X: img.X - strikePad, Y: img.Y - strikePad, W: img.W + 2*strikePad, H: img.H + 2*strikePad}
	g := b.H * strikeOffset
	lines := []struct {
		role           string
		x1, y1, x2, y2 float64
	}{
		{RoleStrike1, b.X, b.Y + b.H - g, b.X + b.W, b.Y},
		{RoleStrike2, b.X, b.Y + b.H, b.X + b.W, b.Y + g},
	}
	out := make([]Spec, 0, len(lines))
	for _, l := range lines {
		id := ID(unit, l.role)
		out = append(out, Spec{
			ID: id, Unit: unit, Role: l.role,
			Box: model.Box{X: min(l.x1, l.x2), Y: min(l.y1, l.y2), W: abs(l.x2 - l.x1), H: abs(l.y2 - l.y1)},
			Create: []model.Op{{Kind: model.OpCreateLine, ObjectID: id, X1: l.x1, Y1: l.y1, X2: l.x2, Y2: l.y2,
				Line: &model.LineStyle{Color: StrikeColor, Weight: StrikePt, Dash: "SOLID"},
				Tag:  DerivedTag(unit, l.role)}},
		})
	}
	return out
}

func maskSpec(unit string, box model.Box) Spec {
	id := ID(unit, RoleMask)
	white := MaskColor
	return Spec{ID: id, Unit: unit, Role: RoleMask, Box: box,
		Create: []model.Op{{Kind: model.OpCreateShape, ObjectID: id, Box: box,
			Shape: &model.ShapeStyle{ShapeType: "RECTANGLE", Fill: &white},
			Tag:   DerivedTag(unit, RoleMask)}}}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// ErrFinalMode: final（番号詰め済み）のままでは凡例を取り込み直せない。
var ErrFinalMode = errors.New("legend is in final mode (labels renumbered); run legend mode draft first")

// AdoptLegend: デッキの凡例（下帯 y>220mm の base ラベルで ①..⑳ で始まるもの＋真上の看板画像）から
// manifest.Legend を作る。prev があれば項目ごとの on/off は Key で引き継ぐ（新しい項目は on）。
func AdoptLegend(d *model.Deck, prev *manifest.Legend) (*manifest.Legend, error) {
	if prev != nil && prev.Mode == ModeFinal {
		return nil, ErrFinalMode
	}
	on := map[string]bool{}
	if prev != nil {
		for _, it := range prev.Items {
			on[it.Key] = it.On
		}
	}
	lg := &manifest.Legend{Mode: ModeDraft}
	seen := map[string]string{}
	for i := range d.Elements {
		e := &d.Elements[i]
		if e.Kind != "shape" || e.Tag != nil || e.Box.Y <= parts.LegendLabelMinY {
			continue
		}
		key, ok := leadingKey(e.Text)
		if !ok {
			continue
		}
		if other, dup := seen[key]; dup {
			return nil, fmt.Errorf("legend %s: two labels (%s, %s)", key, other, e.ObjectID)
		}
		seen[key] = e.ObjectID
		r, _ := utf8.DecodeRuneInString(key)
		img, err := parts.FindLegendSign(d, r)
		if err != nil {
			return nil, err
		}
		it := manifest.LegendItem{Key: key, Label: e.Text[len(key):], ImageID: img.ObjectID, LabelID: e.ObjectID, On: true}
		if v, ok := on[key]; ok {
			it.On = v
		}
		lg.Items = append(lg.Items, it)
	}
	if len(lg.Items) == 0 {
		return nil, fmt.Errorf("legend labels ①..⑳ below y=%.0fmm: %w", parts.LegendLabelMinY, model.ErrNotFound)
	}
	sort.SliceStable(lg.Items, func(a, b int) bool { return lg.Items[a].Key < lg.Items[b].Key })
	return lg, nil
}

// SetLegendOn: 項目 key（"③" か "3"）の on/off。key は adopt 時の元の番号。
func SetLegendOn(m *manifest.Manifest, key string, on bool) error {
	if m == nil || m.Legend == nil {
		return errors.New("no legend in manifest; run legend adopt first")
	}
	k, err := NormalizeKey(key)
	if err != nil {
		return err
	}
	for i := range m.Legend.Items {
		if m.Legend.Items[i].Key == k {
			m.Legend.Items[i].On = on
			return nil
		}
	}
	return fmt.Errorf("legend %s: %w", k, model.ErrNotFound)
}

// SetLegendMode: "draft" | "final"。
func SetLegendMode(m *manifest.Manifest, mode string) error {
	if m == nil || m.Legend == nil {
		return errors.New("no legend in manifest; run legend adopt first")
	}
	if mode != ModeDraft && mode != ModeFinal {
		return fmt.Errorf("legend mode %q: want draft|final", mode)
	}
	m.Legend.Mode = mode
	return nil
}
