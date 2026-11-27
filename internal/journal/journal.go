// Package journal: 書込みの逆操作を記録し undo する。
package journal

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/YuujiKamura/AISLIDE/internal/model"
)

// ErrEmpty: undo できる記録が無い。
var ErrEmpty = errors.New("nothing to undo")

// ErrIrreversible: 逆操作を作れない（画像の削除など）。
var ErrIrreversible = errors.New("irreversible op")

// shadow: Inverse 中に追跡する要素の状態（ops を順に適用したつもりの値）。
type shadow struct {
	el      model.Element
	created bool // この ops 内で作成された
	deleted bool
}

// Inverse: before（適用前のデッキ）に対して ops を適用したときの逆操作列（逆順）。
// create→delete、delete→（再作成できない画像は ng を返すため error）、transform→元の Box、set_text→元の文字、set_tag→元の Tag。
//
//   - 同じ ops 内で作成した要素への後続操作は、作成の逆（delete）で消えるので逆操作を出さない。
//   - delete の逆は shape / line のみ再作成（同じ objectId・Box・見た目・Tag）。image / group / table などは ErrIrreversible。
//   - set_tag の元が nil（base 要素の adopt）なら逆は Tag=nil の set_tag（description を空に戻す）。
//   - zorder_back は逆操作を記録しない（元の重なり順を Op で表せないため。見た目の重なりだけの変化）。
func Inverse(ops []model.Op, before *model.Deck) ([]model.Op, error) {
	state := map[string]*shadow{}
	get := func(id string) (*shadow, error) {
		if s, ok := state[id]; ok {
			if s.deleted {
				return nil, fmt.Errorf("%s: already deleted in this batch: %w", id, model.ErrNotFound)
			}
			return s, nil
		}
		if before == nil {
			return nil, fmt.Errorf("%s: %w", id, model.ErrNotFound)
		}
		for i := range before.Elements {
			if before.Elements[i].ObjectID == id {
				s := &shadow{el: before.Elements[i]}
				state[id] = s
				return s, nil
			}
		}
		return nil, fmt.Errorf("%s: %w", id, model.ErrNotFound)
	}

	var inv []model.Op
	for _, op := range ops {
		switch op.Kind {
		case model.OpCreateShape, model.OpCreateLine, model.OpDuplicate:
			if op.ObjectID == "" {
				return nil, fmt.Errorf("%s without ObjectID: %w", op.Kind, ErrIrreversible)
			}
			state[op.ObjectID] = &shadow{created: true, el: model.Element{ObjectID: op.ObjectID}}
			inv = append(inv, model.Op{Kind: model.OpDelete, TargetID: op.ObjectID})
		case model.OpDelete:
			s, err := get(op.TargetID)
			if err != nil {
				return nil, err
			}
			if !s.created {
				re, err := recreate(&s.el)
				if err != nil {
					return nil, err
				}
				inv = append(inv, re)
			}
			s.deleted = true
		case model.OpTransform:
			s, err := get(op.TargetID)
			if err != nil {
				return nil, err
			}
			if !s.created {
				inv = append(inv, model.Op{Kind: model.OpTransform, TargetID: op.TargetID, Box: s.el.Box})
			}
			s.el.Box = op.Box
		case model.OpSetText:
			s, err := get(op.TargetID)
			if err != nil {
				return nil, err
			}
			if !s.created {
				inv = append(inv, model.Op{Kind: model.OpSetText, TargetID: op.TargetID, Text: s.el.Text})
			}
			s.el.Text = op.Text
		case model.OpSetTag:
			s, err := get(op.TargetID)
			if err != nil {
				return nil, err
			}
			if !s.created {
				inv = append(inv, model.Op{Kind: model.OpSetTag, TargetID: op.TargetID, Tag: cloneTag(s.el.Tag)})
			}
			s.el.Tag = cloneTag(op.Tag)
		case model.OpZOrderBack:
			// 逆操作なし（上記コメント）。
		default:
			return nil, fmt.Errorf("unknown op kind %q: %w", op.Kind, ErrIrreversible)
		}
	}
	for i, j := 0, len(inv)-1; i < j; i, j = i+1, j-1 {
		inv[i], inv[j] = inv[j], inv[i]
	}
	return inv, nil
}

