package model

// Op は parts / cp が生成し、slides アダプタだけが batchUpdate へ変換する抽象操作。
// 座標・寸法はすべて紙面 mm（A3 横、原点左上）。色は 0..1 の RGB。
type OpKind string

const (
	OpCreateShape  OpKind = "create_shape"  // 矩形・楕円＋任意の文字
	OpCreateLine   OpKind = "create_line"   // 直線 1 本（X1,Y1 → X2,Y2）
	OpDuplicate    OpKind = "duplicate"     // SourceID を複製し Box に置く（凡例の看板画像など）
	OpTransform    OpKind = "transform"     // TargetID を Box（絶対位置・寸法）へ
	OpSetText      OpKind = "set_text"      // TargetID の文字を丸ごと Text に
	OpDelete       OpKind = "delete"        // TargetID を削除
	OpSetTag       OpKind = "set_tag"       // TargetID の description に Tag を書く
	OpZOrderBack   OpKind = "zorder_back"   // TargetIDs を最背面へ（与えた順に重ねる）
)

type RGB struct{ R, G, B float64 }

type Box struct{ X, Y, W, H float64 }

type ShapeStyle struct {
	ShapeType   string  // "RECTANGLE" | "ELLIPSE"
	Fill        *RGB    // nil = 塗りなし
	Outline     *RGB    // nil = 線なし
	OutlinePt   float64 //
	Text        string
	FontPt      float64
	FontColor   RGB
	Bold        bool
	AlignCenter bool
}

type LineStyle struct {
	Color  RGB
	Weight float64 // pt
	Dash   string  // "SOLID" | "DASH"
}

type Op struct {
	Kind      OpKind
	ObjectID  string   // 作成時に付ける ID（kc_<layer>_<8hex>）
	TargetID  string   // 既存要素
	TargetIDs []string // ZOrder 用
	SourceID  string   // Duplicate 元
	Box       Box
	X1, Y1    float64
	X2, Y2    float64
	Shape     *ShapeStyle
	Line      *LineStyle
	Text      string
	Tag       *Tag // 作成時・SetTag 時に description へ
}

// Tag は要素の description に `kisei:` + JSON で保存する。
type Tag struct {
	Layer  string         `json:"layer"`
	Owner  string         `json:"owner,omitempty"`
	Kind   string         `json:"kind"`
	Params map[string]any `json:"params,omitempty"`
	Georef *GeorefTag     `json:"georef,omitempty"`
}

type GeorefTag struct {
	Z      int                   `json:"z"`
	Center [2]float64            `json:"center"`
	Px     [2]int                `json:"px"`
	Sites  map[string][2]float64 `json:"sites,omitempty"`
}

const TagPrefix = "kisei:"
const LegacyGeorefPrefix = "kisei_live:" // 旧 python 試作の georef（{"z","center","px","sites"}）
