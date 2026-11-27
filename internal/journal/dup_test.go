package journal

import (
	"errors"
	"testing"

	"github.com/YuujiKamura/AISLIDE/internal/model"
)

// 複製で作った派生画像（Tag.Params[src]）の削除は、元画像からの複製で戻せる。src の無い画像は戻せない。
func TestInverseDeleteDuplicatedImage(t *testing.T) {
	b := model.Box{X: 10, Y: 20, W: 10, H: 40}
	d := &model.Deck{Elements: []model.Element{
		{ObjectID: "kd_sign", Kind: "image", Box: b, Tag: &model.Tag{Layer: model.LayerDerived, Kind: "sign",
			Params: map[string]any{"unit": "Z1.E.予告#1", "role": "sign", model.ParamSource: "p1_i21"}}},
		{ObjectID: "photo", Kind: "image", Box: b},
	}}
	inv, err := Inverse([]model.Op{{Kind: model.OpDelete, TargetID: "kd_sign"}}, d)
	if err != nil || len(inv) != 1 {
		t.Fatalf("inverse: %v %+v", err, inv)
	}
	op := inv[0]
	if op.Kind != model.OpDuplicate || op.ObjectID != "kd_sign" || op.SourceID != "p1_i21" || op.Box != b || op.Tag.Params["unit"] != "Z1.E.予告#1" {
		t.Fatalf("recreate %+v", op)
	}
	if _, err := Inverse([]model.Op{{Kind: model.OpDelete, TargetID: "photo"}}, d); !errors.Is(err, ErrIrreversible) {
		t.Fatalf("plain image: %v", err)
	}
}
