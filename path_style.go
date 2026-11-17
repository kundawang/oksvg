// Copyright 2017 The oksvg Authors. All rights reserved.
// created: 2/12/2017 by S.R.Wiley
//
// utils.go implements translation of an SVG2.0 path into a rasterx Path.

package oksvg

import (
	"image/color"

	"github.com/srwiley/rasterx"
)

// TextAnchorMode specifies how a text element is horizontally
// anchored relative to its x position.
type TextAnchorMode uint8

const (
	// TextAnchorStart aligns the start of the text with the x position (default).
	TextAnchorStart TextAnchorMode = iota
	// TextAnchorMiddle centers the text on the x position.
	TextAnchorMiddle
	// TextAnchorEnd aligns the end of the text with the x position.
	TextAnchorEnd
)

// PathStyle holds the state of the SVG style.
type PathStyle struct {
	FillOpacity, LineOpacity          float64
	LineWidth, DashOffset, MiterLimit float64
	Dash                              []float64
	UseNonZeroWinding                 bool
	fillerColor, linerColor           interface{} // either color.Color or rasterx.Gradient
	LineGap                           rasterx.GapFunc
	LeadLineCap                       rasterx.CapFunc // This is used if different than LineCap
	LineCap                           rasterx.CapFunc
	LineJoin                          rasterx.JoinMode
	mAdder                            rasterx.MatrixAdder // current transform
	FontSize                          float64             // font size in user units, used by text elements
	TextAnchor                        TextAnchorMode      // horizontal anchoring of text elements
}

// styleAttribute describes draw options, such as {"fill":"black"; "stroke":"white"}.
type styleAttribute = map[string]string

// DefaultStyle sets the default PathStyle to fill black, winding rule,
// full opacity, no stroke, ButtCap line end and Bevel line connect,
// a 16 unit font size and start text anchoring.
var DefaultStyle = PathStyle{
	FillOpacity: 1.0, LineOpacity: 1.0,
	LineWidth: 2.0, DashOffset: 0.0, MiterLimit: 4.0,
	Dash:              nil,
	UseNonZeroWinding: true,
	fillerColor:       color.NRGBA{0x00, 0x00, 0x00, 0xff},
	linerColor:        nil,
	LineGap:           nil,
	LeadLineCap:       nil,
	LineCap:           rasterx.ButtCap,
	LineJoin:          rasterx.Bevel,
	mAdder:            rasterx.MatrixAdder{M: rasterx.Identity},
	FontSize:          16.0,
	TextAnchor:        TextAnchorStart,
}
