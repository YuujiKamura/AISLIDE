package manifest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/YuujiKamura/AISLIDE/internal/model"
)

// ErrNoNotes: スライドにスピーカーノートの本文 shape が無い（manifest を保存できない）。
var ErrNoNotes = errors.New("slide has no speaker notes shape")

// New: 空の v1 manifest。
func New() *Manifest { return &Manifest{Version: 1} }

// Parse: ノート本文から manifest を取り出す。rest はマーカー行と JSON を除いた残りの文字（人のメモ）。
// マーカーが無ければ (nil, notes, nil)。マーカーがあるのに JSON が壊れていれば error（上書きしない）。
func Parse(notes string) (m *Manifest, rest string, err error) {
	notes = strings.ReplaceAll(notes, "\r\n", "\n")
	start, after := -1, 0
	for off := 0; off <= len(notes); {
		end := strings.IndexByte(notes[off:], '\n')
		line := notes[off:]
		next := len(notes) + 1
		if end >= 0 {
			line = notes[off : off+end]
			next = off + end + 1
		}
		if strings.TrimSpace(line) == Marker {
			start, after = off, min(next, len(notes))
			break
		}
		off = next
	}
	if start < 0 {
		return nil, notes, nil
	}
	dec := json.NewDecoder(strings.NewReader(notes[after:]))
	m = &Manifest{}
	if err := dec.Decode(m); err != nil {
		return nil, notes, fmt.Errorf("manifest: bad JSON after %q: %w", Marker, err)
	}
	if m.Version != 1 {
		return nil, notes, fmt.Errorf("manifest: unsupported version %d", m.Version)
	}
	tail := notes[after+int(dec.InputOffset()):]
	before := strings.TrimRight(notes[:start], " \t\n")
	tail = strings.TrimLeft(tail, " \t\n")
	switch {
	case before == "":
		rest = tail
	case tail == "":
		rest = before
	default:
		rest = before + "\n" + tail
	}
	return m, rest, nil
}

// Encode: rest（人のメモ）の後ろに空行・マーカー行・JSON を付けたノート本文。
// Parse(Encode(m, r)) は (m, TrimRight(r)) を返し、Encode は同じ入力に同じ文字列を返す（冪等）。
func Encode(m *Manifest, rest string) (string, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", " ")
	if err := enc.Encode(m); err != nil {
		return "", err
	}
	body := Marker + "\n" + strings.TrimRight(b.String(), "\n")
	rest = strings.TrimRight(strings.ReplaceAll(rest, "\r\n", "\n"), " \t\n")
	if rest == "" {
		return body, nil
	}
	return rest + "\n\n" + body, nil
}

// FromDeck: デッキのノートから manifest を読む。無ければ (nil, ノート全文, nil)。
func FromDeck(d *model.Deck) (*Manifest, string, error) {
	if d.NotesID == "" {
		return nil, "", ErrNoNotes
	}
	return Parse(d.NotesText)
}

// WriteOp: m を rest と共にノートへ書く Op。文字が変わらなければ ok=false（Op 不要）。
func WriteOp(d *model.Deck, m *Manifest, rest string) (op model.Op, ok bool, err error) {
	if d.NotesID == "" {
		return model.Op{}, false, ErrNoNotes
	}
	text, err := Encode(m, rest)
	if err != nil {
		return model.Op{}, false, err
	}
	if text == d.NotesText {
		return model.Op{}, false, nil
	}
	return model.Op{Kind: model.OpSetText, TargetID: d.NotesID, Text: text}, true, nil
}
