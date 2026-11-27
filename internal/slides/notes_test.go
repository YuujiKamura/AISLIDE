package slides

import (
	"testing"

	"github.com/YuujiKamura/AISLIDE/internal/model"
)

// スピーカーノートの本文 shape は Elements に無いが、set_text の対象にできる。
// 既存文字があるときだけ DeleteText を出す。transform は寸法不明なので拒否。
func TestSetTextOnNotes(t *testing.T) {
	for _, tc := range []struct {
		notes   string
		wantDel bool
	}{{"", false}, {"人のメモ", true}} {
		d := testDeck()
		d.NotesID, d.NotesText = "p1_g1", tc.notes
		reqs, err := ToRequests([]model.Op{{Kind: model.OpSetText, TargetID: "p1_g1", Text: "#aislide manifest v1\n{}"}}, "p1", d)
		if err != nil {
			t.Fatalf("notes=%q: %v", tc.notes, err)
		}
		hasDel := false
		for _, r := range reqs {
			if r.DeleteText != nil {
				hasDel = true
				if r.DeleteText.ObjectId != "p1_g1" {
					t.Errorf("DeleteText target %s", r.DeleteText.ObjectId)
				}
			}
		}
		if hasDel != tc.wantDel {
			t.Errorf("notes=%q: DeleteText=%v want %v", tc.notes, hasDel, tc.wantDel)
		}
		last := reqs[len(reqs)-1]
		if last.InsertText == nil || last.InsertText.ObjectId != "p1_g1" {
			t.Fatalf("want InsertText on notes, got %+v", last)
		}
	}
	d := testDeck()
	d.NotesID = "p1_g1"
	if _, err := ToRequests([]model.Op{{Kind: model.OpTransform, TargetID: "p1_g1", Box: model.Box{W: 1, H: 1}}}, "p1", d); err == nil {
		t.Fatal("transform of notes shape must fail")
	}
}
