// Package zone: 規制区間（lat/lon の折れ線）の純幾何（THEORY P2, P5）。
//
// 道路沿いの長さは紙面上の折れ線を georef の MetersBetween で測った実 m の和。紙面上では各区間は
// 直線として描かれるので、区間内の点・接線は紙面 mm で線形に扱い、結果を lat/lon に戻す。
package zone

import (
	"errors"
	"fmt"
	"math"

	"github.com/YuujiKamura/AISLIDE/internal/geo"
	"github.com/YuujiKamura/AISLIDE/internal/manifest"
	"github.com/YuujiKamura/AISLIDE/internal/model"
)

// LL: 緯度経度 {lat, lon}（manifest.Zone.Path の要素と同形）。
type LL = [2]float64

// MinLengthM: trim 後に残すべき最小の区間長。
const MinLengthM = 0.5

var ErrTooShort = errors.New("zone would be shorter than 0.5m")

// Frame: 背景写真の georef と紙面上の箱。lat/lon <-> 紙面 mm と実距離の換算に使う。
type Frame struct {
	G   geo.Georef
	Img geo.Box
}

// FrameOf: デッキの背景写真から Frame（georef 必須）。
func FrameOf(d *model.Deck) (Frame, error) {
	g, b, err := d.Geo()
	if err != nil {
		return Frame{}, err
	}
	return Frame{G: g, Img: b}, nil
}

// MM: lat/lon → 紙面 mm。
func (f Frame) MM(p LL) (x, y float64) { return f.G.ToMM(f.Img, p[0], p[1]) }

// LL: 紙面 mm → lat/lon。
func (f Frame) LL(x, y float64) LL {
	lat, lon := f.G.ToLatLon(f.Img, x, y)
	return LL{lat, lon}
}

// Meters: 2 点間の実距離（紙面上の直線を MetersBetween で測る）。
func (f Frame) Meters(a, b LL) float64 {
	x1, y1 := f.MM(a)
	x2, y2 := f.MM(b)
	return f.G.MetersBetween(f.Img, x1, y1, x2, y2)
}

// MMPerMeterAlong: 紙面上の単位方向 (ux,uy) に沿った縮尺（mm / 実 m）。縦横非等倍にも正しい。
func (f Frame) MMPerMeterAlong(ux, uy float64) float64 {
	m := f.G.MetersBetween(f.Img, 0, 0, ux, uy)
	if m <= 0 {
		return 0
	}
	return math.Hypot(ux, uy) / m
}

// Length: 折れ線の実長（m）。
func Length(f Frame, path []LL) float64 {
	var s float64
	for i := 1; i < len(path); i++ {
		s += f.Meters(path[i-1], path[i])
	}
	return s
}

// locate: 実距離 s（0..L にクランプ）の区間 index と区間内の割合。
func locate(f Frame, path []LL, s float64) (seg int, t float64) {
	if len(path) < 2 {
		return 0, 0
	}
	if s <= 0 {
		return 0, 0
	}
	for i := 1; i < len(path); i++ {
		l := f.Meters(path[i-1], path[i])
		if s <= l || i == len(path)-1 {
			if l <= 0 {
				return i - 1, 1
			}
			return i - 1, math.Min(1, s/l)
		}
		s -= l
	}
	return len(path) - 2, 1
}

// PointAt: 始点から実距離 s の点（紙面 mm）。s は 0..Length にクランプ。
func PointAt(f Frame, path []LL, s float64) (x, y float64) {
	if len(path) == 0 {
		return 0, 0
	}
	if len(path) == 1 {
		return f.MM(path[0])
	}
	i, t := locate(f, path, s)
	x1, y1 := f.MM(path[i])
	x2, y2 := f.MM(path[i+1])
	return x1 + (x2-x1)*t, y1 + (y2-y1)*t
}

// TangentAt: 実距離 s での進行方向（始点→終点向き）の紙面上の単位ベクトル。
func TangentAt(f Frame, path []LL, s float64) (ux, uy float64) {
	if len(path) < 2 {
		return 1, 0
	}
	i, _ := locate(f, path, s)
	x1, y1 := f.MM(path[i])
	x2, y2 := f.MM(path[i+1])
	return unit(x2-x1, y2-y1)
}

func unit(dx, dy float64) (float64, float64) {
	l := math.Hypot(dx, dy)
	if l == 0 {
		return 1, 0
	}
	return dx / l, dy / l
}

func reversed(p []LL) []LL {
	out := make([]LL, len(p))
	for i := range p {
		out[len(p)-1-i] = p[i]
	}
	return out
}

