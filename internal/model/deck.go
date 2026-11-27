package model

import "errors"

var ErrNotFound = errors.New("not found")
var ErrReadOnly = errors.New("read-only (base element; adopt first)")
var ErrNotOwner = errors.New("layer owned by another agent")

// Element: スライド 1 枚上の要素（トップレベルのみ。group の中身は展開しない）。
type Element struct {
	Index    int    // list 順の index（pageElements の順）
	ObjectID string
	Kind     string // "image" | "shape" | "line" | "group" | "table" | "other"
	Box      Box    // 紙面 mm（transform と size から算出。line は外接矩形で W/H は負にならない）
	Flip     [2]bool // line の向き（scaleX<0 / scaleY<0）
	Text     string
	Tag      *Tag   // nil = base（人が描いた or 元からある要素）
	SizeEMU  [2]float64 // size.width/height magnitude（Transform の scale 算出に要る）
}

// Layer: タグ無しは "base"。
func (e *Element) Layer() string { panic("TODO") }

type Deck struct {
	PresID   string
	Rev      string
	PageID   string
	PageW    float64 // mm
	PageH    float64
	Elements []Element
	Owners   map[string]string // layer -> agent（claim 情報。デッキ再読込でも保持するため cp 側で引き継ぐ）
}

// ParseDeck: presentations.get の生 JSON から slideNo(1 始まり) のページを構築。
// description が TagPrefix で始まれば Tag、LegacyGeorefPrefix なら map_bg の Tag に変換。
func ParseDeck(raw []byte, slideNo int) (*Deck, error) { panic("TODO") }

// Get: ref は index（"12"）か objectId。
func (d *Deck) Get(ref string) (*Element, error) { panic("TODO") }

// Claim: layer を agent が所有。既に他 agent なら ErrNotOwner。"base" "_cursor" "map" 以外の任意名。
func (d *Deck) Claim(layer, agent string) error { panic("TODO") }

// CanWrite: e を agent が書き換えてよいか。base は ErrReadOnly、他人のレイヤは ErrNotOwner。
func (d *Deck) CanWrite(e *Element, agent string) error { panic("TODO") }

// MapBG: georef 付き背景画像。無ければ ErrNotFound。
func (d *Deck) MapBG() (*Element, *GeorefTag, error) { panic("TODO") }

// ResolvePoint: "lat,lon" | "site:N" | "mm:x,y" を紙面 mm に。
func (d *Deck) ResolvePoint(s string) (x, y float64, err error) { panic("TODO") }

// ResolveLength: "40m"（georef で mm に換算）| "12mm"。
func (d *Deck) ResolveLength(s string) (mm float64, err error) { panic("TODO") }
