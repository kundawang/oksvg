// Copyright 2026 The oksvg Authors. All rights reserved.
package oksvg_test

import (
	"bytes"
	"image"
	"image/color"
	"log"
	"strings"
	"testing"

	. "github.com/srwiley/oksvg"
	. "github.com/srwiley/rasterx"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

func loadMonoFont(t *testing.T) *sfnt.Font {
	t.Helper()
	f, err := opentype.Parse(gomono.TTF)
	if err != nil {
		t.Fatal("cannot parse test font:", err)
	}
	return f
}

// monoAdvance returns the advance width of one Go Mono glyph at the given
// font size. Go Mono is monospaced, so every glyph has the same advance.
func monoAdvance(t *testing.T, f *sfnt.Font, size float64) float64 {
	t.Helper()
	buf := &sfnt.Buffer{}
	idx, err := f.GlyphIndex(buf, 'A')
	if err != nil {
		t.Fatal(err)
	}
	adv, err := f.GlyphAdvance(buf, idx, fixed.Int26_6(size*64), font.HintingNone)
	if err != nil {
		t.Fatal(err)
	}
	return float64(adv) / 64
}

func drawTextIcon(t *testing.T, icon *SvgIcon) *image.RGBA {
	t.Helper()
	w, h := int(icon.ViewBox.W), int(icon.ViewBox.H)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	scannerGV := NewScannerGV(w, h, img, img.Bounds())
	raster := NewDasher(w, h, scannerGV)
	icon.Draw(raster, 1.0)
	return img
}

// colorBBox returns the bounding box of all pixels exactly matching c,
// and whether any such pixel was found.
func colorBBox(img *image.RGBA, c color.RGBA) (minX, minY, maxX, maxY int, found bool) {
	minX, minY = img.Bounds().Max.X, img.Bounds().Max.Y
	maxX, maxY = img.Bounds().Min.X, img.Bounds().Min.Y
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			if img.RGBAAt(x, y) == c {
				found = true
				if x < minX {
					minX = x
				}
				if x > maxX {
					maxX = x
				}
				if y < minY {
					minY = y
				}
				if y > maxY {
					maxY = y
				}
			}
		}
	}
	return
}

func inRange(t *testing.T, name string, v, lo, hi float64) {
	t.Helper()
	if v < lo || v > hi {
		t.Errorf("%s = %v, expected in [%v, %v]", name, v, lo, hi)
	}
}

// TestTextDraw verifies pixel positions, colors and anchoring of text and
// tspan elements rendered with a fixed (monospace) font.
func TestTextDraw(t *testing.T) {
	f := loadMonoFont(t)
	icon, err := ReadIconWithFont("testdata/TestText.svg", f, WarnErrorMode)
	if err != nil {
		t.Fatal(err)
	}
	img := drawTextIcon(t, icon)

	adv20 := monoAdvance(t, f, 20)
	adv16 := monoAdvance(t, f, 16)

	red := color.RGBA{0xFF, 0, 0, 0xFF}
	blue := color.RGBA{0, 0, 0xFF, 0xFF}
	green := color.RGBA{0, 0xFF, 0, 0xFF}
	magenta := color.RGBA{0xFF, 0, 0xFF, 0xFF}
	white := color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}

	// The white background rect is drawn first, text composited over it.
	if got := img.RGBAAt(5, 5); got != white {
		t.Errorf("background pixel = %v, expected white", got)
	}

	// Red "AB" starts at x=10, baseline y=30, font-size 20.
	rMinX, rMinY, rMaxX, _, found := colorBBox(img, red)
	if !found {
		t.Fatal("no red text pixels found")
	}
	inRange(t, "red minX", float64(rMinX), 10, 13)
	inRange(t, "red maxX", float64(rMaxX), 10+adv20, 10+2*adv20)
	inRange(t, "red minY", float64(rMinY), 30-20, 30-10)

	// Blue "CD" in a tspan follows the two red glyphs plus dx=10, shifted dy=5 down.
	bMinX, bMinY, _, _, found := colorBBox(img, blue)
	if !found {
		t.Fatal("no blue tspan pixels found")
	}
	inRange(t, "blue minX", float64(bMinX), 10+2*adv20+10-3, 10+2*adv20+10+4)
	inRange(t, "blue minY offset", float64(bMinY-rMinY), 5-3, 5+3)

	// Green "EF" with text-anchor=middle is centered on x=110.
	gMinX, _, gMaxX, _, found := colorBBox(img, green)
	if !found {
		t.Fatal("no green text pixels found")
	}
	inRange(t, "green center", float64(gMinX+gMaxX)/2, 110-2, 110+2)
	inRange(t, "green minX", float64(gMinX), 110-adv16-3, 110-adv16+3)

	// Magenta "GH" with text-anchor=end ends at x=210.
	mMinX, _, mMaxX, _, found := colorBBox(img, magenta)
	if !found {
		t.Fatal("no magenta text pixels found")
	}
	inRange(t, "magenta maxX", float64(mMaxX), 210-6, 210)
	inRange(t, "magenta minX", float64(mMinX), 210-2*adv16-4, 210-2*adv16+4)

	// Black "Hi": surrounding white space and newlines are collapsed and
	// stripped, so the text starts at x=10. At font-size 12 the thin stems
	// may not reach full coverage, so match dark pixels by threshold.
	kMinX, found := img.Bounds().Max.X, false
	for y := 30; y < 50; y++ {
		for x := 0; x < 60; x++ {
			if c := img.RGBAAt(x, y); c.R < 128 && c.G < 128 && c.B < 128 {
				found = true
				if x < kMinX {
					kMinX = x
				}
			}
		}
	}
	if !found {
		t.Fatal("no black text pixels found")
	}
	inRange(t, "black minX", float64(kMinX), 10, 13)
}

