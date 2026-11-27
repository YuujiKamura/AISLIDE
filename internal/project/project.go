// Package project: manifest（意図）→ 図形の投影エンジン（THEORY P1, P3）。
//
// 各 Projector は「あるべき図形」の Spec 列を返す。Run がデッキの現状と突き合わせて差分 Op を作る:
//   - Spec.ID の要素が無い        → Spec.Create（作成 Op 列）
//   - あるが Box が 0.1mm 超ずれる → OpTransform（ただし人が動かした場合の扱いは Drift 参照）
//   - あるが Text が違う          → OpSetText
//   - Projector が所有する unit の派生要素で、Spec に無いもの → OpDelete
//
// 派生要素は Tag{Layer:"derived", Kind:<role>, Params:{"unit":<unit 名>, "role":<role>}} を持ち、
// objectId は ID(unit, role) で決定的に作る（再投影で同じ ID になる）。
package project

import (
	"github.com/YuujiKamura/AISLIDE/internal/manifest"
	"github.com/YuujiKamura/AISLIDE/internal/model"
)

// LayerDerived: 投影エンジンだけが書く層（agent の claim 不可、人の手直しは Drift として拾う）。
const LayerDerived = model.LayerDerived

type Spec struct {
	ID     string     // ID(unit, role)
	Unit   string     // "legend.③", "Z1", "Z1.E.予告#1" …
	Role   string     // "strike1", "flag_line", "flag_text", "sign", "badge" …
	Box    model.Box  // あるべき位置（紙面 mm）
	Text   string     // 文字を持つ要素のあるべき文字（無ければ ""）
	Create []model.Op // 無いときに作る Op 列（Tag 付き、ObjectID=ID）
	// Movable: 人が動かしてよい要素（看板など）。true のとき位置ずれは上書きせず Drift として報告する。
	Movable bool
	// Base: ID は既存の base 要素（凡例ラベル等）。Run は Text だけを合わせ、作成・移動・削除はしない
	// （要素が無ければ error）。凡例の番号詰め（THEORY P8）のための唯一の base への書込み口。
	Base bool
}

type Projector interface {
	Name() string
	Owns(unit string) bool // この Projector が責任を持つ unit か（削除判定に使う）
	Project(m *manifest.Manifest, d *model.Deck) ([]Spec, error)
}

// Drift: Movable な派生要素を人が動かした分（紙面 mm）。呼び出し側が manifest の Override に記録する（P4）。
type Drift struct {
	Unit   string
	Role   string
	DX, DY float64
}

// ID（決定的 objectId）と Run（差分）の実装は run.go。
