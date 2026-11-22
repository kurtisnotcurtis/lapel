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

// RenderWindowsTerminalIconPNG renders a 64x64 transparent antialiased PNG of
// the lapel-bolt-tucked logo tinted to hexColor (e.g. "#0EA5E9" or "#F59E0B").
// Windows Terminal profile icons require raster formats (.png/.ico) rather than
// SVG, so generating this in pure Go keeps lapel.exe self-contained and matches
// any preset or custom --tab-color override.
func RenderWindowsTerminalIconPNG(hexColor string) ([]byte, error) {
	r, g, b := parseHexColor(hexColor, 0x38, 0xBD, 0xF8)

	// Single closed lapel-bolt tucked silhouette sampled into a smooth polyline
	// in 0..100 viewBox coordinates.
	outer := buildLapelBoltOuterPoly()

	const (
		size       = 64
		sub        = 4 // 4x4 supersampling for crisp edges at 16-32px tab scale
		strokeHalf = 3.1
		fillAlpha  = 0.22
	)

	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	invSub := 1.0 / float64(sub)
	invTotal := 1.0 / float64(sub*sub)

	for py := 0; py < size; py++ {
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
