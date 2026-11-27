package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/YuujiKamura/AISLIDE/internal/geo"
)

var ErrNotFound = errors.New("not found")
var ErrReadOnly = errors.New("read-only (base element; adopt first)")
var ErrNotOwner = errors.New("layer owned by another agent")

// ErrReserved: 予約レイヤ（base / _cursor / map）は agent が claim・書込みできない。
var ErrReserved = errors.New("reserved layer (system only)")

// 予約レイヤ名。
const (
	LayerBase   = "base"
	LayerCursor = "_cursor"
	LayerMap    = "map"
)

// IsReservedLayer: agent が claim できないレイヤ。
func IsReservedLayer(layer string) bool {
	return layer == LayerBase || layer == LayerCursor || layer == LayerMap
}

// LegacyMaskPrefix: 旧 python 試作のマスク図形（タグ無しでも layer "map" kind "mask" とみなす）。
const LegacyMaskPrefix = "kl_mask_"

// EMUPerMM: 1mm = 36000 EMU。
const EMUPerMM = 36000.0

// emuPerPt: 1pt = 12700 EMU。
const emuPerPt = 12700.0

// Element: スライド 1 枚上の要素（トップレベルのみ。group の中身は展開しない）。
type Element struct {
	Index    int // list 順の index（pageElements の順）
	ObjectID string
	Kind     string     // "image" | "shape" | "line" | "group" | "table" | "other"
	Box      Box        // 紙面 mm（transform と size から算出。line は外接矩形で W/H は負にならない）
	Flip     [2]bool    // line の向き（scaleX<0 / scaleY<0）
	Text     string     // textRun の連結（前後空白除去）
	Tag      *Tag       // nil = base（人が描いた or 元からある要素）
	SizeEMU  [2]float64 // size.width/height magnitude（Transform の scale 算出に要る）

	// 以下は undo（削除の取り消し＝再作成）のために読み取る見た目。取れなければ nil。
	Shape *ShapeStyle // Kind=="shape" のとき
	Line  *LineStyle  // Kind=="line" のとき
}

// Layer: タグ無しは "base"。
func (e *Element) Layer() string {
	if e.Tag == nil || e.Tag.Layer == "" {
		return LayerBase
	}
	return e.Tag.Layer
}

// LineEnds: line の始点・終点（transform の (0,0)→(w,h) を Flip で戻したもの）。
func (e *Element) LineEnds() (x1, y1, x2, y2 float64) {
	x1, x2 = e.Box.X, e.Box.X+e.Box.W
	y1, y2 = e.Box.Y, e.Box.Y+e.Box.H
	if e.Flip[0] {
		x1, x2 = x2, x1
	}
	if e.Flip[1] {
		y1, y2 = y2, y1
	}
	return
}

// Center: Box の中心。
func (e *Element) Center() (x, y float64) { return e.Box.X + e.Box.W/2, e.Box.Y + e.Box.H/2 }

type Deck struct {
	PresID   string
	Rev      string
	PageID   string
	PageW    float64 // mm
	PageH    float64
	Elements []Element
	Owners   map[string]string // layer -> agent（claim 情報。デッキ再読込でも保持するため cp 側で引き継ぐ）
}

// ---- presentations.get の最小構造 ----

type rawDim struct {
	Magnitude float64 `json:"magnitude"`
	Unit      string  `json:"unit"`
}

func (d *rawDim) emu() float64 {
	if d == nil {
		return 0
	}
	if d.Unit == "PT" {
		return d.Magnitude * emuPerPt
	}
	return d.Magnitude
}

type rawRGB struct {
	Red   float64 `json:"red"`
	Green float64 `json:"green"`
	Blue  float64 `json:"blue"`
}

type rawColor struct {
	RGB *rawRGB `json:"rgbColor"`
}

type rawSolid struct {
	Color *rawColor `json:"color"`
}

type rawFill struct {
	Solid         *rawSolid `json:"solidFill"`
	PropertyState string    `json:"propertyState"`
}

func (f *rawFill) rgb() *RGB {
	if f == nil || f.PropertyState == "NOT_RENDERED" || f.Solid == nil || f.Solid.Color == nil || f.Solid.Color.RGB == nil {
		return nil
	}
	c := f.Solid.Color.RGB
	return &RGB{c.Red, c.Green, c.Blue}
}

