// Copyright 2026 The oksvg Authors. All rights reserved.
package oksvg_test

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	. "github.com/srwiley/oksvg"
	. "github.com/srwiley/rasterx"
)

var (
	testRed   = color.RGBA{0xFF, 0x00, 0x00, 0xFF}
	testGreen = color.RGBA{0x00, 0xC0, 0x00, 0xFF}
)

// twoColorImage returns a w x h bitmap: left half c1, right half c2.
func twoColorImage(w, h int, c1, c2 color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x < w/2 {
				img.SetRGBA(x, y, c1)
			} else {
				img.SetRGBA(x, y, c2)
			}
		}
	}
	return img
}

func pngDataURI(t *testing.T, img image.Image) string {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func jpegDataURI(t *testing.T, img image.Image) string {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

// giantPNGDataURI returns a data URI for a PNG whose IHDR claims
// 50000x50000 pixels but which holds no image data at all.
func giantPNGDataURI() string {
	var buf bytes.Buffer
	buf.Write([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A})
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], 50000)
	binary.BigEndian.PutUint32(ihdr[4:8], 50000)
	ihdr[8] = 8 // bit depth
	ihdr[9] = 6 // color type RGBA
	chunk := append([]byte("IHDR"), ihdr...)
	binary.Write(&buf, binary.BigEndian, uint32(13))
	buf.Write(chunk)
	binary.Write(&buf, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func renderSVGString(t *testing.T, svg string, w, h int, errMode ...ErrorMode) (*image.RGBA, *SvgIcon, error) {
	t.Helper()
	icon, err := ReadIconStream(strings.NewReader(svg), errMode...)
	if err != nil {
		return nil, icon, err
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	scanner := NewScannerGV(w, h, img, img.Bounds())
	raster := NewDasher(w, h, scanner)
	icon.Draw(raster, 1.0)
	return img, icon, nil
}

func checkPixel(t *testing.T, img *image.RGBA, x, y int, want color.RGBA, tol int) {
	t.Helper()
	got := img.RGBAAt(x, y)
	for _, d := range [][2]int{
		{int(got.R), int(want.R)},
		{int(got.G), int(want.G)},
		{int(got.B), int(want.B)},
		{int(got.A), int(want.A)},
	} {
		diff := d[0] - d[1]
		if diff < 0 {
			diff = -diff
		}
		if diff > tol {
			t.Errorf("pixel (%d,%d): got %v, want %v (tol %d)", x, y, got, want, tol)
			return
		}
	}
}

func TestImagePNGPlacement(t *testing.T) {
	uri := pngDataURI(t, twoColorImage(4, 2, testRed, testGreen))
	svg := `<svg viewBox="0 0 10 10" xmlns="http://www.w3.org/2000/svg">` +
		`<rect x="0" y="0" width="10" height="10" fill="white"/>` +
		`<image x="2" y="3" width="4" height="2" href="` + uri + `"/>` +
		`</svg>`
	img, icon, err := renderSVGString(t, svg, 10, 10, StrictErrorMode)
	if err != nil {
		t.Fatal(err)
	}
	if len(icon.Images) != 1 {
		t.Fatalf("expected 1 embedded image, got %d", len(icon.Images))
	}
	white := color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}
	checkPixel(t, img, 0, 0, white, 0)     // background
	checkPixel(t, img, 1, 2, white, 0)     // outside the image
	checkPixel(t, img, 3, 4, testRed, 0)   // left half of the bitmap
	checkPixel(t, img, 5, 4, testGreen, 0) // right half of the bitmap
	checkPixel(t, img, 6, 4, white, 0)     // just past the right edge
	checkPixel(t, img, 3, 5, white, 0)     // just past the bottom edge
}

func TestImageJPEGPlacement(t *testing.T) {
	uri := jpegDataURI(t, twoColorImage(16, 16, testRed, testGreen))
	svg := `<svg viewBox="0 0 20 20" xmlns="http://www.w3.org/2000/svg">` +
		`<image x="2" y="2" width="16" height="16" href="` + uri + `"/>` +
		`</svg>`
	img, _, err := renderSVGString(t, svg, 20, 20, StrictErrorMode)
	if err != nil {
		t.Fatal(err)
	}
	// JPEG is lossy; sample away from the color boundary with a tolerance.
	checkPixel(t, img, 4, 10, testRed, 40)
	checkPixel(t, img, 15, 10, testGreen, 40)
	checkPixel(t, img, 0, 0, color.RGBA{0, 0, 0, 0}, 0) // untouched canvas
}

func TestImageOpacity(t *testing.T) {
	uri := pngDataURI(t, twoColorImage(4, 2, testRed, testGreen))
	svg := `<svg viewBox="0 0 10 10" xmlns="http://www.w3.org/2000/svg">` +
		`<rect x="0" y="0" width="10" height="10" fill="white"/>` +
		`<g opacity="0.5"><image x="0" y="0" width="4" height="2" href="` + uri + `"/></g>` +
		`</svg>`
	img, _, err := renderSVGString(t, svg, 10, 10, StrictErrorMode)
	if err != nil {
		t.Fatal(err)
	}
	// 50% red over white
	checkPixel(t, img, 1, 1, color.RGBA{0xFF, 0x7F, 0x7F, 0xFF}, 2)
	// 50% green over white
	checkPixel(t, img, 3, 1, color.RGBA{0x7F, 0xDF, 0x7F, 0xFF}, 2)
}

func TestImageDocumentOrder(t *testing.T) {
	uri := pngDataURI(t, twoColorImage(4, 4, testRed, testGreen))
	svg := `<svg viewBox="0 0 8 8" xmlns="http://www.w3.org/2000/svg">` +
		`<rect x="0" y="0" width="8" height="8" fill="blue"/>` +
		`<image x="2" y="2" width="4" height="4" preserveAspectRatio="none" href="` + uri + `"/>` +
		`<rect x="4" y="4" width="2" height="2" fill="white"/>` +
		`</svg>`
	img, _, err := renderSVGString(t, svg, 8, 8, StrictErrorMode)
	if err != nil {
		t.Fatal(err)
	}
	blue := color.RGBA{0, 0, 0xFF, 0xFF}
	white := color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}
	checkPixel(t, img, 1, 1, blue, 0)    // background rect
	checkPixel(t, img, 3, 3, testRed, 0) // image over the rect
	checkPixel(t, img, 5, 3, testGreen, 0)
	checkPixel(t, img, 5, 5, white, 0) // later rect over the image
}

func TestImageParentTransform(t *testing.T) {
	uri := pngDataURI(t, twoColorImage(4, 2, testRed, testGreen))
	svg := `<svg viewBox="0 0 20 10" xmlns="http://www.w3.org/2000/svg">` +
		`<g transform="translate(6,2)">` +
		`<image x="1" y="1" width="4" height="2" href="` + uri + `"/>` +
		`</g></svg>`
	img, _, err := renderSVGString(t, svg, 20, 10, StrictErrorMode)
	if err != nil {
		t.Fatal(err)
	}
	checkPixel(t, img, 8, 4, testRed, 0) // shifted by the group transform
	checkPixel(t, img, 10, 4, testGreen, 0)
	checkPixel(t, img, 6, 4, color.RGBA{0, 0, 0, 0}, 0) // left of the image
}

func TestImageSetTarget(t *testing.T) {
	uri := pngDataURI(t, twoColorImage(4, 2, testRed, testGreen))
	svg := `<svg viewBox="0 0 10 10" xmlns="http://www.w3.org/2000/svg">` +
		`<image x="2" y="3" width="4" height="2" href="` + uri + `"/>` +
		`</svg>`
	icon, err := ReadIconStream(strings.NewReader(svg), StrictErrorMode)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 20, 20))
	scanner := NewScannerGV(20, 20, img, img.Bounds())
	raster := NewDasher(20, 20, scanner)
	icon.SetTarget(0, 0, 20, 20) // 2x scale of the viewBox
	icon.Draw(raster, 1.0)
	checkPixel(t, img, 5, 7, testRed, 0) // device (4..8, 6..8) left half
	checkPixel(t, img, 10, 8, testGreen, 0)
	checkPixel(t, img, 3, 6, color.RGBA{0, 0, 0, 0}, 0)
}

