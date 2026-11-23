package wsl

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"strconv"
	"strings"
)

type vec2 struct{ x, y float64 }

// BrandIconTopHex, BrandIconMidHex, and BrandIconBotHex define the vertical
// golden-amber -> warm-orange -> coral-rose gradient matching Windows 11's
// Fluent ⚡ emoji so the Windows Terminal tab icon and shell prompt harmonize.
const (
	BrandIconTopHex = "#FFB952"
	BrandIconMidHex = "#FA8546"
	BrandIconBotHex = "#F2595B"
)

// RenderBrandWindowsTerminalIconPNG renders the 64x64 lapel-bolt-tucked icon
// with the brand golden-orange-coral gradient matching ⚡.
func RenderBrandWindowsTerminalIconPNG() ([]byte, error) {
	return RenderWindowsTerminalIconGradientPNG(BrandIconTopHex, BrandIconMidHex, BrandIconBotHex)
}

// RenderWindowsTerminalIconPNG renders a 64x64 transparent antialiased PNG of
// the lapel-bolt-tucked logo tinted to a solid hexColor (e.g. "#FA8546").
// Windows Terminal profile icons require raster formats (.png/.ico) rather than
// SVG, so generating this in pure Go keeps lapel.exe self-contained.
func RenderWindowsTerminalIconPNG(hexColor string) ([]byte, error) {
	return RenderWindowsTerminalIconGradientPNG(hexColor, hexColor, hexColor)
}

// RenderWindowsTerminalIconGradientPNG renders a 64x64 transparent antialiased
// PNG of the lapel-bolt-tucked logo with a vertical 3-stop gradient from
// topHex (y=8) through midHex (y=50) to botHex (y=92).
func RenderWindowsTerminalIconGradientPNG(topHex, midHex, botHex string) ([]byte, error) {
	r0, g0, b0 := parseHexColor(topHex, 0xFF, 0xB9, 0x52)
	r1, g1, b1 := parseHexColor(midHex, 0xFA, 0x85, 0x46)
	r2, g2, b2 := parseHexColor(botHex, 0xF2, 0x59, 0x5B)

	// Single closed lapel-bolt tucked silhouette sampled into a smooth polyline
	// in 0..100 viewBox coordinates.
	outer := buildLapelBoltOuterPoly()

	const (
		size       = 64
		sub        = 4 // 4x4 supersampling for crisp edges at 16-32px tab scale
		strokeHalf = 3.1
		fillAlpha  = 0.28
	)

	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	invSub := 1.0 / float64(sub)
	invTotal := 1.0 / float64(sub*sub)

	lerpU8 := func(a, b uint8, t float64) uint8 {
		return uint8(math.Round(float64(a)*(1-t) + float64(b)*t))
	}

	for py := 0; py < size; py++ {
		// Vertical gradient parameter across the bolt's 8..92 viewBox span.
		centerY := (float64(py) + 0.5) * (100.0 / float64(size))
		gt := (centerY - 8.0) / (92.0 - 8.0)
		if gt < 0 {
			gt = 0
		} else if gt > 1 {
			gt = 1
		}
		var r, g, b uint8
		if gt <= 0.5 {
			t := gt * 2.0
			r, g, b = lerpU8(r0, r1, t), lerpU8(g0, g1, t), lerpU8(b0, b1, t)
		} else {
			t := (gt - 0.5) * 2.0
			r, g, b = lerpU8(r1, r2, t), lerpU8(g1, g2, t), lerpU8(b1, b2, t)
		}

		for px := 0; px < size; px++ {
			var cov float64
			for sy := 0; sy < sub; sy++ {
				vy := (float64(py) + (float64(sy)+0.5)*invSub) * (100.0 / float64(size))
				for sx := 0; sx < sub; sx++ {
					vx := (float64(px) + (float64(sx)+0.5)*invSub) * (100.0 / float64(size))
					p := vec2{vx, vy}
					d := distToPolyline(p, outer, true)
					if d <= strokeHalf {
						cov += 1.0
					} else if pointInPolygon(p, outer) {
						cov += fillAlpha
					}
				}
			}
			a := cov * invTotal
			if a > 1 {
				a = 1
			}
			if a > 0 {
				img.SetNRGBA(px, py, color.NRGBA{
					R: r,
					G: g,
					B: b,
					A: uint8(math.Round(a * 255)),
				})
			}
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func buildLapelBoltOuterPoly() []vec2 {
	var pts []vec2
	appendCubic := func(p0, p1, p2, p3 vec2, steps int) {
		for i := 0; i < steps; i++ {
			t := float64(i) / float64(steps)
			mt := 1 - t
			x := mt*mt*mt*p0.x + 3*mt*mt*t*p1.x + 3*mt*t*t*p2.x + t*t*t*p3.x
			y := mt*mt*mt*p0.y + 3*mt*mt*t*p1.y + 3*mt*t*t*p2.y + t*t*t*p3.y
			pts = append(pts, vec2{x, y})
		}
	}
	// Exact single closed path of the lapel bolt tucked logo (viewBox 0 0 100 100).
	appendCubic(vec2{37, 8}, vec2{48.5, 9.5}, vec2{59.5, 12.5}, vec2{69, 17}, 16)
	pts = append(pts, vec2{69, 17}, vec2{72, 34}, vec2{55.5, 41})
	appendCubic(vec2{70, 47}, vec2{58, 65}, vec2{44, 79}, vec2{28, 92}, 24)
	appendCubic(vec2{28, 92}, vec2{34.5, 71}, vec2{39.2, 53}, vec2{39.5, 39.5}, 20)
	appendCubic(vec2{39.5, 39.5}, vec2{39.6, 33}, vec2{38.4, 28}, vec2{36.4, 24.2}, 12)
	pts = append(pts, vec2{36.4, 24.2})
	appendCubic(vec2{46, 21.2}, vec2{44.5, 15.8}, vec2{41.5, 11.2}, vec2{37, 8}, 12)
	return pts
}

func distToPolyline(p vec2, poly []vec2, closed bool) float64 {
	best := math.Inf(1)
	n := len(poly)
	limit := n - 1
	if closed {
		limit = n
	}
	for i := 0; i < limit; i++ {
		a := poly[i]
		b := poly[(i+1)%n]
		if d := distToSegment(p, a, b); d < best {
			best = d
		}
	}
	return best
}

func distToSegment(p, a, b vec2) float64 {
	dx, dy := b.x-a.x, b.y-a.y
	lenSq := dx*dx + dy*dy
	if lenSq == 0 {
		return math.Hypot(p.x-a.x, p.y-a.y)
	}
	t := ((p.x-a.x)*dx + (p.y-a.y)*dy) / lenSq
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	return math.Hypot(p.x-(a.x+t*dx), p.y-(a.y+t*dy))
}

func pointInPolygon(p vec2, poly []vec2) bool {
	inside := false
	n := len(poly)
	j := n - 1
	for i := 0; i < n; i++ {
		pi, pj := poly[i], poly[j]
		if (pi.y > p.y) != (pj.y > p.y) &&
			p.x < (pj.x-pi.x)*(p.y-pi.y)/(pj.y-pi.y)+pi.x {
			inside = !inside
		}
		j = i
	}
	return inside
}

func parseHexColor(s string, defR, defG, defB uint8) (uint8, uint8, uint8) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return defR, defG, defB
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return defR, defG, defB
	}
	return uint8(v >> 16), uint8((v >> 8) & 0xFF), uint8(v & 0xFF)
}
