package journal

import (
	"errors"
	"testing"

	"github.com/YuujiKamura/AISLIDE/internal/model"
)

func TestInverseNotesSetText(t *testing.T) {
	d := &model.Deck{NotesID: "p1_g1", NotesText: "旧メモ"}
	inv, err := Inverse([]model.Op{{Kind: model.OpSetText, TargetID: "p1_g1", Text: "新"}}, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(inv) != 1 || inv[0].Kind != model.OpSetText || inv[0].TargetID != "p1_g1" || inv[0].Text != "旧メモ" {
		t.Fatalf("inverse = %+v", inv)
	}
	if _, err := Inverse([]model.Op{{Kind: model.OpDelete, TargetID: "p1_g1"}}, d); !errors.Is(err, ErrIrreversible) {
		t.Fatalf("delete notes: want ErrIrreversible, got %v", err)
	}
}
