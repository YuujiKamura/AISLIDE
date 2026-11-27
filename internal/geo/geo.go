// Package geo: WebMercator による背景航空写真の緯度経度 <-> 紙面 mm 変換。純関数のみ。
package geo

import "math"

// Box は紙面 mm の矩形（左上 X,Y と幅高 W,H）。model.Box と同形だが依存を切るため独立定義。
type Box struct{ X, Y, W, H float64 }

// Georef: 背景画像は「z レベルの WebMercator ピクセル空間で Center を中心に PxW x PxH を切り出したもの」。
type Georef struct {
	Z                    int
	CenterLat, CenterLon float64
	PxW, PxH             int
}

// TileSize は WebMercator のタイル 1 枚のピクセル数。
const TileSize = 256

// EarthMPerPxZ0 は赤道・z=0 での地上解像度（m/px）。
const EarthMPerPxZ0 = 156543.03392

func worldSize(z int) float64 { return TileSize * math.Exp2(float64(z)) }

// LatLonToWorld: z レベルの世界ピクセル座標。
func LatLonToWorld(z int, lat, lon float64) (wx, wy float64) {
	s := worldSize(z)
	phi := lat * math.Pi / 180
	wx = (lon + 180) / 360 * s
	wy = (1 - math.Log(math.Tan(phi)+1/math.Cos(phi))/math.Pi) / 2 * s
	return
}

// WorldToLatLon: LatLonToWorld の逆。
func WorldToLatLon(z int, wx, wy float64) (lat, lon float64) {
	s := worldSize(z)
	lon = wx/s*360 - 180
	lat = math.Atan(math.Sinh(math.Pi*(1-2*wy/s))) * 180 / math.Pi
	return
}

// origin: 画像左上の世界ピクセル座標。
func (g Georef) origin() (x0, y0 float64) {
	cx, cy := LatLonToWorld(g.Z, g.CenterLat, g.CenterLon)
	return cx - float64(g.PxW)/2, cy - float64(g.PxH)/2
}

// ToMM: 画像が紙面上 img に置かれているときの (lat,lon) の紙面座標。
func (g Georef) ToMM(img Box, lat, lon float64) (x, y float64) {
	x0, y0 := g.origin()
	wx, wy := LatLonToWorld(g.Z, lat, lon)
	x = img.X + (wx-x0)/float64(g.PxW)*img.W
	y = img.Y + (wy-y0)/float64(g.PxH)*img.H
	return
}

// ToLatLon: ToMM の逆。
func (g Georef) ToLatLon(img Box, x, y float64) (lat, lon float64) {
	x0, y0 := g.origin()
	wx := x0 + (x-img.X)/img.W*float64(g.PxW)
	wy := y0 + (y-img.Y)/img.H*float64(g.PxH)
	return WorldToLatLon(g.Z, wx, wy)
}

// MetersPerPx: 中心緯度での地上解像度。
func (g Georef) MetersPerPx() float64 {
	return EarthMPerPxZ0 * math.Cos(g.CenterLat*math.Pi/180) / math.Exp2(float64(g.Z))
}

// MMPerMeter: 画像が img に置かれているときの縮尺（紙面 mm / 実 m）。
// 画像が縦横非等倍に引き伸ばされている場合は横方向（W/PxW）の縮尺を返す。
// 任意方向の実距離は MetersBetween を使うこと。
func (g Georef) MMPerMeter(img Box) float64 {
	return img.W / float64(g.PxW) / g.MetersPerPx()
}

// MetersBetween: 紙面上 2 点間の実距離（m）。縦横の縮尺を別々に扱い、中心緯度の解像度で換算する。
func (g Georef) MetersBetween(img Box, x1, y1, x2, y2 float64) float64 {
	dxPx := (x2 - x1) / img.W * float64(g.PxW)
	dyPx := (y2 - y1) / img.H * float64(g.PxH)
	return math.Hypot(dxPx, dyPx) * g.MetersPerPx()
}
