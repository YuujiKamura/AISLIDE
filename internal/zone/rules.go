package zone

import (
	"fmt"
	"sort"
	"strings"

	"github.com/YuujiKamura/AISLIDE/internal/manifest"
	"github.com/YuujiKamura/AISLIDE/internal/model"
)

// 配置セットの生成規則（THEORY P7）。AI は区間と例外だけを決め、枚数・距離はここが決める。

// SetSuffixAdvance: 端ごとの予告看板セットの名前（"Z1.E" + SetSuffixAdvance）。
const SetSuffixAdvance = ".予告"

// LegendLookup: 凡例の文言（部分一致）→ 凡例キー（"④"）。無ければ error。
type LegendLookup func(labelPart string) (string, error)

// Rule: 区間 zname の各端に配置セットを生成し、作った（置き換えた）セット名を返す。
type Rule func(m *manifest.Manifest, zname string, lookup LegendLookup) ([]string, error)

// Rules: 規則 ID → 生成規則。
var Rules = map[string]Rule{
	"FR-10": fr10,
}

// RuleIDs: 既知の規則 ID（ソート済み）。
func RuleIDs() []string {
	ids := make([]string, 0, len(Rules))
	for k := range Rules {
		ids = append(ids, k)
	}
	sort.Strings(ids)
	return ids
}

// ApplyRule: 規則 rule を区間 zname に当てる。
func ApplyRule(m *manifest.Manifest, zname, rule string, lookup LegendLookup) ([]string, error) {
	r, ok := Rules[rule]
	if !ok {
		return nil, fmt.Errorf("rule %q unknown (rules: %s)", rule, strings.Join(RuleIDs(), ", "))
	}
	if _, ok := m.Zones[zname]; !ok {
		return nil, fmt.Errorf("zone %s: %w", zname, model.ErrNotFound)
	}
	return r(m, zname, lookup)
}

// fr10: construction-skills traffic-control-db FR-10（田園部小規模の車両通行止め）。
// 予告は各方向（区間の開いた端ごと）に「50m先工事中」を 1 枚、端から 50m 外側・進行方向左。
// 100m先・徐行は置かない。
func fr10(m *manifest.Manifest, zname string, lookup LegendLookup) ([]string, error) {
	key, err := lookup("50m先")
	if err != nil {
		return nil, fmt.Errorf("FR-10 needs the 50m先 sign in the legend: %w", err)
	}
	z := m.Zones[zname]
	if len(z.Ends) == 0 {
		return nil, fmt.Errorf("zone %s has no named ends", zname)
	}
	if m.Parts == nil {
		m.Parts = map[string]manifest.Part{}
	}
	if m.Sets == nil {
		m.Sets = map[string]manifest.SetSwitch{}
	}
	ends := make([]string, 0, len(z.Ends))
	for e := range z.Ends {
		ends = append(ends, e)
	}
	sort.Strings(ends)
	var sets []string
	for _, e := range ends {
		set := zname + "." + e + SetSuffixAdvance
		unit := set + "#1"
		prev, had := m.Parts[unit]
		for u := range m.Parts { // セットは規則が丸ごと決める（前の規則の残りを消す）
			if strings.HasPrefix(u, set+"#") {
				delete(m.Parts, u)
			}
		}
		p := manifest.Part{Kind: "sign", Key: key, Anchor: zname + "." + e, OffsetM: 50, Side: "L", On: true}
		if had {
			p.Override = prev.Override // 人の手直しは保つ（P4）
		}
		m.Parts[unit] = p
		m.Sets[set] = manifest.SetSwitch{Rule: "FR-10", On: true}
		sets = append(sets, set)
	}
	return sets, nil
}
