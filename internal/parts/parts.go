// Package parts: 部品（site/sign/cone_line/guard/closure/detour/note）を抽象 Op 列に展開する。
// 基準値（コーン間隔など）はここに埋め込む。AICAD AgentParts.kt の Slides 版。純関数のみ。
package parts

import "github.com/YuujiKamura/AISLIDE/internal/model"

const (
	ConePitchMinM = 3.0
	ConePitchMaxM = 5.0
	NoteMaxRunes  = 20
)

type Ctx struct {
	Deck  *model.Deck
	Layer string
	Agent string
	NewID func(layer string) string // kc_<layer>_<8hex>。テストでは決定的な関数を渡す
}

// Build: kind と k=v 引数から Op 列。作成する全要素に Tag{Layer,Owner,Kind,Params} を付ける。
func Build(kind string, args map[string]string, ctx Ctx) ([]model.Op, error) { panic("TODO") }

func Kinds() []string { panic("TODO") }

// ParseArgs: AICAD 互換の `k=v k="a b"` パーサ。
func ParseArgs(s string) map[string]string { panic("TODO") }
