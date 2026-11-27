// Package manifest: 図の「意図の正本」（docs/THEORY.md P1）。スライドのスピーカーノートに保存する。
//
// ノート本文の形式:
//
//	#aislide manifest v1
//	{ ...JSON (Manifest) ... }
//
// ノートに他の文字がある場合、この 2 部分（マーカー行から JSON の終わりまで）だけを置き換え、他は保持する。
package manifest

// Manifest: 図 1 枚の意図。図形はここから投影される。
type Manifest struct {
	Version int                  `json:"version"` // 1
	Legend  *Legend              `json:"legend,omitempty"`
	Sites   map[string]Site      `json:"sites,omitempty"` // "S1".. 施工箇所
	Zones   map[string]Zone      `json:"zones,omitempty"` // "Z1".. 規制区間
	Parts   map[string]Part      `json:"parts,omitempty"` // "Z1.E.予告#1" 等。アンカー相対の部品
	Sets    map[string]SetSwitch `json:"sets,omitempty"`  // "Z1.E.予告" 等の配置セットの on/off
}

// Legend: 看板セット（ひな形一式）と項目ごとのスイッチ（THEORY P8）。
type Legend struct {
	Mode  string       `json:"mode"` // "draft"（off=斜線） | "final"（off=非表示＋番号詰め）
	Items []LegendItem `json:"items"`
}

type LegendItem struct {
	Key     string `json:"key"`     // 元の番号 "③"
	Label   string `json:"label"`   // 「100m先工事中」（番号を除いた文言）
	ImageID string `json:"imageId"` // 凡例の看板画像の objectId
	LabelID string `json:"labelId"` // 凡例ラベルの objectId
	On      bool   `json:"on"`
}

type Site struct {
	N  int        `json:"n"`
	At [2]float64 `json:"at"` // lat,lon
}

// Zone: 道路沿いの規制区間（折れ線、lat/lon）。端は Ends で名前を付ける（"E","W","N","S"）。
type Zone struct {
	Kind    string          `json:"kind"` // "closure"（通行止め）| "detour"
	Path    [][2]float64    `json:"path"`
	Ends    map[string]int  `json:"ends,omitempty"` // 端名 -> Path の index（0 か len-1）
	Exclude [][2][2]float64 `json:"exclude,omitempty"`
	From    string          `json:"from,omitempty"` // adopt 元の objectId（人の線）
	Flag    [2]float64      `json:"flag,omitempty"` // 旗揚げ（L=…m）の人の手直し（紙面 mm の dx,dy、P4）
}

// Part: アンカー相対の部品（THEORY P2, P4）。
type Part struct {
	Kind     string     `json:"kind"`               // sign / guard / note / site …
	Key      string     `json:"key,omitempty"`      // 看板の凡例キー "④"
	Anchor   string     `json:"anchor"`             // "Z1.E" | "S2" | "abs"
	OffsetM  float64    `json:"offsetM"`            // アンカーから道路に沿って外側へ +、内側へ -（m）
	Side     string     `json:"side,omitempty"`     // 道路の左右 "L"/"R"/""
	At       [2]float64 `json:"at,omitempty"`       // Anchor=="abs" のとき lat,lon
	Override [2]float64 `json:"override,omitempty"` // 人の手直し（紙面 mm の dx,dy）
	Text     string     `json:"text,omitempty"`
	On       bool       `json:"on"`
}

type SetSwitch struct {
	Rule string `json:"rule,omitempty"` // "FR-10" 等。生成規則
	On   bool   `json:"on"`
}

const Marker = "#aislide manifest v1"
