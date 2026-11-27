package project

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/YuujiKamura/AISLIDE/internal/manifest"
	"github.com/YuujiKamura/AISLIDE/internal/model"
	"github.com/YuujiKamura/AISLIDE/internal/parts"
	"github.com/YuujiKamura/AISLIDE/internal/zone"
)

// DriftRecorder: Drift を manifest の上書き（Override / Zone.Flag）に記録できる Projector（P4）。
// Run の呼び出し側（sync / project / zone・set コマンド）が、Run の返す Drift を自分の unit を
// 持つ Projector に渡す。記録したら true。
type DriftRecorder interface {
	RecordDrift(m *manifest.Manifest, dr Drift) bool
}

// 旗揚げ（区間長の引出し）の寸法。紙面 mm / pt。
const (
	FlagGapMM    = 1.0  // 区間線から引出し始点までの法線方向のすき間
	FlagLeaderMM = 12.0 // 引出し線の長さ（60°）。下線の先が区間線に近いときは伸ばす
	FlagClearMM  = 2.0  // 下線（と文字）と区間線の最小のすき間（区間中点の接線から）
	FlagUnderMM  = 22.0 // 下線の長さ
	FlagTextHMM  = 6.0  // 文字枠の高さ（下線の上に載る）
	FlagFontPt   = 10
	FlagLinePt   = 1.0
)

var (
	colorBlack = model.RGB{R: 0, G: 0, B: 0}
	dirUpRight = [2]float64{0.5, -math.Sqrt(3) / 2} // 60°（紙面 y 下向き）
	dirUpLeft  = [2]float64{-0.5, -math.Sqrt(3) / 2}
)

// IsZoneUnit: "Z1" のような区間そのものの unit（端 "Z1.E" やセット・部品は含まない）。
func IsZoneUnit(u string) bool {
	return strings.HasPrefix(u, "Z") && len(u) > 1 && !strings.ContainsAny(u, ".#")
}

// Zones: 区間ごとの派生物を投影する（unit = 区間名 "Z1"）。
//   - 旗揚げ: 区間の中点から上側へ 60° の引出し線 flag_leader、水平の下線 flag_under、
//     下線に載る「L=<実長を四捨五入>m」flag_text（10pt 太字 黒）。flag_text は Movable（人が
//     ドラッグした分は Zone.Flag に記録され、下線と引出し線の先が付いてくる。引出し始点は区間上に固定）。
//   - 区間線の派生オーバーレイ line（, line_2, …）: 区間が人の線（From）と一致しない
//     （extend 等で manifest 側が変わった / From が無い・消えた）ときだけ、赤実線（迂回路は青破線）で描く。
//     人の線そのもの（base）は読み取り専用のまま触らない。古くなった人の線は StaleBases で報告する。
type Zones struct {
	IDFunc func(unit, role string) string // nil なら ID
}

func (Zones) Name() string          { return "zones" }
func (Zones) Owns(unit string) bool { return IsZoneUnit(unit) }

func (p Zones) id(u, r string) string {
	if p.IDFunc != nil {
		return p.IDFunc(u, r)
	}
	return ID(u, r)
}

func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func derivedTag(unit, role string) *model.Tag {
	return &model.Tag{Layer: LayerDerived, Kind: role, Params: map[string]any{"unit": unit, "role": role}}
}

func lineBox(x1, y1, x2, y2 float64) model.Box {
	return model.Box{X: math.Min(x1, x2), Y: math.Min(y1, y2), W: math.Abs(x2 - x1), H: math.Abs(y2 - y1)}
}

func (p Zones) lineSpec(unit, role string, x1, y1, x2, y2 float64, st model.LineStyle) Spec {
	id := p.id(unit, role)
	return Spec{ID: id, Unit: unit, Role: role, Box: lineBox(x1, y1, x2, y2),
		Create: []model.Op{{Kind: model.OpCreateLine, ObjectID: id, X1: x1, Y1: y1, X2: x2, Y2: y2,
			Line: &st, Tag: derivedTag(unit, role)}}}
}

// FlagText: 旗揚げの文字。
func FlagText(lengthM float64) string { return fmt.Sprintf("L=%dm", int(math.Round(lengthM))) }

// FlagGeom: 旗揚げの幾何（紙面 mm）。
type FlagGeom struct {
	StartX, StartY float64 // 引出し始点（区間中点から上側へ FlagGapMM）
	ElbowX, ElbowY float64 // 引出し線の先＝下線の始点（Override 込み）
	EndX, EndY     float64 // 下線の終点
	Text           model.Box
}

