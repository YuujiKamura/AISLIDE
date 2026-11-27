package slides

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/YuujiKamura/AISLIDE/internal/model"
	sl "google.golang.org/api/slides/v1"
)

// EMUPerMM: 1 mm = 36000 EMU。
const EMUPerMM = 36000.0

// minMM: 0 寸法は Slides が受け付けない（非可逆 transform）ので下限を設ける。
const minMM = 0.01

// FontFamily: 作成する文字の既定フォント。
const FontFamily = "Meiryo UI"

func emu(mm float64) float64 { return mm * EMUPerMM }

func atLeast(v float64) float64 {
	v = math.Abs(v)
	if v < minMM {
		return minMM
	}
	return v
}

func rgb(c model.RGB) *sl.OpaqueColor {
	return &sl.OpaqueColor{RgbColor: &sl.RgbColor{
		Red: c.R, Green: c.G, Blue: c.B,
		ForceSendFields: []string{"Red", "Green", "Blue"},
	}}
}

func solid(c model.RGB) *sl.SolidFill { return &sl.SolidFill{Color: rgb(c)} }

func dimPT(pt float64) *sl.Dimension { return &sl.Dimension{Magnitude: pt, Unit: "PT"} }

// TagDescription: Tag を description 文字列（"kisei:" + JSON）に。HTML エスケープしない。
func TagDescription(t *model.Tag) (string, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(t); err != nil {
		return "", err
	}
	return model.TagPrefix + strings.TrimRight(b.String(), "\n"), nil
}

func altText(id string, t *model.Tag) (*sl.Request, error) {
	desc, err := TagDescription(t)
	if err != nil {
		return nil, fmt.Errorf("tag %s: %w", id, err)
	}
	return &sl.Request{UpdatePageElementAltText: &sl.UpdatePageElementAltTextRequest{
		ObjectId: id, Description: desc,
	}}, nil
}

// known: ToRequests 中に参照する要素の状態（deck の要素 + 同じバッチで作ったもの）。
type known struct {
	sizeEMU [2]float64
	flip    [2]bool
	hasText bool
}

type state map[string]*known

func newState(d *model.Deck) state {
	s := state{}
	if d == nil {
		return s
	}
	for i := range d.Elements {
		e := &d.Elements[i]
		s[e.ObjectID] = &known{sizeEMU: e.SizeEMU, flip: e.Flip, hasText: e.Text != ""}
	}
	return s
}

func (s state) get(id string) (*known, error) {
	if id == "" {
		return nil, fmt.Errorf("empty object id")
	}
	k, ok := s[id]
	if !ok {
		return nil, fmt.Errorf("%s: %w", id, model.ErrNotFound)
	}
	return k, nil
}

// absTransform: 要素 k を Box（外接矩形 mm）へ置く ABSOLUTE transform。
// flip の軸は scale を負にし、translate を Box の反対端に置く（局所 box が負方向へ伸びる）。
func absTransform(id string, k *known, b model.Box) (*sl.Request, error) {
	if k.sizeEMU[0] <= 0 || k.sizeEMU[1] <= 0 {
		return nil, fmt.Errorf("%s: unknown size (SizeEMU=%v)", id, k.sizeEMU)
	}
	sx := emu(atLeast(b.W)) / k.sizeEMU[0]
	sy := emu(atLeast(b.H)) / k.sizeEMU[1]
	tx, ty := emu(b.X), emu(b.Y)
	if k.flip[0] {
		sx, tx = -sx, emu(b.X+atLeast(b.W))
	}
	if k.flip[1] {
		sy, ty = -sy, emu(b.Y+atLeast(b.H))
	}
	return &sl.Request{UpdatePageElementTransform: &sl.UpdatePageElementTransformRequest{
		ObjectId:  id,
		ApplyMode: "ABSOLUTE",
		Transform: &sl.AffineTransform{
			ScaleX: sx, ScaleY: sy, TranslateX: tx, TranslateY: ty, Unit: "EMU",
			ForceSendFields: []string{"ScaleX", "ScaleY", "ShearX", "ShearY", "TranslateX", "TranslateY"},
		},
	}}, nil
}