type rawTextElement struct {
	ParagraphMarker *struct {
		Style struct {
			Alignment string `json:"alignment"`
		} `json:"style"`
	} `json:"paragraphMarker"`
	TextRun *struct {
		Content string `json:"content"`
		Style   struct {
			Bold            bool    `json:"bold"`
			FontSize        *rawDim `json:"fontSize"`
			ForegroundColor *struct {
				Opaque *rawColor `json:"opaqueColor"`
			} `json:"foregroundColor"`
		} `json:"style"`
	} `json:"textRun"`
}

type rawPageElement struct {
	ObjectID    string `json:"objectId"`
	Description string `json:"description"`
	Size        *struct {
		Width  *rawDim `json:"width"`
		Height *rawDim `json:"height"`
	} `json:"size"`
	Transform *struct {
		ScaleX     float64 `json:"scaleX"`
		ScaleY     float64 `json:"scaleY"`
		TranslateX float64 `json:"translateX"`
		TranslateY float64 `json:"translateY"`
		Unit       string  `json:"unit"`
	} `json:"transform"`
	Shape *struct {
		ShapeType       string `json:"shapeType"`
		ShapeProperties *struct {
			Background *rawFill `json:"shapeBackgroundFill"`
			Outline    *struct {
				OutlineFill   *rawFill `json:"outlineFill"`
				Weight        *rawDim  `json:"weight"`
				PropertyState string   `json:"propertyState"`
			} `json:"outline"`
		} `json:"shapeProperties"`
		Text *struct {
			TextElements []rawTextElement `json:"textElements"`
		} `json:"text"`
	} `json:"shape"`
	Line *struct {
		LineProperties *struct {
			LineFill  *rawFill `json:"lineFill"`
			Weight    *rawDim  `json:"weight"`
			DashStyle string   `json:"dashStyle"`
		} `json:"lineProperties"`
	} `json:"line"`
	Image        json.RawMessage `json:"image"`
	ElementGroup json.RawMessage `json:"elementGroup"`
	Table        json.RawMessage `json:"table"`
}

type rawPresentation struct {
	PresentationID string `json:"presentationId"`
	RevisionID     string `json:"revisionId"`
	PageSize       *struct {
		Width  *rawDim `json:"width"`
		Height *rawDim `json:"height"`
	} `json:"pageSize"`
	Slides []struct {
		ObjectID     string           `json:"objectId"`
		PageElements []rawPageElement `json:"pageElements"`
	} `json:"slides"`
}

// ParseDeck: presentations.get の生 JSON から slideNo(1 始まり) のページを構築。
// description が TagPrefix で始まれば Tag、LegacyGeorefPrefix なら map_bg の Tag に変換。
// objectId が LegacyMaskPrefix の要素はタグ無しでも Tag{Layer:"map",Kind:"mask"}。
// 壊れた kisei: タグは無視（base＝読み取り専用として扱う）。
func ParseDeck(raw []byte, slideNo int) (*Deck, error) {
	var p rawPresentation
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("parse presentation: %w", err)
	}
	if slideNo < 1 || slideNo > len(p.Slides) {
		return nil, fmt.Errorf("slide %d: %w (deck has %d slides)", slideNo, ErrNotFound, len(p.Slides))
	}
	s := p.Slides[slideNo-1]
	d := &Deck{
		PresID: p.PresentationID,
		Rev:    p.RevisionID,
		PageID: s.ObjectID,
		Owners: map[string]string{},
	}
	if p.PageSize != nil {
		d.PageW = p.PageSize.Width.emu() / EMUPerMM
		d.PageH = p.PageSize.Height.emu() / EMUPerMM
	}
	for i, re := range s.PageElements {
		d.Elements = append(d.Elements, parseElement(i, &re))
	}
	return d, nil
}