// Extend: 端（atEnd=true なら終点、false なら始点）を deltaM だけ延ばす（+）か詰める（-）。
// 延長は端の区間の向きに直線で伸ばし、頂点数は変えない。短縮は道なりに詰め、越えた頂点は落とす。
// 結果が MinLengthM 未満になる短縮は ErrTooShort。元の path は変更しない。
func Extend(f Frame, path []LL, atEnd bool, deltaM float64) ([]LL, error) {
	if len(path) < 2 {
		return nil, errors.New("zone path needs at least 2 points")
	}
	if math.IsNaN(deltaM) || math.IsInf(deltaM, 0) {
		return nil, errors.New("bad length")
	}
	p := append([]LL(nil), path...)
	if !atEnd {
		p = reversed(p)
	}
	n := len(p)
	if deltaM >= 0 {
		x1, y1 := f.MM(p[n-2])
		x2, y2 := f.MM(p[n-1])
		ux, uy := unit(x2-x1, y2-y1)
		k := deltaM * f.MMPerMeterAlong(ux, uy)
		p[n-1] = f.LL(x2+ux*k, y2+uy*k)
	} else {
		L := Length(f, p)
		keep := L + deltaM
		if keep < MinLengthM {
			return nil, fmt.Errorf("trim %.1fm of %.1fm: %w", -deltaM, L, ErrTooShort)
		}
		i, _ := locate(f, p, keep)
		x, y := PointAt(f, p, keep)
		p = append(p[:i+1:i+1], f.LL(x, y))
	}
	if !atEnd {
		p = reversed(p)
	}
	return p, nil
}

// approxMeters: lat/lon 差の概算（東向き dx, 北向き dy, m）。端の命名にだけ使う。
func approxMeters(a, b LL) (dx, dy float64) {
	lat := (a[0] + b[0]) / 2 * math.Pi / 180
	return (b[1] - a[1]) * 111320 * math.Cos(lat), (b[0] - a[0]) * 110574
}

// EndNames: 端の名前。始点→終点が主に東西なら経度で "E"/"W"、主に南北なら緯度で "N"/"S"。
func EndNames(path []LL) map[string]int {
	n := len(path)
	if n < 2 {
		return nil
	}
	dx, dy := approxMeters(path[0], path[n-1])
	if math.Abs(dx) >= math.Abs(dy) {
		if dx >= 0 {
			return map[string]int{"W": 0, "E": n - 1}
		}
		return map[string]int{"E": 0, "W": n - 1}
	}
	if dy >= 0 {
		return map[string]int{"S": 0, "N": n - 1}
	}
	return map[string]int{"N": 0, "S": n - 1}
}

// EndIndex: z の端名 → Path の index（0 か len-1）。
func EndIndex(z manifest.Zone, name string) (int, error) {
	i, ok := z.Ends[name]
	if !ok {
		return 0, fmt.Errorf("end %q: %w (ends: %s)", name, model.ErrNotFound, endList(z))
	}
	if n := len(z.Path); n < 2 || (i != 0 && i != n-1) {
		return 0, fmt.Errorf("end %q: index %d is not an end of a %d-point path", name, i, len(z.Path))
	}
	return i, nil
}

func endList(z manifest.Zone) string {
	s := ""
	for _, k := range []string{"E", "W", "N", "S"} {
		if _, ok := z.Ends[k]; ok {
			s += k
		}
	}
	return s
}

// Outward: 端 endIdx での外向き（区間から離れる向き）の紙面単位ベクトル。
func Outward(f Frame, path []LL, endIdx int) (ux, uy float64) {
	L := Length(f, path)
	if endIdx == 0 {
		tx, ty := TangentAt(f, path, 0)
		return -tx, -ty
	}
	return TangentAt(f, path, L)
}

// FromEnd: 端 endIdx から道なりに m（+ は外側へ端の向きのまま直線、- は内側へ道なり）の点と、
// その端の外向き単位ベクトル。
func FromEnd(f Frame, path []LL, endIdx int, m float64) (x, y, ox, oy float64) {
	ox, oy = Outward(f, path, endIdx)
	if m >= 0 {
		ex, ey := f.MM(path[endIdx])
		k := m * f.MMPerMeterAlong(ox, oy)
		return ex + ox*k, ey + oy*k, ox, oy
	}
	L := Length(f, path)
	s := -m
	if endIdx != 0 {
		s = L + m
	}
	x, y = PointAt(f, path, s)
	return x, y, ox, oy
}

// RemapEnds: 頂点数が oldN → newN に変わったとき、終点を指していた端名を新しい終点へ。
func RemapEnds(ends map[string]int, oldN, newN int) map[string]int {
	out := make(map[string]int, len(ends))
	for k, v := range ends {
		if v == oldN-1 {
			v = newN - 1
		}
		out[k] = v
	}
	return out
}