func TestImagePreserveAspectRatio(t *testing.T) {
	uri := pngDataURI(t, twoColorImage(4, 2, testRed, testGreen))
	draw := func(par string) *image.RGBA {
		attr := ""
		if par != "" {
			attr = ` preserveAspectRatio="` + par + `"`
		}
		svg := `<svg viewBox="0 0 4 4" xmlns="http://www.w3.org/2000/svg">` +
			`<rect x="0" y="0" width="4" height="4" fill="white"/>` +
			`<image x="0" y="0" width="4" height="4"` + attr + ` href="` + uri + `"/>` +
			`</svg>`
		img, _, err := renderSVGString(t, svg, 4, 4, StrictErrorMode)
		if err != nil {
			t.Fatal(err)
		}
		return img
	}
	white := color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}

	// Default is xMidYMid meet: the 4x2 bitmap is centered in the 4x4
	// viewport, occupying rows 1 and 2.
	img := draw("")
	checkPixel(t, img, 1, 0, white, 0)
	checkPixel(t, img, 1, 1, testRed, 0)
	checkPixel(t, img, 3, 2, testGreen, 0)
	checkPixel(t, img, 1, 3, white, 0)

	// none: stretched to fill the whole viewport.
	img = draw("none")
	checkPixel(t, img, 1, 0, testRed, 0)
	checkPixel(t, img, 3, 3, testGreen, 0)

	// xMinYMin meet: top aligned, letterboxed at the bottom.
	img = draw("xMinYMin meet")
	checkPixel(t, img, 1, 0, testRed, 0)
	checkPixel(t, img, 1, 3, white, 0)

	// xMaxYMax meet: bottom aligned, letterboxed at the top.
	img = draw("xMaxYMax meet")
	checkPixel(t, img, 1, 0, white, 0)
	checkPixel(t, img, 3, 3, testGreen, 0)
}