func parseElement(i int, re *rawPageElement) Element {
	e := Element{Index: i, ObjectID: re.ObjectID}
	switch {
	case len(re.Image) > 0:
		e.Kind = "image"
	case re.Shape != nil:
		e.Kind = "shape"
	case re.Line != nil:
		e.Kind = "line"
	case len(re.ElementGroup) > 0:
		e.Kind = "group"
	case len(re.Table) > 0:
		e.Kind = "table"
	default:
		e.Kind = "other"
	}
	if re.Size != nil {
		e.SizeEMU = [2]float64{re.Size.Width.emu(), re.Size.Height.emu()}
	}
	if t := re.Transform; t != nil {
		k := 1.0
		if t.Unit == "PT" {
			k = emuPerPt
		}
		w := t.ScaleX * e.SizeEMU[0]
		h := t.ScaleY * e.SizeEMU[1]
		x := t.TranslateX * k
		y := t.TranslateY * k
		e.Flip = [2]bool{t.ScaleX < 0, t.ScaleY < 0}
		e.Box = Box{
			X: (x + math.Min(0, w)) / EMUPerMM,
			Y: (y + math.Min(0, h)) / EMUPerMM,
			W: math.Abs(w) / EMUPerMM,
			H: math.Abs(h) / EMUPerMM,
		}
	}
	if re.Shape != nil {
		e.Shape = parseShapeStyle(re)
		e.Text = e.Shape.Text
	}
	if re.Line != nil && re.Line.LineProperties != nil {
		lp := re.Line.LineProperties
		ls := &LineStyle{Weight: lp.Weight.emu() / emuPerPt, Dash: lp.DashStyle}
		if c := lp.LineFill.rgb(); c != nil {
			ls.Color = *c
		}
		e.Line = ls
	}
	e.Tag = parseTag(re.Description)
	if e.Tag == nil && strings.HasPrefix(re.ObjectID, LegacyMaskPrefix) {
		e.Tag = &Tag{Layer: LayerMap, Kind: "mask"}
	}
	return e
}

func parseShapeStyle(re *rawPageElement) *ShapeStyle {
	sh := re.Shape
	st := &ShapeStyle{ShapeType: sh.ShapeType}
	if pp := sh.ShapeProperties; pp != nil {
		st.Fill = pp.Background.rgb()
		if o := pp.Outline; o != nil && o.PropertyState != "NOT_RENDERED" {
			st.Outline = o.OutlineFill.rgb()
			if st.Outline != nil {
				st.OutlinePt = o.Weight.emu() / emuPerPt
			}
		}
	}
	if sh.Text != nil {
		var b strings.Builder
		styled := false
		for _, te := range sh.Text.TextElements {
			if pm := te.ParagraphMarker; pm != nil && pm.Style.Alignment == "CENTER" {
				st.AlignCenter = true
			}
			tr := te.TextRun
			if tr == nil {
				continue
			}
			b.WriteString(tr.Content)
			if !styled && strings.TrimSpace(tr.Content) != "" {
				styled = true
				st.Bold = tr.Style.Bold
				if tr.Style.FontSize != nil {
					st.FontPt = tr.Style.FontSize.emu() / emuPerPt
				}
				if fc := tr.Style.ForegroundColor; fc != nil && fc.Opaque != nil && fc.Opaque.RGB != nil {
					c := fc.Opaque.RGB
					st.FontColor = RGB{c.Red, c.Green, c.Blue}
				}
			}
		}
		st.Text = strings.TrimSpace(b.String())
	}
	return st
}

// parseTag: kisei:{Tag} または旧 python 試作 kisei_live:{georef}（GeorefTag と同じキー）。
func parseTag(desc string) *Tag {
	switch {
	case strings.HasPrefix(desc, TagPrefix):
		var t Tag
		if err := json.Unmarshal([]byte(desc[len(TagPrefix):]), &t); err != nil || t.Layer == "" {
			return nil
		}
		return &t
	case strings.HasPrefix(desc, LegacyGeorefPrefix):
		var g GeorefTag
		if err := json.Unmarshal([]byte(desc[len(LegacyGeorefPrefix):]), &g); err != nil || g.Z == 0 {
			return nil
		}
		return &Tag{Layer: LayerMap, Kind: "map_bg", Georef: &g}
	}
	return nil
}