// Flag: 区間 z の旗揚げ。引出しは区間の法線のうち紙面上側（区間が垂直なら右側）へ出し、
// 60° 右上を基本に、区間とほぼ平行になる場合は 60° 左上にする（文字が区間線に載らないように）。
func Flag(f zone.Frame, z manifest.Zone) (FlagGeom, float64) {
	L := zone.Length(f, z.Path)
	mx, my := zone.PointAt(f, z.Path, L/2)
	tx, ty := zone.TangentAt(f, z.Path, L/2)
	nx, ny := -ty, tx
	if ny > 1e-9 || (math.Abs(ny) <= 1e-9 && nx < 0) {
		nx, ny = -nx, -ny
	}
	cross := func(d [2]float64) float64 { return math.Abs(tx*d[1] - ty*d[0]) }
	dot := func(d [2]float64) float64 { return d[0]*nx + d[1]*ny }
	d := dirUpRight
	if !(dot(dirUpRight) > 0 && cross(dirUpRight) >= 0.5) && dot(dirUpLeft) > 0 && cross(dirUpLeft) > cross(dirUpRight) {
		d = dirUpLeft
	}
	g := FlagGeom{StartX: mx + nx*FlagGapMM, StartY: my + ny*FlagGapMM}
	sx := 1.0
	if d[0] < 0 {
		sx = -1
	}
	// 下線の先が区間線に近づく（傾いた区間）なら、引出しを 60° のまま伸ばして FlagClearMM 離す。
	lead := FlagLeaderMM
	ux, uy := (d[0]*lead + sx*FlagUnderMM), d[1]*lead // 始点→下線の先
	if dist := FlagGapMM + ux*nx + uy*ny; dist < FlagClearMM {
		lead += (FlagClearMM - dist) / dot(d)
	}
	g.ElbowX = g.StartX + d[0]*lead + z.Flag[0]
	g.ElbowY = g.StartY + d[1]*lead + z.Flag[1]
	g.EndX, g.EndY = g.ElbowX+sx*FlagUnderMM, g.ElbowY
	g.Text = model.Box{X: math.Min(g.ElbowX, g.EndX), Y: g.ElbowY - FlagTextHMM, W: FlagUnderMM, H: FlagTextHMM}
	return g, L
}

// baseDiffers: 区間が From の人の線と一致しないか（From 無し・消えた・2 点でない・端が 0.5m 超ずれる）。
func baseDiffers(f zone.Frame, z manifest.Zone, d *model.Deck) (differs bool, base *model.Element) {
	if z.From == "" {
		return true, nil
	}
	e, err := d.Get(z.From)
	if err != nil || e.Kind != "line" {
		return true, nil
	}
	if len(z.Path) != 2 {
		return true, e
	}
	x1, y1, x2, y2 := e.LineEnds()
	a, b := f.LL(x1, y1), f.LL(x2, y2)
	const tol = 0.5
	same := f.Meters(a, z.Path[0]) <= tol && f.Meters(b, z.Path[1]) <= tol
	swap := f.Meters(a, z.Path[1]) <= tol && f.Meters(b, z.Path[0]) <= tol
	return !(same || swap), e
}

// StaleBases: manifest の区間と一致しなくなった人の線（"Z1=<objectId>"）。人の線は書き換えないので、
// 応答で人に知らせる（消す・引き直すのは人の判断）。
func StaleBases(m *manifest.Manifest, d *model.Deck) []string {
	if m == nil || len(m.Zones) == 0 {
		return nil
	}
	f, err := zone.FrameOf(d)
	if err != nil {
		return nil
	}
	var out []string
	for _, n := range sortedKeys(m.Zones) {
		z := m.Zones[n]
		if differs, base := baseDiffers(f, z, d); differs && base != nil {
			out = append(out, n+"="+base.ObjectID)
		}
	}
	return out
}

func (p Zones) Project(m *manifest.Manifest, d *model.Deck) ([]Spec, error) {
	if m == nil || len(m.Zones) == 0 {
		return nil, nil
	}
	f, err := zone.FrameOf(d)
	if err != nil {
		return nil, fmt.Errorf("zones: %w", err)
	}
	var specs []Spec
	for _, name := range sortedKeys(m.Zones) {
		z := m.Zones[name]
		if len(z.Path) < 2 {
			return nil, fmt.Errorf("zone %s: path needs at least 2 points", name)
		}
		if differs, _ := baseDiffers(f, z, d); differs {
			st := model.LineStyle{Color: parts.ColorRed, Weight: parts.ClosurePt, Dash: "SOLID"}
			if z.Kind == "detour" {
				st = model.LineStyle{Color: parts.ColorBlue, Weight: parts.DetourPt, Dash: "DASH"}
			}
			for i := 1; i < len(z.Path); i++ {
				role := "line"
				if i > 1 {
					role = fmt.Sprintf("line_%d", i)
				}
				x1, y1 := f.MM(z.Path[i-1])
				x2, y2 := f.MM(z.Path[i])
				specs = append(specs, p.lineSpec(name, role, x1, y1, x2, y2, st))
			}
		}
		g, L := Flag(f, z)
		ink := model.LineStyle{Color: colorBlack, Weight: FlagLinePt, Dash: "SOLID"}
		specs = append(specs,
			p.lineSpec(name, "flag_leader", g.StartX, g.StartY, g.ElbowX, g.ElbowY, ink),
			p.lineSpec(name, "flag_under", g.ElbowX, g.ElbowY, g.EndX, g.EndY, ink))
		id := p.id(name, "flag_text")
		txt := FlagText(L)
		specs = append(specs, Spec{ID: id, Unit: name, Role: "flag_text", Box: g.Text, Text: txt, Movable: true,
			Create: []model.Op{{Kind: model.OpCreateShape, ObjectID: id, Box: g.Text, Tag: derivedTag(name, "flag_text"),
				Shape: &model.ShapeStyle{ShapeType: "RECTANGLE", Text: txt, FontPt: FlagFontPt,
					FontColor: colorBlack, Bold: true, AlignCenter: true}}}})
	}
	return specs, nil
}

// RecordDrift: flag_text を人が動かした分を Zone.Flag に足す。
func (Zones) RecordDrift(m *manifest.Manifest, dr Drift) bool {
	if dr.Role != "flag_text" || !IsZoneUnit(dr.Unit) {
		return false
	}
	z, ok := m.Zones[dr.Unit]
	if !ok {
		return false
	}
	z.Flag[0] += dr.DX
	z.Flag[1] += dr.DY
	m.Zones[dr.Unit] = z
	return true
}