// lineGeometry: X1,Y1 → X2,Y2 を size（|dx|,|dy|、下限 0.01mm）と transform に。
//
// Slides の affine: x' = scaleX*x + translateX（shear 0）。直線は局所座標 (0,0)→(W,H)。
// 始点 (0,0) は常に (translateX, translateY) へ写るので translate = (X1, Y1)。
// 終点 (W,H) は (X1 + scaleX*W, Y1 + scaleY*H)。X2 < X1 なら scaleX = -1 で X1 - W = X2。
// 外接矩形は min(X1,X2), min(Y1,Y2), |dx|, |dy| と一致する。
func lineGeometry(x1, y1, x2, y2 float64) (size *sl.Size, tr *sl.AffineTransform) {
	w, h := atLeast(x2-x1), atLeast(y2-y1)
	sx, sy := 1.0, 1.0
	if x2 < x1 {
		sx = -1
	}
	if y2 < y1 {
		sy = -1
	}
	size = &sl.Size{
		Width:  &sl.Dimension{Magnitude: emu(w), Unit: "EMU"},
		Height: &sl.Dimension{Magnitude: emu(h), Unit: "EMU"},
	}
	tr = &sl.AffineTransform{
		ScaleX: sx, ScaleY: sy, TranslateX: emu(x1), TranslateY: emu(y1), Unit: "EMU",
		ForceSendFields: []string{"ScaleX", "ScaleY", "ShearX", "ShearY", "TranslateX", "TranslateY"},
	}
	return size, tr
}

