// Package parts: 部品（site/sign/cone_line/guard/closure/detour/note）を抽象 Op 列に展開する。
// 基準値（コーン間隔など）はここに埋め込む。AICAD AgentParts.kt の Slides 版。純関数のみ。
package parts

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/YuujiKamura/AISLIDE/internal/model"
)

const (
	ConePitchMinM = 3.0
	ConePitchMaxM = 5.0
	NoteMaxRunes  = 20
)

// 基準寸法・色（紙面 mm / pt / RGB 0..1）。
const (
	ConePitchDefaultM = 4.0
	ConeDiaMM         = 2.2
	SiteRadiusMM      = 5.0
	SiteFontPt        = 14
	SiteOutlinePt     = 1.5
	SignWidthMM       = 12.0
	SignBadgeDiaMM    = 4.0
	SignBadgeFontPt   = 7
	GuardDiaMM        = 5.0
	GuardFontPt       = 10
	ClosurePt         = 4.0
	DetourPt          = 3.0
	NoteFontPt        = 10
	NoteHeightMM      = 6.0

	// 凡例（看板セット）の位置: ラベルは y>LegendLabelMinY、看板画像は y が LegendImageMinY..LegendImageMaxY。
	LegendLabelMinY = 220.0
	LegendImageMinY = 225.0
	LegendImageMaxY = 275.0
)

var (
	ColorRed    = model.RGB{R: 0.85, G: 0, B: 0}
	ColorWhite  = model.RGB{R: 1, G: 1, B: 1}
	ColorBlack  = model.RGB{R: 0, G: 0, B: 0}
	ColorBlue   = model.RGB{R: 0, G: 0.35, B: 0.85}
	ColorOrange = model.RGB{R: 0.95, G: 0.35, B: 0.1}
)

const mmPerPt = 25.4 / 72

type Ctx struct {
	Deck  *model.Deck
	Layer string
	Agent string
	NewID func(layer string) string // kc_<layer>_<8hex>。テストでは決定的な関数を渡す
}

type builder func(args map[string]string, ctx Ctx) ([]model.Op, error)

var builders = map[string]builder{
	"site":      buildSite,
	"sign":      buildSign,
	"cone_line": buildConeLine,
	"guard":     buildGuard,
	"closure":   func(a map[string]string, c Ctx) ([]model.Op, error) { return buildPath("closure", a, c) },
	"detour":    func(a map[string]string, c Ctx) ([]model.Op, error) { return buildPath("detour", a, c) },
	"note":      buildNote,
}

// Build: kind と k=v 引数から Op 列。作成する全要素に Tag{Layer,Owner,Kind,Params} を付ける。
// Params は args の写し。予約レイヤ（base/_cursor/map）には置けない。所有権の確認は呼び出し側（cp）。
func Build(kind string, args map[string]string, ctx Ctx) ([]model.Op, error) {
	b, ok := builders[kind]
	if !ok {
		return nil, fmt.Errorf("unknown kind %q (kinds: %s)", kind, strings.Join(Kinds(), ", "))
	}
	if ctx.Deck == nil || ctx.NewID == nil {
		return nil, errors.New("parts: ctx.Deck and ctx.NewID required")
	}
	if ctx.Agent == "" {
		return nil, errors.New("agent required")
	}
	if ctx.Layer == "" || model.IsReservedLayer(ctx.Layer) {
		return nil, fmt.Errorf("layer %q: %w", ctx.Layer, model.ErrReserved)
	}
	if args == nil {
		args = map[string]string{}
	}
	ops, err := b(args, ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", kind, err)
	}
	params := make(map[string]any, len(args))
	for k, v := range args {
		params[k] = v
	}
	for i := range ops {
		ops[i].ObjectID = ctx.NewID(ctx.Layer)
		ops[i].Tag = &model.Tag{Layer: ctx.Layer, Owner: ctx.Agent, Kind: kind, Params: params}
	}
	return ops, nil
}

