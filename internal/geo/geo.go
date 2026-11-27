// Package geo: WebMercator による背景航空写真の緯度経度 <-> 紙面 mm 変換。純関数のみ。
package geo

// Box は紙面 mm の矩形（左上 X,Y と幅高 W,H）。model.Box と同形だが依存を切るため独立定義。
type Box struct{ X, Y, W, H float64 }

// Georef: 背景画像は「z レベルの WebMercator ピクセル空間で Center を中心に PxW x PxH を切り出したもの」。
type Georef struct {
	Z                    int
	CenterLat, CenterLon float64
	PxW, PxH             int
}

// ToMM: 画像が紙面上 img に置かれているときの (lat,lon) の紙面座標。
func (g Georef) ToMM(img Box, lat, lon float64) (x, y float64) { panic("TODO") }

// ToLatLon: ToMM の逆。
func (g Georef) ToLatLon(img Box, x, y float64) (lat, lon float64) { panic("TODO") }

// MetersPerPx: 中心緯度での地上解像度。
func (g Georef) MetersPerPx() float64 { panic("TODO") }

// MMPerMeter: 画像が img に置かれているときの縮尺（紙面 mm / 実 m）。
func (g Georef) MMPerMeter(img Box) float64 { panic("TODO") }
