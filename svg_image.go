// Copyright 2026 The oksvg Authors. All rights reserved.
//
// svg_image.go implements restricted rendering of bitmap images embedded
// in SVG image elements as data URIs.

package oksvg

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"strings"
	"unicode"

	"github.com/srwiley/rasterx"
)

const (
	// maxImagePixels bounds the decoded size of an embedded bitmap, guarding
	// against decompression bombs in hostile SVG files.
	maxImagePixels = 1 << 24 // 16 Mi pixels
	// maxImageDataURILen bounds the base64 payload of a data URI.
	maxImageDataURILen = 1 << 26 // 64 MiB
)

// SvgImage is a bitmap embedded in the SVG document through an image element
// holding a PNG or JPEG data URI. It is composited with the SvgPaths of the
// icon in document order.
type SvgImage struct {
	Image   image.Image // decoded bitmap
	Opacity float64     // element opacity captured at parse time
	// X, Y, W, H give the destination rectangle in SVG user coordinates,
	// after preserveAspectRatio has been resolved.
	X, Y, W, H float64
	m          rasterx.Matrix2D // user space transform captured at parse time
}

// decodeImageHref decodes the href of an image element. Only base64 coded
// PNG and JPEG data URIs are accepted; external references (http:, file:,
// fragment ids, ...) and nested SVG payloads are rejected.
func decodeImageHref(href string) (image.Image, error) {
	if href == "" {
		return nil, errors.New("oksvg: image element has no href attribute")
	}
	if !strings.HasPrefix(href, "data:") {
		return nil, fmt.Errorf("oksvg: only data: URIs are supported in image elements; not loading external reference %.32q", href)
	}
	rest := href[len("data:"):]
	comma := strings.IndexByte(rest, ',')
	if comma < 0 {
		return nil, errors.New("oksvg: malformed data URI in image element")
	}
	meta, payload := rest[:comma], rest[comma+1:]
	parts := strings.Split(meta, ";")
	mediaType := strings.ToLower(strings.TrimSpace(parts[0]))
	isBase64 := false
	for _, p := range parts[1:] {
		if strings.EqualFold(strings.TrimSpace(p), "base64") {
			isBase64 = true
		}
	}
	if !isBase64 {
		return nil, fmt.Errorf("oksvg: only base64 encoded data URIs are supported in image elements, not %q", meta)
	}
	var decode func(r io.Reader) (image.Image, error)
	var decodeConfig func(r io.Reader) (image.Config, error)
	switch mediaType {
	case "image/png":
		decode, decodeConfig = png.Decode, png.DecodeConfig
	case "image/jpeg", "image/jpg":
		decode, decodeConfig = jpeg.Decode, jpeg.DecodeConfig
	default:
		return nil, fmt.Errorf("oksvg: unsupported media type %q in image element (only image/png and image/jpeg)", mediaType)
	}
	if len(payload) > maxImageDataURILen {
		return nil, fmt.Errorf("oksvg: data URI in image element exceeds the %d byte limit", maxImageDataURILen)
	}
	// Embedded data URIs are frequently line wrapped; base64 ignores nothing
	// by itself, so strip whitespace first.
	payload = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, payload)
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(payload)
		if err != nil {
			return nil, fmt.Errorf("oksvg: invalid base64 data in image element: %v", err)
		}
	}
	// Check the header before decoding the full image so that absurd
	// dimensions are rejected cheaply.
	cfg, err := decodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("oksvg: cannot read embedded image header: %v", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 ||
		int64(cfg.Width)*int64(cfg.Height) > maxImagePixels {
		return nil, fmt.Errorf("oksvg: embedded image dimensions %dx%d exceed the %d pixel limit",
			cfg.Width, cfg.Height, maxImagePixels)
	}
	img, err := decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("oksvg: cannot decode embedded image: %v", err)
	}
	return img, nil
}