// EncodeTag: description に書く文字列（TagPrefix + JSON）。
func EncodeTag(t *Tag) (string, error) {
	if t == nil {
		return "", nil
	}
	b, err := json.Marshal(t)
	if err != nil {
		return "", err
	}
	return TagPrefix + string(b), nil
}

// Get: ref は index（"12"）か objectId。完全一致のみ（部分一致・文字検索はしない）。
func (d *Deck) Get(ref string) (*Element, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, ErrNotFound
	}
	if isDigits(ref) {
		i, err := strconv.Atoi(ref)
		if err != nil || i < 0 || i >= len(d.Elements) {
			return nil, fmt.Errorf("index %s: %w", ref, ErrNotFound)
		}
		return &d.Elements[i], nil
	}
	for i := range d.Elements {
		if d.Elements[i].ObjectID == ref {
			return &d.Elements[i], nil
		}
	}
	return nil, fmt.Errorf("objectId %s: %w", ref, ErrNotFound)
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

var layerNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,31}$`)

// ValidLayerName: objectId（kc_<layer>_<8hex>）に入れられる名前か。
func ValidLayerName(layer string) bool { return layerNameRe.MatchString(layer) }

// owner: layer の実効オーナー。claim 情報（Owners）優先、無ければ要素タグの owner。
func (d *Deck) owner(layer string) string {
	if a := d.Owners[layer]; a != "" {
		return a
	}
	for i := range d.Elements {
		if t := d.Elements[i].Tag; t != nil && t.Layer == layer && t.Owner != "" {
			return t.Owner
		}
	}
	return ""
}

// Claim: layer を agent が所有。既に他 agent なら ErrNotOwner。"base" "_cursor" "map" は ErrReserved。
// Owners が空（再起動直後など）でも、要素タグの owner が他 agent なら ErrNotOwner。
// 同じ agent の再 claim は成功（冪等）。
func (d *Deck) Claim(layer, agent string) error {
	if IsReservedLayer(layer) {
		return fmt.Errorf("claim %s: %w", layer, ErrReserved)
	}
	if !ValidLayerName(layer) {
		return fmt.Errorf("claim %q: invalid layer name ([A-Za-z0-9][A-Za-z0-9_-]*, <=32)", layer)
	}
	if agent == "" {
		return errors.New("claim: agent required")
	}
	if o := d.owner(layer); o != "" && o != agent {
		return fmt.Errorf("claim %s (owner %s): %w", layer, o, ErrNotOwner)
	}
	if d.Owners == nil {
		d.Owners = map[string]string{}
	}
	d.Owners[layer] = agent
	return nil
}

// CanWrite: e を agent が書き換えてよいか。
// base（タグ無し）は ErrReadOnly、予約レイヤ（map / _cursor）は ErrReserved、
// 実効オーナー（Owners→タグ owner）が agent でなければ ErrNotOwner（未 claim のレイヤも ErrNotOwner）。
func (d *Deck) CanWrite(e *Element, agent string) error {
	l := e.Layer()
	if l == LayerBase {
		return ErrReadOnly
	}
	if IsReservedLayer(l) {
		return ErrReserved
	}
	if agent == "" || d.owner(l) != agent {
		return ErrNotOwner
	}
	return nil
}

// CanWriteSystem: daemon 内部（view コマンドの map、pick の _cursor）が e を書き換えてよいか。
// 予約レイヤ map / _cursor の要素のみ可。base は ErrReadOnly、agent レイヤは ErrNotOwner。
func (d *Deck) CanWriteSystem(e *Element) error {
	switch l := e.Layer(); {
	case l == LayerBase:
		return ErrReadOnly
	case l == LayerMap || l == LayerCursor:
		return nil
	default:
		return ErrNotOwner
	}
}

// MapBG: georef 付き背景画像。無ければ ErrNotFound。
func (d *Deck) MapBG() (*Element, *GeorefTag, error) {
	for i := range d.Elements {
		e := &d.Elements[i]
		if e.Tag != nil && e.Tag.Kind == "map_bg" && e.Tag.Georef != nil && e.Box.W > 0 && e.Box.H > 0 {
			return e, e.Tag.Georef, nil
		}
	}
	return nil, nil, fmt.Errorf("map_bg with georef: %w", ErrNotFound)
}

// Geo: 背景画像の georef と現在の紙面上の箱（geo 型）。
func (d *Deck) Geo() (geo.Georef, geo.Box, error) {
	e, g, err := d.MapBG()
	if err != nil {
		return geo.Georef{}, geo.Box{}, err
	}
	return geo.Georef{Z: g.Z, CenterLat: g.Center[0], CenterLon: g.Center[1], PxW: g.Px[0], PxH: g.Px[1]},
		geo.Box{X: e.Box.X, Y: e.Box.Y, W: e.Box.W, H: e.Box.H}, nil
}

// ToLatLon: 紙面 mm → 緯度経度（georef 必須）。
func (d *Deck) ToLatLon(x, y float64) (lat, lon float64, err error) {
	g, b, err := d.Geo()
	if err != nil {
		return 0, 0, err
	}
	lat, lon = g.ToLatLon(b, x, y)
	return lat, lon, nil
}

// MetersBetween: 紙面上 2 点間の実距離 m（georef 必須）。
func (d *Deck) MetersBetween(x1, y1, x2, y2 float64) (float64, error) {
	g, b, err := d.Geo()
	if err != nil {
		return 0, err
	}
	return g.MetersBetween(b, x1, y1, x2, y2), nil
}

func parsePair(s string) (a, b float64, err error) {
	p := strings.Split(s, ",")
	if len(p) != 2 {
		return 0, 0, fmt.Errorf("want two numbers a,b: %q", s)
	}
	if a, err = strconv.ParseFloat(strings.TrimSpace(p[0]), 64); err != nil {
		return 0, 0, fmt.Errorf("bad number %q", p[0])
	}
	if b, err = strconv.ParseFloat(strings.TrimSpace(p[1]), 64); err != nil {
		return 0, 0, fmt.Errorf("bad number %q", p[1])
	}
	return a, b, nil
}

// ResolvePoint: "lat,lon" | "site:N" | "mm:x,y" を紙面 mm に。
func (d *Deck) ResolvePoint(s string) (x, y float64, err error) {
	s = strings.TrimSpace(s)
	switch {
	case strings.HasPrefix(s, "mm:"):
		return parsePair(s[3:])
	case strings.HasPrefix(s, "site:"):
		n := strings.TrimSpace(s[5:])
		_, gt, err := d.MapBG()
		if err != nil {
			return 0, 0, fmt.Errorf("point %q: %w", s, err)
		}
		ll, ok := gt.Sites[n]
		if !ok {
			return 0, 0, fmt.Errorf("point %q: site %s %w in georef", s, n, ErrNotFound)
		}
		g, b, err := d.Geo()
		if err != nil {
			return 0, 0, err
		}
		x, y = g.ToMM(b, ll[0], ll[1])
		return x, y, nil
	default:
		lat, lon, perr := parsePair(s)
		if perr != nil {
			return 0, 0, fmt.Errorf("point %q: want lat,lon | site:N | mm:x,y", s)
		}
		if lat < -85 || lat > 85 || lon < -180 || lon > 180 {
			return 0, 0, fmt.Errorf("point %q: lat/lon out of range", s)
		}
		g, b, gerr := d.Geo()
		if gerr != nil {
			return 0, 0, fmt.Errorf("point %q: %w", s, gerr)
		}
		x, y = g.ToMM(b, lat, lon)
		return x, y, nil
	}
}

// ResolveLength: "40m"（georef の横縮尺で mm に換算）| "12mm"。単位必須。
func (d *Deck) ResolveLength(s string) (mm float64, err error) {
	s = strings.TrimSpace(s)
	var unit string
	switch {
	case strings.HasSuffix(s, "mm"):
		unit, s = "mm", s[:len(s)-2]
	case strings.HasSuffix(s, "m"):
		unit, s = "m", s[:len(s)-1]
	default:
		return 0, fmt.Errorf("length %q: unit required (m | mm)", s)
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, fmt.Errorf("length %q: bad number", s+unit)
	}
	if unit == "mm" {
		return v, nil
	}
	g, b, err := d.Geo()
	if err != nil {
		return 0, fmt.Errorf("length %q: %w", s+unit, err)
	}
	return v * g.MMPerMeter(b), nil
}