func recreate(e *model.Element) (model.Op, error) {
	switch {
	case e.Kind == "shape" && e.Shape != nil:
		st := *e.Shape
		st.Text = e.Text
		if st.ShapeType != "ELLIPSE" {
			// TEXT_BOX 等は Op では RECTANGLE として作り直す（塗り・線は元の値）。
			st.ShapeType = "RECTANGLE"
		}
		return model.Op{Kind: model.OpCreateShape, ObjectID: e.ObjectID, Box: e.Box, Shape: &st, Tag: cloneTag(e.Tag)}, nil
	case e.Kind == "line" && e.Line != nil:
		ls := *e.Line
		x1, y1, x2, y2 := e.LineEnds()
		return model.Op{Kind: model.OpCreateLine, ObjectID: e.ObjectID, X1: x1, Y1: y1, X2: x2, Y2: y2, Line: &ls, Tag: cloneTag(e.Tag)}, nil
	}
	return model.Op{}, fmt.Errorf("delete %s (%s) cannot be undone: %w", e.ObjectID, e.Kind, ErrIrreversible)
}

func cloneTag(t *model.Tag) *model.Tag {
	if t == nil {
		return nil
	}
	c := *t
	if t.Params != nil {
		c.Params = make(map[string]any, len(t.Params))
		for k, v := range t.Params {
			c.Params[k] = v
		}
	}
	if t.Georef != nil {
		g := *t.Georef
		if t.Georef.Sites != nil {
			g.Sites = make(map[string][2]float64, len(t.Georef.Sites))
			for k, v := range t.Georef.Sites {
				g.Sites[k] = v
			}
		}
		c.Georef = &g
	}
	return &c
}

// Journal: <dir>/<presId>.jsonl に追記。agent ごとのスタック。
//
// 追記のみのログ。1 行 1 レコード:
//
//	{"seq":N,"t":"...","agent":"a1","inverse":[...]}   push
//	{"seq":M,"t":"...","agent":"a1","pop":N}           Pop で N を取り消し済みにした印
//
// Pop(agent) は agent の未取消 push のうち最新を返す。agent=="" は全 agent で最新。
// Pop は記録を消費する（返した逆操作の適用に失敗したら、呼び出し側が Push し直す）。
type Journal struct {
	Dir string
	mu  sync.Mutex
}

type record struct {
	Seq     int        `json:"seq"`
	T       string     `json:"t,omitempty"`
	Agent   string     `json:"agent"`
	Inverse []model.Op `json:"inverse,omitempty"`
	Pop     int        `json:"pop,omitempty"`
}

var presIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func (j *Journal) path(presID string) (string, error) {
	if !presIDRe.MatchString(presID) {
		return "", fmt.Errorf("journal: bad presentation id %q", presID)
	}
	return filepath.Join(j.Dir, presID+".jsonl"), nil
}

func (j *Journal) load(path string) ([]record, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var recs []record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	line := 0
	for sc.Scan() {
		line++
		b := sc.Bytes()
		if len(b) == 0 {
			continue
		}
		var r record
		if err := json.Unmarshal(b, &r); err != nil {
			return nil, fmt.Errorf("journal %s line %d: %w", path, line, err)
		}
		recs = append(recs, r)
	}
	return recs, sc.Err()
}

func (j *Journal) appendRec(path string, r record) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func nextSeq(recs []record) int {
	m := 0
	for _, r := range recs {
		if r.Seq > m {
			m = r.Seq
		}
	}
	return m + 1
}

// Push: 逆操作列を agent のスタックに積む。空の inverse は記録しない。
func (j *Journal) Push(presID, agent string, inverse []model.Op) error {
	if len(inverse) == 0 {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	path, err := j.path(presID)
	if err != nil {
		return err
	}
	recs, err := j.load(path)
	if err != nil {
		return err
	}
	return j.appendRec(path, record{Seq: nextSeq(recs), T: time.Now().UTC().Format(time.RFC3339), Agent: agent, Inverse: inverse})
}

// Pop: agent の直近の逆操作列を取り出す（agent=="" なら全 agent で直近）。無ければ ErrEmpty。
func (j *Journal) Pop(presID, agent string) ([]model.Op, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	path, err := j.path(presID)
	if err != nil {
		return nil, err
	}
	recs, err := j.load(path)
	if err != nil {
		return nil, err
	}
	popped := map[int]bool{}
	for _, r := range recs {
		if r.Pop != 0 {
			popped[r.Pop] = true
		}
	}
	for i := len(recs) - 1; i >= 0; i-- {
		r := recs[i]
		if r.Pop != 0 || popped[r.Seq] || len(r.Inverse) == 0 {
			continue
		}
		if agent != "" && r.Agent != agent {
			continue
		}
		if err := j.appendRec(path, record{Seq: nextSeq(recs), T: time.Now().UTC().Format(time.RFC3339), Agent: r.Agent, Pop: r.Seq}); err != nil {
			return nil, err
		}
		return r.Inverse, nil
	}
	return nil, ErrEmpty
}
