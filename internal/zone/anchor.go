package zone

import (
	"fmt"
	"strings"

	"github.com/YuujiKamura/AISLIDE/internal/manifest"
	"github.com/YuujiKamura/AISLIDE/internal/model"
)

// SideOffsetMM: Side "L"/"R" の紙面上の横ずらし（進行方向の左右）。
const SideOffsetMM = 6.0

// SplitEnd: "Z1.E" → ("Z1", "E")。端名の無い参照は ok=false。
func SplitEnd(ref string) (zoneName, end string, ok bool) {
	i := strings.LastIndexByte(ref, '.')
	if i <= 0 || i == len(ref)-1 {
		return "", "", false
	}
	return ref[:i], ref[i+1:], true
}

// Resolve: 部品のアンカー相対位置 → 紙面 mm（THEORY P2, P4）。
//
//   - "Z1.E": 区間 Z1 の E 端。OffsetM>0 は端から外側（接近側＝予告看板の側）へ端の向きのまま直線で、
//     <0 は内側へ道なり。Side "L"/"R" は端へ向かって走る車（外→内）から見た左右に SideOffsetMM。
//   - "S2": 施工箇所 m.Sites["S2"]。"abs": p.At。これらに OffsetM / Side は使えない（ng）。
//   - Override（紙面 mm の dx,dy、人の手直し）は最後に足す。
func Resolve(m *manifest.Manifest, f Frame, p manifest.Part) (x, y float64, err error) {
	a := strings.TrimSpace(p.Anchor)
	switch {
	case a == "abs":
		if p.OffsetM != 0 || p.Side != "" {
			return 0, 0, fmt.Errorf("anchor abs: offsetM/side not allowed")
		}
		x, y = f.MM(p.At)
	case strings.HasPrefix(a, "S") && !strings.Contains(a, "."):
		if p.OffsetM != 0 || p.Side != "" {
			return 0, 0, fmt.Errorf("anchor %s: offsetM/side need a zone end anchor", a)
		}
		s, ok := m.Sites[a]
		if !ok {
			return 0, 0, fmt.Errorf("anchor %s: site %w", a, model.ErrNotFound)
		}
		x, y = f.MM(s.At)
	default:
		zn, end, ok := SplitEnd(a)
		if !ok {
			return 0, 0, fmt.Errorf("anchor %q: want Z<n>.<E|W|N|S> | S<n> | abs", a)
		}
		z, ok := m.Zones[zn]
		if !ok {
			return 0, 0, fmt.Errorf("anchor %s: zone %s %w", a, zn, model.ErrNotFound)
		}
		ei, err := EndIndex(z, end)
		if err != nil {
			return 0, 0, fmt.Errorf("anchor %s: %w", a, err)
		}
		var ox, oy float64
		x, y, ox, oy = FromEnd(f, z.Path, ei, p.OffsetM)
		// 進行方向 t = -outward。紙面（y 下向き）で t の左は (ty, -tx) = (-oy, ox)。
		lx, ly := -oy, ox
		switch p.Side {
		case "":
		case "L":
			x, y = x+lx*SideOffsetMM, y+ly*SideOffsetMM
		case "R":
			x, y = x-lx*SideOffsetMM, y-ly*SideOffsetMM
		default:
			return 0, 0, fmt.Errorf("side %q: want L | R", p.Side)
		}
	}
	return x + p.Override[0], y + p.Override[1], nil
}