func TestImageBadReferences(t *testing.T) {
	validURI := pngDataURI(t, twoColorImage(4, 2, testRed, testGreen))
	cases := map[string]string{
		"external http URI": `href="https://example.com/texture.png"`,
		"fragment id":       `href="#texture"`,
		"missing href":      ``,
		"bad base64":        `href="data:image/png;base64,%%%not-base64%%%"`,
		"not base64 coded":  `href="data:image/png,iVBORw0KGgo="`,
		"unsupported media": `href="data:image/gif;base64,R0lGODdhAQABAIAAAP///////yH5BAEAAAEALAAAAAABAAEAAAICTAEAOw=="`,
		"nested svg":        `href="data:image/svg+xml;base64,PHN2Zy8+"`,
		"giant pixel count": `href="` + giantPNGDataURI() + `"`,
		"truncated png":     `href="data:image/png;base64,iVBORw0KGgoAAAANSUhEUg"`,
	}
	for name, href := range cases {
		svg := `<svg viewBox="0 0 10 10" xmlns="http://www.w3.org/2000/svg">` +
			`<rect x="0" y="0" width="5" height="5" fill="red"/>` +
			`<image x="0" y="0" width="4" height="2" ` + href + `/></svg>`
		_, _, err := renderSVGString(t, svg, 10, 10, StrictErrorMode)
		if err == nil {
			t.Errorf("%s: expected an error in strict mode", name)
		}
		// In the default (ignore) mode the image is skipped and the rest
		// of the document still renders.
		img, icon, err := renderSVGString(t, svg, 10, 10)
		if err != nil {
			t.Errorf("%s: unexpected error in ignore mode: %v", name, err)
			continue
		}
		if len(icon.Images) != 0 {
			t.Errorf("%s: rejected image was recorded", name)
		}
		checkPixel(t, img, 2, 2, testRed, 0) // sibling rect still drawn
	}
	// A valid image next to a rejected one still renders.
	svg := `<svg viewBox="0 0 10 10" xmlns="http://www.w3.org/2000/svg">` +
		`<image x="0" y="0" width="4" height="2" href="https://example.com/x.png"/>` +
		`<image x="4" y="0" width="4" height="2" href="` + validURI + `"/></svg>`
	img, icon, err := renderSVGString(t, svg, 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(icon.Images) != 1 {
		t.Fatalf("expected 1 accepted image, got %d", len(icon.Images))
	}
	checkPixel(t, img, 5, 1, testRed, 0)
}

func TestImageZeroSize(t *testing.T) {
	uri := pngDataURI(t, twoColorImage(4, 2, testRed, testGreen))
	for _, wh := range []string{`width="0" height="2"`, `width="4" height="0"`, `width="4"`} {
		svg := `<svg viewBox="0 0 10 10" xmlns="http://www.w3.org/2000/svg">` +
			`<image x="0" y="0" ` + wh + ` href="` + uri + `"/></svg>`
		_, icon, err := renderSVGString(t, svg, 10, 10, StrictErrorMode)
		if err != nil {
			t.Errorf("%s: zero sized image should not be an error: %v", wh, err)
		}
		if len(icon.Images) != 0 {
			t.Errorf("%s: zero sized image should be skipped", wh)
		}
	}
}

func TestImageXlinkHref(t *testing.T) {
	uri := pngDataURI(t, twoColorImage(4, 2, testRed, testGreen))
	svg := `<svg viewBox="0 0 10 10" xmlns="http://www.w3.org/2000/svg" ` +
		`xmlns:xlink="http://www.w3.org/1999/xlink">` +
		`<image x="1" y="1" width="4" height="2" xlink:href="` + uri + `"/></svg>`
	img, _, err := renderSVGString(t, svg, 10, 10, StrictErrorMode)
	if err != nil {
		t.Fatal(err)
	}
	checkPixel(t, img, 2, 2, testRed, 0)
	checkPixel(t, img, 4, 2, testGreen, 0)
}

func TestImageEmbeddedSample(t *testing.T) {
	// Visual sample: a two-color PNG and a two-color JPEG composited with
	// vector shapes, saved for inspection like the other icon tests.
	pngURI := pngDataURI(t, twoColorImage(8, 8, testRed, testGreen))
	jpgURI := jpegDataURI(t, twoColorImage(8, 8, testGreen, testRed))
	svg := `<svg viewBox="0 0 48 24" xmlns="http://www.w3.org/2000/svg">` +
		`<rect x="0" y="0" width="48" height="24" fill="white"/>` +
		`<circle cx="12" cy="12" r="10" fill="lightblue"/>` +
		`<image x="4" y="4" width="16" height="16" preserveAspectRatio="none" href="` + pngURI + `"/>` +
		`<image x="28" y="4" width="16" height="16" opacity="0.75" href="` + jpgURI + `"/>` +
		`</svg>`
	icon, err := ReadIconStream(strings.NewReader(svg), StrictErrorMode)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 48, 24))
	scanner := NewScannerGV(48, 24, img, img.Bounds())
	raster := NewDasher(48, 24, scanner)
	icon.Draw(raster, 1.0)
	if err := SaveToPngFile("testdata/image_embed.png", img); err != nil {
		t.Error(err)
	}
}
