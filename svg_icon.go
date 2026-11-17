// Copyright 2017 The oksvg Authors. All rights reserved.
// created: 2/12/2017 by S.R.Wiley
//
// utils.go implements translation of an SVG2.0 path into a rasterx Path.

package oksvg

import (
	"github.com/srwiley/rasterx"
)

// SvgIcon holds data from parsed SVGs.
type SvgIcon struct {
	ViewBox      struct{ X, Y, W, H float64 }
	Titles       []string // Title elements collect here
	Descriptions []string // Description elements collect here
	Grads        map[string]*rasterx.Gradient
	Defs         map[string][]definition
	SVGPaths     []SvgPath
	Images       []SvgImage // embedded bitmaps, composited with SVGPaths in document order
	Transform    rasterx.Matrix2D
	classes      map[string]styleAttribute
	drawOrder    []drawOrderEntry // document order of SVGPaths and Images
}

// drawOrderEntry records whether the next parsed element was a path or an
// image, and its index in the corresponding slice.
type drawOrderEntry struct {
	isImage bool
	index   int
}

func (s *SvgIcon) appendPath(svgp SvgPath) {
	s.SVGPaths = append(s.SVGPaths, svgp)
	s.drawOrder = append(s.drawOrder, drawOrderEntry{false, len(s.SVGPaths) - 1})
}

func (s *SvgIcon) appendImage(img SvgImage) {
	s.Images = append(s.Images, img)
	s.drawOrder = append(s.drawOrder, drawOrderEntry{true, len(s.Images) - 1})
}

// Draw the compiled SVG icon into the GraphicContext.
// All elements should be contained by the Bounds rectangle of the SvgIcon.
func (s *SvgIcon) Draw(r *rasterx.Dasher, opacity float64) {
	if len(s.drawOrder) == 0 {
		// The icon was not assembled by the parser (e.g. built by hand);
		// draw the paths as before.
		for _, svgp := range s.SVGPaths {
			svgp.DrawTransformed(r, opacity, s.Transform)
		}
		return
	}
	for _, e := range s.drawOrder {
		if e.isImage {
			s.Images[e.index].DrawTransformed(r, opacity, s.Transform)
		} else {
			s.SVGPaths[e.index].DrawTransformed(r, opacity, s.Transform)
		}
	}
}

// SetTarget sets the Transform matrix to draw within the bounds of the rectangle arguments
func (s *SvgIcon) SetTarget(x, y, w, h float64) {
	scaleW := w / s.ViewBox.W
	scaleH := h / s.ViewBox.H
	s.Transform = rasterx.Identity.Translate(x-s.ViewBox.X, y-s.ViewBox.Y).Scale(scaleW, scaleH)
}