// captureLog redirects the standard logger while f runs and returns what was logged.
func captureLog(f func()) string {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(nil)
	f()
	return buf.String()
}

// TestTextNoFont verifies that text elements are skipped with a warning
// when no font is provided.
func TestTextNoFont(t *testing.T) {
	const doc = `<svg viewBox="0 0 20 20"><text x="2" y="10">Hi</text></svg>`
	var icon *SvgIcon
	var err error
	logs := captureLog(func() {
		icon, err = ReadIconStream(strings.NewReader(doc), WarnErrorMode)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(icon.SVGPaths) != 0 {
		t.Error("text was drawn without a font")
	}
	if !strings.Contains(logs, "no font") {
		t.Error("missing no-font warning, got:", logs)
	}
}

// TestTextMissingGlyph verifies that runes missing from the font produce a
// warning and fall back to the .notdef glyph without aborting the parse.
func TestTextMissingGlyph(t *testing.T) {
	f := loadMonoFont(t)
	const doc = `<svg viewBox="0 0 60 20"><text x="2" y="15" font-size="12">A世B</text></svg>`
	var icon *SvgIcon
	var err error
	logs := captureLog(func() {
		icon, err = ReadIconStreamWithFont(strings.NewReader(doc), f, WarnErrorMode)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(icon.SVGPaths) == 0 {
		t.Error("text with a missing glyph was not drawn at all")
	}
	if !strings.Contains(logs, "no glyph") {
		t.Error("missing glyph warning not logged, got:", logs)
	}
}

// TestTextBadFontSize verifies that an unparsable font-size falls back to
// the default size with a warning instead of aborting the parse.
func TestTextBadFontSize(t *testing.T) {
	f := loadMonoFont(t)
	const doc = `<svg viewBox="0 0 60 20"><text x="2" y="15" font-size="bogus">Hi</text></svg>`
	var icon *SvgIcon
	var err error
	logs := captureLog(func() {
		icon, err = ReadIconStreamWithFont(strings.NewReader(doc), f, WarnErrorMode)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(icon.SVGPaths) == 0 {
		t.Error("text with invalid font-size was not drawn with the fallback size")
	}
	if !strings.Contains(logs, "font-size") {
		t.Error("font-size warning not logged, got:", logs)
	}
}

// TestTextNestedTspan verifies that nested tspans warn but their character
// data is still drawn as part of the parent run.
func TestTextNestedTspan(t *testing.T) {
	f := loadMonoFont(t)
	const doc = `<svg viewBox="0 0 60 20"><text x="2" y="15" font-size="12">A<tspan>B<tspan>C</tspan></tspan></text></svg>`
	var icon *SvgIcon
	var err error
	logs := captureLog(func() {
		icon, err = ReadIconStreamWithFont(strings.NewReader(doc), f, WarnErrorMode)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(icon.SVGPaths) == 0 {
		t.Error("text with nested tspan was not drawn")
	}
	if !strings.Contains(logs, "nested tspan") {
		t.Error("nested tspan warning not logged, got:", logs)
	}
}

// TestTextInheritance verifies that font-size, fill, opacity and transforms
// are inherited from ancestor elements.
func TestTextInheritance(t *testing.T) {
	f := loadMonoFont(t)
	const doc = `<svg viewBox="0 0 100 100">` +
		`<g fill="#FF0000" font-size="20" transform="translate(10, 20)">` +
		`<text x="5" y="5">A</text></g></svg>`
	icon, err := ReadIconStreamWithFont(strings.NewReader(doc), f, WarnErrorMode)
	if err != nil {
		t.Fatal(err)
	}
	img := drawTextIcon(t, icon)
	red := color.RGBA{0xFF, 0, 0, 0xFF}
	minX, minY, _, _, found := colorBBox(img, red)
	if !found {
		t.Fatal("inherited red text not drawn")
	}
	// x=5 translated by 10 -> 15, glyph top near y=5+20-ascent.
	inRange(t, "inherited minX", float64(minX), 15, 18)
	inRange(t, "inherited minY", float64(minY), 25-20, 25-8)
}

// TestTextStrictMode verifies that StrictErrorMode turns text problems into errors.
func TestTextStrictMode(t *testing.T) {
	const doc = `<svg viewBox="0 0 20 20"><text x="2" y="10">Hi</text></svg>`
	_, err := ReadIconStream(strings.NewReader(doc), StrictErrorMode)
	if err == nil {
		t.Error("expected error for text without font in strict mode")
	}
}
