// Copyright 2026 The oksvg Authors. All rights reserved.
//
// svg_text.go implements basic drawing of SVG text and tspan elements.
// Text is converted to vector paths at parse time using a caller-provided
// font, so the resulting glyphs compose with shapes in document order and
// inherit the element's style, transform and opacity like any other path.

package oksvg

import (
	"encoding/xml"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/srwiley/rasterx"
	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// textRun is a fragment of text sharing a single style within a text element.
// A run starts at the text element itself and at every tspan start and end tag.
type textRun struct {
	style        PathStyle
	text         []byte // raw character data, whitespace is collapsed at layout time
	x, y, dx, dy float64
	hasX, hasY   bool
}

// textContext accumulates the content of one text element while parsing.
type textContext struct {
	style        PathStyle // style of the text element, used for anchoring
	x, y, dx, dy float64
	runs         []textRun
	inTspan      bool
}

// appendText adds character data to the current run.
func (tctx *textContext) appendText(s string) {
	if len(tctx.runs) == 0 {
		return
	}
	tctx.runs[len(tctx.runs)-1].text = append(tctx.runs[len(tctx.runs)-1].text, s...)
}

// unsupportedTextAttrs are complex text layout attributes that this basic
// implementation cannot honor; encountering one produces a warning
// (or an error in StrictErrorMode) instead of being silently ignored.
var unsupportedTextAttrs = map[string]bool{
	"rotate": true, "textlength": true, "lengthadjust": true,
	"letter-spacing": true, "word-spacing": true, "writing-mode": true,
	"text-orientation": true, "glyph-orientation-horizontal": true,
	"glyph-orientation-vertical": true, "dominant-baseline": true,
	"alignment-baseline": true, "baseline-shift": true,
}

// textError reports a text related problem according to the cursor's ErrorMode.
// It returns a non-nil error only in StrictErrorMode.
func (c *IconCursor) textError(msg string) error {
	if c.returnError("oksvg: " + msg) {
		return errors.New("oksvg: " + msg)
	}
	return nil
}

// parseTextPosition parses x, y, dx and dy attributes of a text or tspan element.
// Only the first value of a coordinate list is used.
func (c *IconCursor) parseTextPosition(attrs []xml.Attr, x, y, dx, dy *float64, hasX, hasY *bool) error {
	for _, attr := range attrs {
		var target *float64
		switch strings.ToLower(attr.Name.Local) {
		case "x":
			target, *hasX = x, true
		case "y":
			target, *hasY = y, true
		case "dx":
			target = dx
		case "dy":
			target = dy
		default:
			if unsupportedTextAttrs[strings.ToLower(attr.Name.Local)] {
				if err := c.textError(fmt.Sprintf("text attribute %q is not supported and was ignored", attr.Name.Local)); err != nil {
					return err
				}
			}
			continue
		}
		parts := splitOnCommaOrSpace(strings.TrimSpace(attr.Value))
		if len(parts) == 0 {
			continue
		}
		if len(parts) > 1 {
			if err := c.textError(fmt.Sprintf("only the first value of text attribute %q is used", attr.Name.Local)); err != nil {
				return err
			}
		}
		v, err := parseFloat(parts[0], 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			if err := c.textError(fmt.Sprintf("invalid value %q for text attribute %q, using 0", attr.Value, attr.Name.Local)); err != nil {
				return err
			}
			continue
		}
		*target = v
	}
	return nil
}

// textF starts collection of a text element's content.
var textF svgFunc = func(c *IconCursor, attrs []xml.Attr) error {
	if c.inText {
		return c.textError("nested text elements are not supported")
	}
	tctx := &textContext{style: c.StyleStack[len(c.StyleStack)-1]}
	if err := c.parseTextPosition(attrs, &tctx.x, &tctx.y, &tctx.dx, &tctx.dy, new(bool), new(bool)); err != nil {
		return err
	}
	// The first run collects character data placed directly in the text element.
	tctx.runs = append(tctx.runs, textRun{style: tctx.style})
	c.text = tctx
	c.inText = true
	return nil
}

// tspanF starts a new run within the current text element.
// Only a single level of tspan elements is supported.
var tspanF svgFunc = func(c *IconCursor, attrs []xml.Attr) error {
	if !c.inText || c.text == nil {
		return c.textError("tspan element outside of a text element was ignored")
	}
	if c.text.inTspan {
		return c.textError("nested tspan elements are not supported")
	}
	run := textRun{style: c.StyleStack[len(c.StyleStack)-1]}
	if err := c.parseTextPosition(attrs, &run.x, &run.y, &run.dx, &run.dy, &run.hasX, &run.hasY); err != nil {
		return err
	}
	c.text.runs = append(c.text.runs, run)
	c.text.inTspan = true
	return nil
}

// endTspan closes the current tspan run and starts a continuation run
// with the text element's own style for any following character data.
func (c *IconCursor) endTspan() {
	if !c.inText || c.text == nil || !c.text.inTspan {
		return
	}
	c.text.inTspan = false
	c.text.runs = append(c.text.runs, textRun{style: c.text.style})
}

// finalizeText lays out the collected runs of the current text element,
// converts the glyphs to paths using the cursor's font and appends them
// to the icon's paths, preserving document order with shapes.
func (c *IconCursor) finalizeText() error {
	tctx := c.text
	c.inText = false
	c.text = nil
	if tctx == nil {
		return nil
	}
	runs := collapseTextRuns(tctx.runs)
	if len(runs) == 0 {
		return nil
	}
	if c.font == nil {
		return c.textError("cannot draw text element: no font was provided; " +
			"use ReadIconWithFont or ReadIconStreamWithFont")
	}
	// Check font sizes before measuring.
	for i := range runs {
		fs := runs[i].style.FontSize
		if fs <= 0 || math.IsNaN(fs) || math.IsInf(fs, 0) {
			if err := c.textError(fmt.Sprintf("invalid font size %v, text was not drawn", fs)); err != nil {
				return err
			}
			runs[i].text = nil
		}
	}
	buf := &sfnt.Buffer{}
	// Measure the total advance of the element for text-anchor. The text
	// element's own dx/dy shift the anchor point and are not part of the width.
	total := 0.0
	for i := range runs {
		total += runs[i].dx + c.runAdvance(buf, &runs[i])
	}
	penX, penY := tctx.x+tctx.dx, tctx.y+tctx.dy
	switch tctx.style.TextAnchor {
	case TextAnchorMiddle:
		penX -= total / 2
	case TextAnchorEnd:
		penX -= total
	}
	for i := range runs {
		run := &runs[i]
		if len(run.text) == 0 {
			continue
		}
		if run.hasX {
			penX = run.x
		}
		if run.hasY {
			penY = run.y
		}
		penX += run.dx
		penY += run.dy
		var path rasterx.Path
		var adv float64
		path, adv = c.renderRun(buf, run, penX, penY)
		if len(path) > 0 {
			c.icon.SVGPaths = append(c.icon.SVGPaths, SvgPath{run.style, path})
		}
		penX += adv
	}
	return nil
}

// glyphIndex returns the glyph for r, warning once per missing rune and
// falling back to the font's .notdef glyph (index 0).
func (c *IconCursor) glyphIndex(buf *sfnt.Buffer, r rune) sfnt.GlyphIndex {
	idx, err := c.font.GlyphIndex(buf, r)
	if err != nil {
		idx = 0
	}
	if idx == 0 && r != 0 {
		if c.missingGlyphs == nil {
			c.missingGlyphs = make(map[rune]bool)
		}
		if !c.missingGlyphs[r] {
			c.missingGlyphs[r] = true
			_ = c.textError(fmt.Sprintf("font has no glyph for rune %q, using .notdef", r))
		}
	}
	return idx
}

// runAdvance measures the advance width of a run in user units.
func (c *IconCursor) runAdvance(buf *sfnt.Buffer, run *textRun) float64 {
	ppem := fixed.Int26_6(run.style.FontSize*64 + 0.5)
	var adv fixed.Int26_6
	var prev sfnt.GlyphIndex
	var havePrev bool
	for _, r := range string(run.text) {
		idx := c.glyphIndex(buf, r)
		if havePrev {
			if k, err := c.font.Kern(buf, prev, idx, ppem, font.HintingNone); err == nil {
				adv += k
			}
		}
		a, err := c.font.GlyphAdvance(buf, idx, ppem, font.HintingNone)
		if err != nil {
			_ = c.textError(fmt.Sprintf("cannot measure glyph for rune %q: %v", r, err))
			a = ppem / 2
		}
		adv += a
		prev, havePrev = idx, true
	}
	return float64(adv) / 64
}

// renderRun converts a run's glyphs into a rasterx.Path positioned at
// baseline origin (penX, penY) and returns the path and the total advance.
func (c *IconCursor) renderRun(buf *sfnt.Buffer, run *textRun, penX, penY float64) (rasterx.Path, float64) {
	ppem := fixed.Int26_6(run.style.FontSize*64 + 0.5)
	startX := penX
	var path rasterx.Path
	var prev sfnt.GlyphIndex
	var havePrev bool
	for _, r := range string(run.text) {
		idx := c.glyphIndex(buf, r)
		if havePrev {
			if k, err := c.font.Kern(buf, prev, idx, ppem, font.HintingNone); err == nil {
				penX += float64(k) / 64
			}
		}
		segs, err := c.font.LoadGlyph(buf, idx, ppem, nil)
		if err != nil {
			_ = c.textError(fmt.Sprintf("cannot load glyph for rune %q: %v", r, err))
		} else {
			appendGlyphSegments(&path, segs, penX, penY)
		}
		a, err := c.font.GlyphAdvance(buf, idx, ppem, font.HintingNone)
		if err != nil {
			a = ppem / 2
		}
		penX += float64(a) / 64
		prev, havePrev = idx, true
	}
	return path, penX - startX
}

// appendGlyphSegments adds glyph outline segments, translated so the glyph's
// origin sits at (dx, dy), to the path. Glyph coordinates use a downward Y
// axis, matching the SVG coordinate system.
func appendGlyphSegments(p *rasterx.Path, segs sfnt.Segments, dx, dy float64) {
	ox, oy := fixed.Int26_6(dx*64), fixed.Int26_6(dy*64)
	tr := func(pt fixed.Point26_6) fixed.Point26_6 {
		return fixed.Point26_6{X: pt.X + ox, Y: pt.Y + oy}
	}
	for _, seg := range segs {
		switch seg.Op {
		case sfnt.SegmentOpMoveTo:
			p.Start(tr(seg.Args[0]))
		case sfnt.SegmentOpLineTo:
			p.Line(tr(seg.Args[0]))
		case sfnt.SegmentOpQuadTo:
			p.QuadBezier(tr(seg.Args[0]), tr(seg.Args[1]))
		case sfnt.SegmentOpCubeTo:
			p.CubeBezier(tr(seg.Args[0]), tr(seg.Args[1]), tr(seg.Args[2]))
		}
	}
}

// collapseTextRuns applies SVG's default white space handling to the runs of
// a text element: sequences of white space collapse to a single space, and
// leading and trailing white space of the whole element is removed.
// Runs left without text are dropped.
func collapseTextRuns(runs []textRun) []textRun {
	out := runs[:0]
	prevEndsWithSpace := true // strips leading white space of the element
	pendingSpace := false     // a white space sequence was seen since the last written character
	for i := range runs {
		var sb strings.Builder
		for _, r := range string(runs[i].text) {
			switch r {
			case ' ', '\t', '\n', '\r':
				pendingSpace = true
			default:
				if pendingSpace && !prevEndsWithSpace {
					sb.WriteByte(' ')
				}
				sb.WriteRune(r)
				prevEndsWithSpace = false
				pendingSpace = false
			}
		}
		if sb.Len() == 0 {
			continue // a pending space carries over to the next run
		}
		runs[i].text = []byte(sb.String())
		out = append(out, runs[i])
	}
	// Trailing white space is never emitted because a pending space is only
	// written once a non-space character follows it.
	return out
}