func Kinds() []string {
	ks := make([]string, 0, len(builders))
	for k := range builders {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func need(args map[string]string, keys ...string) error {
	for _, k := range keys {
		if strings.TrimSpace(args[k]) == "" {
			return fmt.Errorf("%s= required", k)
		}
	}
	return nil
}

func point(ctx Ctx, s string) (x, y float64, err error) { return ctx.Deck.ResolvePoint(s) }

func circle(cx, cy, dia float64, st model.ShapeStyle) model.Op {
	st.ShapeType = "ELLIPSE"
	return model.Op{Kind: model.OpCreateShape, Box: model.Box{X: cx - dia/2, Y: cy - dia/2, W: dia, H: dia}, Shape: &st}
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

// site n=<番号> at=<pt>: 赤丸白数字。中は数字だけ（丸囲み文字は使わない）。
func buildSite(args map[string]string, ctx Ctx) ([]model.Op, error) {
	if err := need(args, "n", "at"); err != nil {
		return nil, err
	}
	n := strings.TrimSpace(args["n"])
	if !isDigits(n) || len(n) > 3 {
		return nil, fmt.Errorf("n=%q: digits only (1..999, no circled numbers)", n)
	}
	x, y, err := point(ctx, args["at"])
	if err != nil {
		return nil, err
	}
	white := ColorWhite
	red := ColorRed
	return []model.Op{circle(x, y, 2*SiteRadiusMM, model.ShapeStyle{
		Fill: &red, Outline: &white, OutlinePt: SiteOutlinePt,
		Text: n, FontPt: SiteFontPt, FontColor: ColorWhite, Bold: true, AlignCenter: true,
	})}, nil
}

// 凡例番号 ①..⑳ <-> 数字。
func keyRune(key string) (rune, int, error) {
	key = strings.TrimSpace(key)
	if isDigits(key) {
		n, _ := strconv.Atoi(key)
		if n < 1 || n > 20 {
			return 0, 0, fmt.Errorf("key=%s: 1..20", key)
		}
		return rune('①' + n - 1), n, nil
	}
	r, size := utf8.DecodeRuneInString(key)
	if size != len(key) || r < '①' || r > '⑳' {
		return 0, 0, fmt.Errorf("key=%q: want ①..⑳ (or 1..20)", key)
	}
	return r, int(r-'①') + 1, nil
}

// FindLegendSign: 凡例ラベル（下帯 y>220mm、文字が key で始まる shape）の真上にある看板画像。
// 画像は y が 225..275mm、ラベルより上にあり、横中心がラベル中心に最も近いもの。
func FindLegendSign(d *model.Deck, key rune) (*model.Element, error) {
	var label *model.Element
	for i := range d.Elements {
		e := &d.Elements[i]
		if e.Kind == "shape" && e.Box.Y > LegendLabelMinY && strings.HasPrefix(e.Text, string(key)) {
			label = e
			break
		}
	}
	if label == nil {
		return nil, fmt.Errorf("legend label %c: %w", key, model.ErrNotFound)
	}
	lx, _ := label.Center()
	var best *model.Element
	bestD := math.Inf(1)
	for i := range d.Elements {
		e := &d.Elements[i]
		if e.Kind != "image" || e.Box.Y < LegendImageMinY || e.Box.Y > LegendImageMaxY || e.Box.Y >= label.Box.Y || e.Box.W <= 0 {
			continue
		}
		cx, _ := e.Center()
		if dd := math.Abs(cx - lx); dd < bestD {
			best, bestD = e, dd
		}
	}
	if best == nil {
		return nil, fmt.Errorf("legend image above %c: %w", key, model.ErrNotFound)
	}
	return best, nil
}

// sign key=<①..> at=<pt> [w=12mm]: 凡例の看板画像を複製し、右肩に番号バッジ。
func buildSign(args map[string]string, ctx Ctx) ([]model.Op, error) {
	if err := need(args, "key", "at"); err != nil {
		return nil, err
	}
	r, n, err := keyRune(args["key"])
	if err != nil {
		return nil, err
	}
	src, err := FindLegendSign(ctx.Deck, r)
	if err != nil {
		return nil, err
	}
	x, y, err := point(ctx, args["at"])
	if err != nil {
		return nil, err
	}
	w := SignWidthMM
	if s := strings.TrimSpace(args["w"]); s != "" {
		if w, err = ctx.Deck.ResolveLength(s); err != nil {
			return nil, err
		}
		if w <= 0 || w > 100 {
			return nil, fmt.Errorf("w=%s: 0 < w <= 100mm", s)
		}
	}
	h := w * src.Box.H / src.Box.W
	box := model.Box{X: x - w/2, Y: y - h/2, W: w, H: h}
	white := ColorWhite
	red := ColorRed
	return []model.Op{
		{Kind: model.OpDuplicate, SourceID: src.ObjectID, Box: box},
		circle(box.X+box.W, box.Y, SignBadgeDiaMM, model.ShapeStyle{
			Fill: &red, Outline: &white, OutlinePt: 0.75,
			Text: strconv.Itoa(n), FontPt: SignBadgeFontPt, FontColor: ColorWhite, Bold: true, AlignCenter: true,
		}),
	}, nil
}

func parsePitch(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return ConePitchDefaultM, nil
	}
	if strings.HasSuffix(s, "mm") {
		return 0, fmt.Errorf("pitch=%s: real meters only (e.g. 4m)", s)
	}
	v, err := strconv.ParseFloat(strings.TrimSuffix(s, "m"), 64)
	if err != nil {
		return 0, fmt.Errorf("pitch=%s: bad number", s)
	}
	if v < ConePitchMinM || v > ConePitchMaxM {
		return 0, fmt.Errorf("pitch=%s: must be %g..%gm", s, ConePitchMinM, ConePitchMaxM)
	}
	return v, nil
}

// ConeSegments: 実長 lengthM を pitch 目安で等分する区間数。実間隔は必ず 5m 以下にし、
// その範囲でできるだけ 3m 以上にする（両立しない長さ、例 5.5m は 5m 以下を優先して 2.75m 間隔）。
func ConeSegments(lengthM, pitch float64) int {
	n := int(math.Round(lengthM / pitch))
	if n < 1 {
		n = 1
	}
	for lengthM/float64(n) > ConePitchMaxM {
		n++
	}
	for n > 1 && lengthM/float64(n) < ConePitchMinM && lengthM/float64(n-1) <= ConePitchMaxM {
		n--
	}
	return n
}

// cone_line from=<pt> to=<pt> [pitch=4m]: 実距離を等間隔に区切り、両端を含むコーンを並べる。
func buildConeLine(args map[string]string, ctx Ctx) ([]model.Op, error) {
	if err := need(args, "from", "to"); err != nil {
		return nil, err
	}
	pitch, err := parsePitch(args["pitch"])
	if err != nil {
		return nil, err
	}
	x1, y1, err := point(ctx, args["from"])
	if err != nil {
		return nil, err
	}
	x2, y2, err := point(ctx, args["to"])
	if err != nil {
		return nil, err
	}
	lengthM, err := ctx.Deck.MetersBetween(x1, y1, x2, y2)
	if err != nil {
		return nil, fmt.Errorf("real length needs georef: %w", err)
	}
	if lengthM < 0.01 {
		return nil, errors.New("from and to are the same point")
	}
	n := ConeSegments(lengthM, pitch)
	orange := ColorOrange
	ops := make([]model.Op, 0, n+1)
	for i := 0; i <= n; i++ {
		t := float64(i) / float64(n)
		ops = append(ops, circle(x1+(x2-x1)*t, y1+(y2-y1)*t, ConeDiaMM, model.ShapeStyle{Fill: &orange}))
	}
	return ops, nil
}

// guard k=A|B at=<pt>: 誘導員（青丸＋文字）。
func buildGuard(args map[string]string, ctx Ctx) ([]model.Op, error) {
	if err := need(args, "k", "at"); err != nil {
		return nil, err
	}
	k := strings.ToUpper(strings.TrimSpace(args["k"]))
	if k != "A" && k != "B" {
		return nil, fmt.Errorf("k=%s: A or B", args["k"])
	}
	x, y, err := point(ctx, args["at"])
	if err != nil {
		return nil, err
	}
	blue := ColorBlue
	return []model.Op{circle(x, y, GuardDiaMM, model.ShapeStyle{
		Fill: &blue, Text: k, FontPt: GuardFontPt, FontColor: ColorWhite, Bold: true, AlignCenter: true,
	})}, nil
}

// closure / detour path=<pt>;<pt>;...: 区間ごとに直線 1 本。
func buildPath(kind string, args map[string]string, ctx Ctx) ([]model.Op, error) {
	if err := need(args, "path"); err != nil {
		return nil, err
	}
	var pts [][2]float64
	for _, s := range strings.Split(args["path"], ";") {
		if strings.TrimSpace(s) == "" {
			continue
		}
		x, y, err := point(ctx, s)
		if err != nil {
			return nil, err
		}
		pts = append(pts, [2]float64{x, y})
	}
	if len(pts) < 2 {
		return nil, errors.New("path= needs at least 2 points (a;b;...)")
	}
	style := model.LineStyle{Color: ColorRed, Weight: ClosurePt, Dash: "SOLID"}
	if kind == "detour" {
		style = model.LineStyle{Color: ColorBlue, Weight: DetourPt, Dash: "DASH"}
	}
	ops := make([]model.Op, 0, len(pts)-1)
	for i := 1; i < len(pts); i++ {
		st := style
		ops = append(ops, model.Op{Kind: model.OpCreateLine,
			X1: pts[i-1][0], Y1: pts[i-1][1], X2: pts[i][0], Y2: pts[i][1], Line: &st})
	}
	return ops, nil
}

// note at=<pt> t=<文字>: 短文注記（20 文字超は ng）。文字枠は at を中心に置く。
func buildNote(args map[string]string, ctx Ctx) ([]model.Op, error) {
	if err := need(args, "at", "t"); err != nil {
		return nil, err
	}
	t := strings.TrimSpace(args["t"])
	n := utf8.RuneCountInString(t)
	if n > NoteMaxRunes {
		return nil, fmt.Errorf("t= is %d chars (max %d)", n, NoteMaxRunes)
	}
	if strings.ContainsAny(t, "\r\n") {
		return nil, errors.New("t= must be one line")
	}
	x, y, err := point(ctx, args["at"])
	if err != nil {
		return nil, err
	}
	w := float64(n)*NoteFontPt*mmPerPt + 4
	return []model.Op{{Kind: model.OpCreateShape,
		Box: model.Box{X: x - w/2, Y: y - NoteHeightMM/2, W: w, H: NoteHeightMM},
		Shape: &model.ShapeStyle{ShapeType: "RECTANGLE", Text: t, FontPt: NoteFontPt,
			FontColor: ColorBlack, AlignCenter: true}}}, nil
}

// ParseArgs: AICAD 互換の `k=v k="a b"` パーサ。
// 空白区切り。値は "..." で囲めば空白を含められ、\" と \\ でエスケープ。`=` の無い語は無視。
// 同じキーは後勝ち。
func ParseArgs(s string) map[string]string {
	out := map[string]string{}
	rs := []rune(s)
	i := 0
	for i < len(rs) {
		for i < len(rs) && isSpace(rs[i]) {
			i++
		}
		if i >= len(rs) {
			break
		}
		var key strings.Builder
		for i < len(rs) && rs[i] != '=' && !isSpace(rs[i]) {
			key.WriteRune(rs[i])
			i++
		}
		if i >= len(rs) || rs[i] != '=' {
			continue // = の無い語
		}
		i++ // '='
		var val strings.Builder
		if i < len(rs) && rs[i] == '"' {
			i++
			for i < len(rs) && rs[i] != '"' {
				if rs[i] == '\\' && i+1 < len(rs) && (rs[i+1] == '"' || rs[i+1] == '\\') {
					i++
				}
				val.WriteRune(rs[i])
				i++
			}
			i++ // 閉じ '"'（無ければ末尾まで）
		} else {
			for i < len(rs) && !isSpace(rs[i]) {
				val.WriteRune(rs[i])
				i++
			}
		}
		if key.Len() > 0 {
			out[key.String()] = val.String()
		}
	}
	return out
}

func isSpace(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' }