// ToRequests: 純関数。d は Transform の scale 算出（SizeEMU）と SetText の既存文字判定に使う。
// 同じバッチ内で作った要素も後続 Op から参照できる。
func ToRequests(ops []model.Op, pageID string, d *model.Deck) ([]*sl.Request, error) {
	st := newState(d)
	var out []*sl.Request
	add := func(r ...*sl.Request) { out = append(out, r...) }

	for i, op := range ops {
		errf := func(format string, a ...any) error {
			return fmt.Errorf("op[%d] %s: "+format, append([]any{i, op.Kind}, a...)...)
		}
		switch op.Kind {
		case model.OpCreateShape:
			if op.ObjectID == "" || op.Shape == nil {
				return nil, errf("ObjectID and Shape required")
			}
			s := op.Shape
			typ := s.ShapeType
			if typ == "" {
				typ = "RECTANGLE"
			}
			w, h := atLeast(op.Box.W), atLeast(op.Box.H)
			add(&sl.Request{CreateShape: &sl.CreateShapeRequest{
				ObjectId:  op.ObjectID,
				ShapeType: typ,
				ElementProperties: &sl.PageElementProperties{
					PageObjectId: pageID,
					Size: &sl.Size{
						Width:  &sl.Dimension{Magnitude: emu(w), Unit: "EMU"},
						Height: &sl.Dimension{Magnitude: emu(h), Unit: "EMU"},
					},
					Transform: &sl.AffineTransform{
						ScaleX: 1, ScaleY: 1, TranslateX: emu(op.Box.X), TranslateY: emu(op.Box.Y), Unit: "EMU",
						ForceSendFields: []string{"TranslateX", "TranslateY"},
					},
				},
			}})
			props := &sl.ShapeProperties{}
			fields := []string{"shapeBackgroundFill", "outline"}
			if s.Fill != nil {
				props.ShapeBackgroundFill = &sl.ShapeBackgroundFill{SolidFill: solid(*s.Fill)}
			} else {
				props.ShapeBackgroundFill = &sl.ShapeBackgroundFill{PropertyState: "NOT_RENDERED"}
			}
			if s.Outline != nil {
				o := &sl.Outline{OutlineFill: &sl.OutlineFill{SolidFill: solid(*s.Outline)}}
				if s.OutlinePt > 0 {
					o.Weight = dimPT(s.OutlinePt)
				}
				props.Outline = o
			} else {
				props.Outline = &sl.Outline{PropertyState: "NOT_RENDERED"}
			}
			if s.AlignCenter {
				props.ContentAlignment = "MIDDLE"
				fields = append(fields, "contentAlignment")
			}
			add(&sl.Request{UpdateShapeProperties: &sl.UpdateShapePropertiesRequest{
				ObjectId: op.ObjectID, ShapeProperties: props, Fields: strings.Join(fields, ","),
			}})
			if s.Text != "" {
				add(&sl.Request{InsertText: &sl.InsertTextRequest{
					ObjectId: op.ObjectID, Text: s.Text, InsertionIndex: 0,
					ForceSendFields: []string{"InsertionIndex"},
				}})
				ts := &sl.TextStyle{
					Bold:            s.Bold,
					ForegroundColor: &sl.OptionalColor{OpaqueColor: rgb(s.FontColor)},
					FontFamily:      FontFamily,
					ForceSendFields: []string{"Bold"},
				}
				tf := []string{"bold", "foregroundColor", "fontFamily"}
				if s.FontPt > 0 {
					ts.FontSize = dimPT(s.FontPt)
					tf = append([]string{"fontSize"}, tf...)
				}
				add(&sl.Request{UpdateTextStyle: &sl.UpdateTextStyleRequest{
					ObjectId: op.ObjectID, TextRange: &sl.Range{Type: "ALL"},
					Style: ts, Fields: strings.Join(tf, ","),
				}})
				if s.AlignCenter {
					add(&sl.Request{UpdateParagraphStyle: &sl.UpdateParagraphStyleRequest{
						ObjectId: op.ObjectID, TextRange: &sl.Range{Type: "ALL"},
						Style: &sl.ParagraphStyle{Alignment: "CENTER"}, Fields: "alignment",
					}})
				}
			}
			st[op.ObjectID] = &known{sizeEMU: [2]float64{emu(w), emu(h)}, hasText: s.Text != ""}

		case model.OpCreateLine:
			if op.ObjectID == "" || op.Line == nil {
				return nil, errf("ObjectID and Line required")
			}
			size, tr := lineGeometry(op.X1, op.Y1, op.X2, op.Y2)
			add(&sl.Request{CreateLine: &sl.CreateLineRequest{
				ObjectId:     op.ObjectID,
				LineCategory: "STRAIGHT",
				ElementProperties: &sl.PageElementProperties{
					PageObjectId: pageID, Size: size, Transform: tr,
				},
			}})
			dash := op.Line.Dash
			if dash == "" {
				dash = "SOLID"
			}
			lp := &sl.LineProperties{
				LineFill:  &sl.LineFill{SolidFill: solid(op.Line.Color)},
				DashStyle: dash,
			}
			lf := []string{"lineFill", "dashStyle"}
			if op.Line.Weight > 0 {
				lp.Weight = dimPT(op.Line.Weight)
				lf = []string{"lineFill", "weight", "dashStyle"}
			}
			add(&sl.Request{UpdateLineProperties: &sl.UpdateLinePropertiesRequest{
				ObjectId: op.ObjectID, LineProperties: lp, Fields: strings.Join(lf, ","),
			}})
			st[op.ObjectID] = &known{
				sizeEMU: [2]float64{size.Width.Magnitude, size.Height.Magnitude},
				flip:    [2]bool{tr.ScaleX < 0, tr.ScaleY < 0},
			}

		case model.OpDuplicate:
			if op.ObjectID == "" {
				return nil, errf("ObjectID required")
			}
			src, err := st.get(op.SourceID)
			if err != nil {
				return nil, errf("source %w", err)
			}
			add(&sl.Request{DuplicateObject: &sl.DuplicateObjectRequest{
				ObjectId:  op.SourceID,
				ObjectIds: map[string]string{op.SourceID: op.ObjectID},
			}})
			dup := &known{sizeEMU: src.sizeEMU, flip: src.flip, hasText: src.hasText}
			r, err := absTransform(op.ObjectID, dup, op.Box)
			if err != nil {
				return nil, errf("%w", err)
			}
			add(r)
			st[op.ObjectID] = dup

		case model.OpTransform:
			k, err := st.get(op.TargetID)
			if err != nil {
				return nil, errf("%w", err)
			}
			r, err := absTransform(op.TargetID, k, op.Box)
			if err != nil {
				return nil, errf("%w", err)
			}
			add(r)

		case model.OpSetText:
			k, err := st.get(op.TargetID)
			if err != nil {
				return nil, errf("%w", err)
			}
			if k.hasText {
				add(&sl.Request{DeleteText: &sl.DeleteTextRequest{
					ObjectId: op.TargetID, TextRange: &sl.Range{Type: "ALL"},
				}})
			}
			if op.Text != "" {
				add(&sl.Request{InsertText: &sl.InsertTextRequest{
					ObjectId: op.TargetID, Text: op.Text, InsertionIndex: 0,
					ForceSendFields: []string{"InsertionIndex"},
				}})
			}
			k.hasText = op.Text != ""

		case model.OpDelete:
			if op.TargetID == "" {
				return nil, errf("TargetID required")
			}
			add(&sl.Request{DeleteObject: &sl.DeleteObjectRequest{ObjectId: op.TargetID}})
			delete(st, op.TargetID)

		case model.OpSetTag:
			if op.TargetID == "" || op.Tag == nil {
				return nil, errf("TargetID and Tag required")
			}
		case model.OpZOrderBack:
			if len(op.TargetIDs) == 0 {
				return nil, errf("TargetIDs required")
			}
			add(&sl.Request{UpdatePageElementsZOrder: &sl.UpdatePageElementsZOrderRequest{
				PageElementObjectIds: append([]string(nil), op.TargetIDs...),
				Operation:            "SEND_TO_BACK",
			}})

		default:
			return nil, errf("unknown op kind")
		}

		// 代替テキスト（タグ）。SetTag は TargetID、作成系は ObjectID。
		if op.Tag != nil {
			var id string
			switch op.Kind {
			case model.OpSetTag:
				id = op.TargetID
			case model.OpCreateShape, model.OpCreateLine, model.OpDuplicate:
				id = op.ObjectID
			}
			if id != "" {
				r, err := altText(id, op.Tag)
				if err != nil {
					return nil, errf("%w", err)
				}
				add(r)
			}
		}
	}
	return out, nil
}
