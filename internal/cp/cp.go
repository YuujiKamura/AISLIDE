// Package cp: AICAD CP 互換の 1 行プロトコル TCP サーバとコマンド dispatch。
package cp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/YuujiKamura/AISLIDE/internal/geo"
	"github.com/YuujiKamura/AISLIDE/internal/journal"
	"github.com/YuujiKamura/AISLIDE/internal/model"
	"github.com/YuujiKamura/AISLIDE/internal/parts"
	"github.com/YuujiKamura/AISLIDE/internal/slides"
)

// DefaultAddr: AICAD CP (9876) の隣。
const DefaultAddr = "127.0.0.1:9877"

// Backend: slides.Client を抽象化（テストで fake を差す）。*slides.Client がそのまま満たす。
// Apply は revision 不一致のとき errors.Is(err, slides.ErrStale) となる error を返すこと。
type Backend interface {
	Load(ctx context.Context, presID string) ([]byte, error)
	Apply(ctx context.Context, presID, pageID string, ops []model.Op, requiredRev string, d *model.Deck) (string, error)
	Thumbnail(ctx context.Context, presID, pageID, path string) error
}

// Journal: 逆操作のスタック。*journal.Journal がそのまま満たす。
type Journal interface {
	Push(presID, agent string, inverse []model.Op) error
	Pop(presID, agent string) ([]model.Op, error)
}

// deps: 他パッケージの関数への差し替え口。既定は実装パッケージ、テストでは fake を入れる。
type deps struct {
	parseDeck     func(raw []byte, slideNo int) (*model.Deck, error)
	get           func(d *model.Deck, ref string) (*model.Element, error)
	claim         func(d *model.Deck, layer, agent string) error
	canWrite      func(d *model.Deck, e *model.Element, agent string) error
	layer         func(e *model.Element) string
	mapBG         func(d *model.Deck) (*model.Element, *model.GeorefTag, error)
	resolvePoint  func(d *model.Deck, s string) (float64, float64, error)
	resolveLength func(d *model.Deck, s string) (float64, error)
	build         func(kind string, args map[string]string, ctx parts.Ctx) ([]model.Op, error)
	kinds         func() []string
	inverse       func(ops []model.Op, before *model.Deck) ([]model.Op, error)
	toMM          func(g geo.Georef, img geo.Box, lat, lon float64) (float64, float64)
	toLatLon      func(g geo.Georef, img geo.Box, x, y float64) (float64, float64)
	newID         func(layer string) string
	isStale       func(err error) bool
}

func realDeps() deps {
	return deps{
		parseDeck:     model.ParseDeck,
		get:           (*model.Deck).Get,
		claim:         (*model.Deck).Claim,
		canWrite:      (*model.Deck).CanWrite,
		layer:         (*model.Element).Layer,
		mapBG:         (*model.Deck).MapBG,
		resolvePoint:  (*model.Deck).ResolvePoint,
		resolveLength: (*model.Deck).ResolveLength,
		build:         parts.Build,
		kinds:         parts.Kinds,
		inverse:       journal.Inverse,
		toMM:          geo.Georef.ToMM,
		toLatLon:      geo.Georef.ToLatLon,
		newID:         NewID,
		isStale:       func(err error) bool { return errors.Is(err, slides.ErrStale) },
	}
}

var idSafe = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// NewID: kc_<layer>_<8hex>。
func NewID(layer string) string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return "kc_" + idSafe.ReplaceAllString(layer, "_") + "_" + hex.EncodeToString(b[:])
}

// Server: CP の状態（開いているデッキ、所有者、指差し）。Dispatch は直列化される。
type Server struct {
	mu      sync.Mutex
	backend Backend
	journal Journal
	d       deps

	SlideNo int // 対象スライド（1 始まり、既定 1）

	presID string
	deck   *model.Deck
	raw    []byte
	owners map[string]string // layer -> agent。再読込のたびに Deck.Owners へ戻す
	pickID string            // pick 中の要素 objectId（"" = なし）
}

// New: backend と journal を束ねた Server。
func New(b Backend, j Journal) *Server {
	return &Server{backend: b, journal: j, d: realDeps(), SlideNo: 1, owners: map[string]string{}}
}

// Dispatch: 1 行コマンドを処理し 1 行（list のみ複数行）の応答を返す（末尾改行なし。
// list は "ok n=N\n<行>...\n" を返し、送信時に付く改行で空行終端になる）。
func (s *Server) Dispatch(ctx context.Context, line string) (resp string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer func() {
		if r := recover(); r != nil {
			resp = ng(fmt.Sprintf("internal: %v", r))
		}
	}()
	c, err := parseCommand(line)
	if err != nil {
		return ng(err.Error())
	}
	h, ok := handlers[c.verb]
	if !ok {
		return ng("unknown command " + c.verb)
	}
	if c.verb != "open" && c.verb != "kinds" && s.deck == nil {
		return ng("no deck; open <presId> first")
	}
	out := h(s, ctx, c)
	if c.verb == "list" && strings.HasPrefix(out, "ok") {
		return out
	}
	return oneLine(out)
}

func ng(reason string) string { return "ng " + reason }

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.ReplaceAll(s, "\n", " ")
}

var handlers map[string]func(*Server, context.Context, *command) string

