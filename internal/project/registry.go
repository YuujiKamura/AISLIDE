package project

var registered []Projector

// Register: 追加の Projector（zones 等）を Default に加える。パッケージの init から呼ぶ。
func Register(p Projector) { registered = append(registered, p) }

// Default: cp の `project` などが走らせる全 Projector（凡例が先、登録順に続く）。
func Default() []Projector { return append([]Projector{Legend{}}, registered...) }
