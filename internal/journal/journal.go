// Package journal: 書込みの逆操作を記録し undo する。
package journal

import "github.com/YuujiKamura/AISLIDE/internal/model"

// Inverse: before（適用前のデッキ）に対して ops を適用したときの逆操作列（逆順）。
// create→delete、delete→（再作成できない画像は ng を返すため error）、transform→元の Box、set_text→元の文字、set_tag→元の Tag。
func Inverse(ops []model.Op, before *model.Deck) ([]model.Op, error) { panic("TODO") }

// Journal: <dir>/<presId>.jsonl に追記。agent ごとのスタック。
type Journal struct{ Dir string }

func (j *Journal) Push(presID, agent string, inverse []model.Op) error { panic("TODO") }
func (j *Journal) Pop(presID, agent string) ([]model.Op, error)      { panic("TODO") }
