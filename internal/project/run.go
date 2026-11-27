package project

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"math"
	"strings"

	"github.com/YuujiKamura/AISLIDE/internal/manifest"
	"github.com/YuujiKamura/AISLIDE/internal/model"
)

// BoxTol: これ以下の位置・寸法の差は「ずれていない」とみなす（mm）。
const BoxTol = 0.1

// ID: unit と role から決定的な objectId: "kd_" + sha1(unit+"/"+role) の先頭 12 hex
// （15 文字。Slides の objectId 制約 5..50 文字, [a-zA-Z0-9_-:] を満たす）。
func ID(unit, role string) string {
	h := sha1.Sum([]byte(unit + "/" + role))
	return "kd_" + hex.EncodeToString(h[:])[:12]
}

// DerivedTag: 派生要素のタグ（Spec.Create の作成 Op に付ける）。
func DerivedTag(unit, role string) *model.Tag {
	return &model.Tag{Layer: LayerDerived, Kind: role, Params: map[string]any{"unit": unit, "role": role}}
}

// UnitOf: 派生要素の unit 名（派生でなければ ""）。
func UnitOf(e *model.Element) string {
	if e.Tag == nil || e.Tag.Layer != LayerDerived {
		return ""
	}
	u, _ := e.Tag.Params["unit"].(string)
	return u
}

func boxNear(a, b model.Box) bool {
	return math.Abs(a.X-b.X) <= BoxTol && math.Abs(a.Y-b.Y) <= BoxTol &&
		math.Abs(a.W-b.W) <= BoxTol && math.Abs(a.H-b.H) <= BoxTol
}

// Run: 全 Projector を走らせ、差分 Op 列と Drift を返す。純関数。
//
// Op の順: 不要になった派生要素の削除（デッキ順）→ Spec 順に 作成 / 移動 / 文字。
// 適用後のデッキでもう一度 Run すると 0 件になる（冪等）。m が nil なら空の manifest として扱う。
func Run(m *manifest.Manifest, d *model.Deck, ps []Projector) ([]model.Op, []Drift, error) {
	if m == nil {
		m = manifest.New()
	}
	byID := map[string]*model.Element{}
	for i := range d.Elements {
		byID[d.Elements[i].ObjectID] = &d.Elements[i]
	}
	var specs []Spec
	want := map[string]bool{}
	for _, p := range ps {
		ss, err := p.Project(m, d)
		if err != nil {
			return nil, nil, fmt.Errorf("project %s: %w", p.Name(), err)
		}
		for _, s := range ss {
			if s.ID == "" {
				return nil, nil, fmt.Errorf("project %s: spec %s/%s without ID", p.Name(), s.Unit, s.Role)
			}
			if want[s.ID] {
				return nil, nil, fmt.Errorf("project %s: duplicate spec id %s (%s/%s)", p.Name(), s.ID, s.Unit, s.Role)
			}
			want[s.ID] = true
			specs = append(specs, s)
		}
	}

	var ops []model.Op
	for i := range d.Elements {
		e := &d.Elements[i]
		u := UnitOf(e)
		if u == "" || want[e.ObjectID] {
			continue
		}
		for _, p := range ps {
			if p.Owns(u) {
				ops = append(ops, model.Op{Kind: model.OpDelete, TargetID: e.ObjectID})
				break
			}
		}
	}

	var drifts []Drift
	for _, s := range specs {
		e := byID[s.ID]
		if e == nil {
			if s.Base {
				return nil, nil, fmt.Errorf("spec %s/%s: base element %s: %w", s.Unit, s.Role, s.ID, model.ErrNotFound)
			}
			ops = append(ops, s.Create...)
			continue
		}
		if !s.Base && !boxNear(e.Box, s.Box) {
			if s.Movable {
				drifts = append(drifts, Drift{Unit: s.Unit, Role: s.Role, DX: e.Box.X - s.Box.X, DY: e.Box.Y - s.Box.Y})
			} else {
				ops = append(ops, model.Op{Kind: model.OpTransform, TargetID: s.ID, Box: s.Box})
			}
		}
		// 文字は shape（と Base 指定の要素）だけが持つ。Element.Text は前後空白除去済み。
		if (s.Base || e.Kind == "shape") && strings.TrimSpace(s.Text) != e.Text {
			ops = append(ops, model.Op{Kind: model.OpSetText, TargetID: s.ID, Text: s.Text})
		}
	}
	return ops, drifts, nil
}