func init() {
	handlers = map[string]func(*Server, context.Context, *command) string{
		"open":    (*Server).cmdOpen,
		"state":   (*Server).cmdState,
		"sync":    (*Server).cmdSync,
		"view":    (*Server).cmdView,
		"list":    (*Server).cmdList,
		"layers":  (*Server).cmdLayers,
		"kinds":   (*Server).cmdKinds,
		"pick":    (*Server).cmdPick,
		"unpick":  (*Server).cmdUnpick,
		"pickat":  (*Server).cmdPickAt,
		"claim":   (*Server).cmdClaim,
		"part":    (*Server).cmdPart,
		"move":    (*Server).cmdMove,
		"moveto":  (*Server).cmdMoveTo,
		"settext": (*Server).cmdSetText,
		"delete":  (*Server).cmdDelete,
		"adopt":   (*Server).cmdAdopt,
		"clear":   (*Server).cmdClear,
		"undo":    (*Server).cmdUndo,
		"capture": (*Server).cmdCapture,
		"drc":     (*Server).cmdDRC,
		"georef":  (*Server).cmdGeoref,
	}
	for verb, h := range extraHandlers { // 別ファイルで registerHandlers したもの（registry.go）
		handlers[verb] = h
	}
}

// ---- デッキ読込 ----

// load: presID を読み込み Deck を差し替える。owners を引き継ぐ。
func (s *Server) load(ctx context.Context, presID string) error {
	raw, err := s.backend.Load(ctx, presID)
	if err != nil {
		return err
	}
	no := s.SlideNo
	if no <= 0 {
		no = 1
	}
	d, err := s.d.parseDeck(raw, no)
	if err != nil {
		return err
	}
	if presID != s.presID {
		s.owners = map[string]string{}
		s.pickID = ""
	}
	d.Owners = map[string]string{}
	for k, v := range s.owners {
		d.Owners[k] = v
	}
	if d.PresID == "" {
		d.PresID = presID
	}
	s.presID, s.deck, s.raw = presID, d, raw
	return nil
}

func (s *Server) reload(ctx context.Context) error { return s.load(ctx, s.presID) }

// ---- 書込み ----

// write: ops の対象要素の所有権を確認し、逆操作を記録して Apply、成功なら再読込。
// journalAgent が nil なら journal に積まない（pick 用）。checkAgent が nil なら所有権確認を省く
// （view/georef の map レイヤ、adopt の base 要素など、呼び出し側で確認済みのもの）。
type writeOpts struct {
	checkAgent *string
	journalAs  *string
	force      bool // 逆操作が作れなくても書く（journal に積まない）
}

func (s *Server) write(ctx context.Context, ops []model.Op, o writeOpts) string {
	if len(ops) == 0 {
		return ""
	}
	if o.checkAgent != nil {
		if err := s.checkOwnership(ops, *o.checkAgent); err != nil {
			return ng(err.Error())
		}
	}
	var inv []model.Op
	journalOK := o.journalAs != nil && s.journal != nil
	if journalOK {
		var err error
		inv, err = s.d.inverse(withoutZOrder(ops), s.deck)
		if err != nil {
			if !o.force {
				return ng("cannot record undo: " + err.Error() + " (add force=yes to write without undo)")
			}
			journalOK = false
		}
	}
	if _, err := s.backend.Apply(ctx, s.presID, s.deck.PageID, ops, s.deck.Rev, s.deck); err != nil {
		if s.d.isStale(err) {
			if rerr := s.reload(ctx); rerr != nil {
				return ng("stale rev changed; reload failed: " + rerr.Error())
			}
			return ng("stale rev changed; re-run list")
		}
		return ng("apply: " + err.Error())
	}
	if journalOK && len(inv) > 0 {
		if err := s.journal.Push(s.presID, *o.journalAs, inv); err != nil {
			_ = s.reload(ctx)
			return ng("applied but journal push failed: " + err.Error())
		}
	}
	if err := s.reload(ctx); err != nil {
		return ng("applied but reload failed: " + err.Error())
	}
	return ""
}

func withoutZOrder(ops []model.Op) []model.Op {
	out := make([]model.Op, 0, len(ops))
	for _, op := range ops {
		if op.Kind != model.OpZOrderBack {
			out = append(out, op)
		}
	}
	return out
}

// checkOwnership: ops が触る既存要素すべてに CanWrite。
func (s *Server) checkOwnership(ops []model.Op, agent string) error {
	seen := map[string]bool{}
	check := func(id string) error {
		if id == "" || seen[id] {
			return nil
		}
		seen[id] = true
		e, err := s.d.get(s.deck, id)
		if err != nil {
			return nil // 新規作成される要素（同一 batch 内で作って触る）
		}
		if err := s.d.canWrite(s.deck, e, agent); err != nil {
			return fmt.Errorf("%w: i=%d id=%s", err, e.Index, e.ObjectID)
		}
		return nil
	}
	for _, op := range ops {
		if op.Kind != model.OpCreateShape && op.Kind != model.OpCreateLine && op.Kind != model.OpDuplicate {
			if err := check(op.TargetID); err != nil {
				return err
			}
		}
		for _, id := range op.TargetIDs {
			if err := check(id); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Server) elementLayer(e *model.Element) string { return s.d.layer(e) }

func (s *Server) syncOwners() {
	for k, v := range s.deck.Owners {
		s.owners[k] = v
	}
}
