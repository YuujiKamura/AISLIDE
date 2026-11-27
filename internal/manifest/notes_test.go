package manifest

import (
	"os"
	"strings"
	"testing"

	"github.com/YuujiKamura/AISLIDE/internal/model"
)

func sample() *Manifest {
	return &Manifest{Version: 1, Legend: &Legend{Mode: "draft", Items: []LegendItem{
		{Key: "③", Label: "100m先工事中", ImageID: "p1_i19", LabelID: "p1_i20", On: false},
	}}}
}

func TestParseNoMarker(t *testing.T) {
	m, rest, err := Parse("ただのメモ\n2 行目")
	if err != nil || m != nil || rest != "ただのメモ\n2 行目" {
		t.Fatalf("got %v %q %v", m, rest, err)
	}
}

func TestEncodeParseRoundTripKeepsNotes(t *testing.T) {
	for _, rest := range []string{"", "人のメモ\n東側は夜間のみ"} {
		txt, err := Encode(sample(), rest)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(txt, Marker+"\n{") {
			t.Fatalf("marker line missing: %q", txt)
		}
		if strings.Contains(txt, `\u`) {
			t.Fatalf("JSON must keep Japanese readable: %q", txt)
		}
		m, r2, err := Parse(txt)
		if err != nil {
			t.Fatal(err)
		}
		if r2 != rest {
			t.Errorf("rest = %q want %q", r2, rest)
		}
		if m.Legend == nil || m.Legend.Items[0].Key != "③" || m.Legend.Items[0].On {
			t.Errorf("manifest = %+v", m)
		}
		again, _ := Encode(m, r2)
		if again != txt {
			t.Errorf("Encode not idempotent:\n%q\n%q", again, txt)
		}
	}
}

// 人がマーカーの前後にメモを書いても残る。
func TestParseTextAroundManifest(t *testing.T) {
	body, _ := Encode(sample(), "")
	notes := "前のメモ\r\n\n" + body + "\n\n後のメモ"
	m, rest, err := Parse(notes)
	if err != nil || m == nil {
		t.Fatalf("%v %v", m, err)
	}
	if rest != "前のメモ\n後のメモ" {
		t.Fatalf("rest = %q", rest)
	}
}

func TestParseBrokenJSON(t *testing.T) {
	if _, _, err := Parse(Marker + "\n{\"version\":1,"); err == nil {
		t.Fatal("broken JSON must be an error")
	}
	if _, _, err := Parse(Marker + "\n{\"version\":2}"); err == nil {
		t.Fatal("unknown version must be an error")
	}
}

// 実デッキ（ノート本文は空）: ノート shape の objectId が取れ、WriteOp がそれを対象にする。
func TestFromDeckFixture(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/akitsu_presentation.json")
	if err != nil {
		t.Fatal(err)
	}
	d, err := model.ParseDeck(raw, 1)
	if err != nil {
		t.Fatal(err)
	}
	if d.NotesID != "p1_g1" || d.NotesText != "" {
		t.Fatalf("notes = %q %q", d.NotesID, d.NotesText)
	}
	m, rest, err := FromDeck(d)
	if err != nil || m != nil || rest != "" {
		t.Fatalf("%v %q %v", m, rest, err)
	}
	op, ok, err := WriteOp(d, New(), rest)
	if err != nil || !ok || op.Kind != model.OpSetText || op.TargetID != "p1_g1" {
		t.Fatalf("WriteOp = %+v %v %v", op, ok, err)
	}
	d.NotesText = op.Text
	if _, ok, _ := WriteOp(d, New(), ""); ok {
		t.Fatal("unchanged manifest must not produce an op")
	}
	if _, _, err := FromDeck(&model.Deck{}); err != ErrNoNotes {
		t.Fatalf("no notes: %v", err)
	}
}