// resolveImageRect applies a preserveAspectRatio attribute value to the
// image element viewport (x, y, w, h) for a bitmap of iw x ih pixels and
// returns the rectangle the bitmap is actually drawn into. Unsupported or
// malformed values fall back to the SVG default "xMidYMid meet".
func resolveImageRect(x, y, w, h, iw, ih float64, par string) (dx, dy, dw, dh float64) {
	if iw <= 0 || ih <= 0 {
		return x, y, w, h
	}
	fields := strings.Fields(par)
	if len(fields) > 0 && fields[0] == "defer" { // only meaningful on nested svg
		fields = fields[1:]
	}
	align := "xMidYMid"
	meetOrSlice := "meet"
	if len(fields) > 0 {
		align = fields[0]
	}
	if len(fields) > 1 {
		meetOrSlice = fields[1]
	}
	if align == "none" {
		return x, y, w, h
	}
	var fx, fy float64 // alignment factors: 0 min, 0.5 mid, 1 max
	switch {
	case strings.HasPrefix(align, "xMin"):
		fx = 0
	case strings.HasPrefix(align, "xMax"):
		fx = 1
	case strings.HasPrefix(align, "xMid"):
		fx = 0.5
	default:
		return resolveImageRect(x, y, w, h, iw, ih, "")
	}
	switch {
	case strings.HasSuffix(align, "YMin"):
		fy = 0
	case strings.HasSuffix(align, "YMax"):
		fy = 1
	case strings.HasSuffix(align, "YMid"):
		fy = 0.5
	default:
		return resolveImageRect(x, y, w, h, iw, ih, "")
	}
	scale := math.Min(w/iw, h/ih)
	if meetOrSlice == "slice" {
		scale = math.Max(w/iw, h/ih)
	}
	dw, dh = iw*scale, ih*scale
	dx = x + fx*(w-dw)
	dy = y + fy*(h-dh)
	return
}

// DrawTransformed composites the bitmap into the destination of the Dasher's
// scanner while applying transform t, as SvgPath.DrawTransformed does for
// vector content. Scanners without an accessible destination image (e.g.
// scanFT) are not supported; the image is then silently skipped.
func (si *SvgImage) DrawTransformed(r *rasterx.Dasher, opacity float64, t rasterx.Matrix2D) {
	gv, ok := r.Scanner.(*rasterx.ScannerGV)
	if !ok || gv.Dest == nil {
		return
	}
	clip := gv.Dest.Bounds()
	if !gv.Targ.Empty() {
		clip = clip.Intersect(gv.Targ)
	}
	si.blit(gv.Dest, clip, opacity, t)
}

// blit draws the bitmap into dst through the combined transform t, using
// nearest neighbor sampling and source-over alpha compositing.
func (si *SvgImage) blit(dst draw.Image, clip image.Rectangle, opacity float64, t rasterx.Matrix2D) {
	if si.Image == nil || si.W <= 0 || si.H <= 0 {
		return
	}
	opacity *= si.Opacity
	if opacity <= 0 {
		return
	}
	if opacity > 1 {
		opacity = 1
	}
	sb := si.Image.Bounds()
	iw, ih := float64(sb.Dx()), float64(sb.Dy())
	if iw == 0 || ih == 0 {
		return
	}
	// m maps bitmap pixel coordinates to device coordinates.
	m := t.Mult(si.m).Translate(si.X, si.Y).Scale(si.W/iw, si.H/ih)
	det := m.A*m.D - m.B*m.C
	if det == 0 || math.IsNaN(det) || math.IsInf(det, 0) {
		return // degenerate transform: the image collapses to nothing
	}
	// Device space bounding box of the transformed bitmap corners.
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, p := range [][2]float64{{0, 0}, {iw, 0}, {0, ih}, {iw, ih}} {
		px, py := m.Transform(p[0], p[1])
		minX, maxX = math.Min(minX, px), math.Max(maxX, px)
		minY, maxY = math.Min(minY, py), math.Max(maxY, py)
	}
	bounds := image.Rectangle{
		Min: image.Point{X: int(math.Floor(minX)), Y: int(math.Floor(minY))},
		Max: image.Point{X: int(math.Ceil(maxX)), Y: int(math.Ceil(maxY))},
	}.Intersect(clip)
	if bounds.Empty() {
		return
	}
	inv := m.Invert()
	op := uint32(opacity*65535 + 0.5)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			sx, sy := inv.Transform(float64(x)+0.5, float64(y)+0.5)
			if sx < 0 || sy < 0 || sx >= iw || sy >= ih {
				continue
			}
			sR, sG, sB, sA := si.Image.At(sb.Min.X+int(sx), sb.Min.Y+int(sy)).RGBA()
			if sA == 0 {
				continue
			}
			// RGBA() values are alpha premultiplied, so scaling all four
			// channels by the opacity keeps them premultiplied.
			sA = sA * op / 65535
			sR = sR * op / 65535
			sG = sG * op / 65535
			sB = sB * op / 65535
			dR, dG, dB, dA := dst.At(x, y).RGBA()
			invA := 65535 - sA
			dst.Set(x, y, color.RGBA64{
				R: uint16(sR + dR*invA/65535),
				G: uint16(sG + dG*invA/65535),
				B: uint16(sB + dB*invA/65535),
				A: uint16(sA + dA*invA/65535),
			})
		}
	}
}
