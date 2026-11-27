package model

import "testing"

func TestParseDeckNotes(t *testing.T) {
	raw := []byte(`{"presentationId":"p","revisionId":"r","slides":[{"objectId":"s1","pageElements":[],
	 "slideProperties":{"notesPage":{"notesProperties":{"speakerNotesObjectId":"n1"},"pageElements":[
	  {"objectId":"n0","shape":{"text":{"textElements":[{"textRun":{"content":"slide image\n"}}]}}},
	  {"objectId":"n1","shape":{"text":{"textElements":[{"paragraphMarker":{}},{"textRun":{"content":"メモ\n"}},{"textRun":{"content":"#aislide manifest v1\n"}}]}}}]}}}]}`)
	d, err := ParseDeck(raw, 1)
	if err != nil {
		t.Fatal(err)
	}
	if d.NotesID != "n1" || d.NotesText != "メモ\n#aislide manifest v1" {
		t.Fatalf("notes = %q %q", d.NotesID, d.NotesText)
	}
	if len(d.Elements) != 0 {
		t.Fatal("notes shapes must not be slide elements")
	}
	if !IsReservedLayer(LayerDerived) {
		t.Fatal("derived layer must be reserved")
	}
}
