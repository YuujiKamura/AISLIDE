package geo

import (
	"math"
	"testing"
)

// 秋津町秋田（43）の背景写真の georef（testdata の p1_i4）。
var akitsu = Georef{Z: 17, CenterLat: 32.76958, CenterLon: 130.77837, PxW: 2048, PxH: 1024}

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func TestCenterMapsToImageCenter(t *testing.T) {
	cases := []struct {
		name         string
		img          Box
		wantX, wantY float64
	}{
		{"fixture box", Box{10, 18, 400, 200}, 210, 118},
		{"origin box", Box{0, 0, 2048, 1024}, 1024, 512},
		{"stretched", Box{10.5, 17, 399.5, 225.65}, 10.5 + 399.5/2, 17 + 225.65/2},
	}
	for _, c := range cases {
		x, y := akitsu.ToMM(c.img, akitsu.CenterLat, akitsu.CenterLon)
		if !near(x, c.wantX, 1e-6) || !near(y, c.wantY, 1e-6) {
			t.Errorf("%s: center -> (%v,%v), want (%v,%v)", c.name, x, y, c.wantX, c.wantY)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	img := Box{10, 18, 400, 200}
	pts := [][2]float64{
		{32.7684, 130.77515},
		{32.76954, 130.77827},
		{32.77076, 130.78159},
		{akitsu.CenterLat, akitsu.CenterLon},
	}
	for _, p := range pts {
		x, y := akitsu.ToMM(img, p[0], p[1])
		lat, lon := akitsu.ToLatLon(img, x, y)
		if !near(lat, p[0], 1e-9) || !near(lon, p[1], 1e-9) {
			t.Errorf("round trip %v -> (%v,%v) -> (%v,%v)", p, x, y, lat, lon)
		}
	}
	// mm -> latlon -> mm
	for _, m := range [][2]float64{{10, 18}, {410, 218}, {123.4, 99.9}} {
		lat, lon := akitsu.ToLatLon(img, m[0], m[1])
		x, y := akitsu.ToMM(img, lat, lon)
		if !near(x, m[0], 1e-6) || !near(y, m[1], 1e-6) {
			t.Errorf("round trip mm %v -> (%v,%v)", m, x, y)
		}
	}
}

func TestOrientation(t *testing.T) {
	img := Box{10, 18, 400, 200}
	cx, cy := akitsu.ToMM(img, akitsu.CenterLat, akitsu.CenterLon)
	// 北（緯度+）は紙面上（y-）、東（経度+）は紙面右（x+）。
	_, yN := akitsu.ToMM(img, akitsu.CenterLat+0.001, akitsu.CenterLon)
	xE, _ := akitsu.ToMM(img, akitsu.CenterLat, akitsu.CenterLon+0.001)
	if !(yN < cy) || !(xE > cx) {
		t.Fatalf("orientation wrong: yN=%v cy=%v xE=%v cx=%v", yN, cy, xE, cx)
	}
}

func TestScale(t *testing.T) {
	mpp := akitsu.MetersPerPx()
	want := 156543.03392 * math.Cos(32.76958*math.Pi/180) / 131072
	if !near(mpp, want, 1e-12) || !near(mpp, 1.0040, 0.001) {
		t.Fatalf("MetersPerPx = %v, want %v (~1.004)", mpp, want)
	}
	img := Box{10, 18, 400, 200}
	mmpm := akitsu.MMPerMeter(img)
	if !near(mmpm, 400.0/2048/mpp, 1e-12) {
		t.Fatalf("MMPerMeter = %v", mmpm)
	}
	// 横 100m 相当の紙面距離 → MetersBetween で 100m に戻る。
	d := 100 * mmpm
	if m := akitsu.MetersBetween(img, 50, 50, 50+d, 50); !near(m, 100, 1e-9) {
		t.Fatalf("MetersBetween horizontal = %v", m)
	}
	// 縦方向は縦の縮尺で換算（このボックスは等倍なので同じ）。
	if m := akitsu.MetersBetween(img, 50, 50, 50, 50+d); !near(m, 100, 1e-9) {
		t.Fatalf("MetersBetween vertical = %v", m)
	}
	// 縦に 2 倍引き伸ばした画像では縦の紙面距離は半分の実距離。
	tall := Box{10, 18, 400, 400}
	if m := akitsu.MetersBetween(tall, 50, 50, 50, 50+d); !near(m, 50, 1e-9) {
		t.Fatalf("MetersBetween stretched = %v", m)
	}
}
