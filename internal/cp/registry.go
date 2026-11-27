package cp

import "context"

type handlerFunc = func(*Server, context.Context, *command) string

// extraHandlers: コマンドを別ファイルから足す口（並行作業で cp.go の handlers 表を触らないため）。
// パッケージ変数の初期化は全 init() より先に済むので、`var _ = registerHandlers(...)` で登録すれば
// cp.go の init が handlers へ取り込む。
var extraHandlers = map[string]handlerFunc{}

func registerHandlers(m map[string]handlerFunc) bool {
	for verb, h := range m {
		if _, dup := extraHandlers[verb]; dup {
			panic("cp: duplicate command " + verb)
		}
		extraHandlers[verb] = h
	}
	return true
}
