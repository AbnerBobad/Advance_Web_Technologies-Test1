// Package imageproc turns an accepted original into the fixed Version 1
// variant set: a square thumbnail, and aspect-preserving preview and display
// versions. It uses only the standard library (image/jpeg and image/png) and
// performs no client-configurable transformations.
package imageproc

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"math"
)

// Result is one generated variant ready to be stored by the worker. Width and
// Height are the actual output dimensions, which may be smaller than the
// contract bounds (never upscaled, and never stretched).
type Result struct {
	Name   string
	Width  int
	Height int
	Data   []byte
}

// spec is one row of the fixed variant profile.
type spec struct {
	name  string
	thumb bool // exact square crop
	maxW  int  // fit bound (width)
	maxH  int  // fit bound (height)
}

// profile is the authoritative Version 1 variant set, in generation order.
var profile = []spec{
	{name: "thumbnail", thumb: true, maxW: 150, maxH: 150},
	{name: "preview", maxW: 800, maxH: 600},
	{name: "display", maxW: 1200, maxH: 900},
}

// Generate decodes src and produces every variant in the profile, preserving
// the original's media format. The returned results are complete or none are:
// the first error aborts generation so the worker never stores a partial set.
func Generate(src []byte) ([]Result, error) {
	source, format, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, fmt.Errorf("decode source image: %w", err)
	}
	if format != "jpeg" && format != "png" {
		return nil, fmt.Errorf("unsupported image format %q", format)
	}

	results := make([]Result, 0, len(profile))
	for _, s := range profile {
		out := render(source, s)
		encoded, err := encode(out, format)
		if err != nil {
			return nil, fmt.Errorf("encode %s variant: %w", s.name, err)
		}
		results = append(results, Result{
			Name:   s.name,
			Width:  out.Bounds().Dx(),
			Height: out.Bounds().Dy(),
			Data:   encoded,
		})
	}
	return results, nil
}

// render applies one variant contract to the source image.
func render(src image.Image, s spec) image.Image {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()

	if s.thumb {
		// Exact square: crop the centered square region of the original,
		// then scale it down to the fixed 150x150 target.
		side := w
		if h < side {
			side = h
		}
		x0 := (w - side) / 2
		y0 := (h - side) / 2

		square := image.NewRGBA(image.Rect(0, 0, side, side))
		draw.Draw(square, square.Bounds(), src,
			image.Point{X: src.Bounds().Min.X + x0, Y: src.Bounds().Min.Y + y0}, draw.Src)
		return resize(square, s.maxW, s.maxH)
	}

	// Fit within the maximum bounds preserving aspect ratio. Never upscale:
	// an image that already fits is returned unchanged, so the actual output
	// dimensions may be smaller than the contract maximum.
	scale := math.Min(float64(s.maxW)/float64(w), float64(s.maxH)/float64(h))
	if scale >= 1 {
		return src
	}
	dw := int(math.Round(float64(w) * scale))
	dh := int(math.Round(float64(h) * scale))
	if dw < 1 {
		dw = 1
	}
	if dh < 1 {
		dh = 1
	}
	return resize(src, dw, dh)
}

// resize scales src to dstW x dstH with bilinear interpolation. It never
// samples outside the source bounds, so edges are clamped rather than blurred.
func resize(src image.Image, dstW, dstH int) image.Image {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))

	sw := float64(b.Dx())
	sh := float64(b.Dy())

	for y := 0; y < dstH; y++ {
		// Centre of the destination pixel mapped back into source space.
		sy := (float64(y)+0.5)*sh/float64(dstH) - 0.5
		if sy < 0 {
			sy = 0
		}
		y0 := int(math.Floor(sy))
		y1 := y0 + 1
		if y1 > b.Dy()-1 {
			y1 = b.Dy() - 1
		}
		fy := sy - float64(y0)

		for x := 0; x < dstW; x++ {
			sx := (float64(x)+0.5)*sw/float64(dstW) - 0.5
			if sx < 0 {
				sx = 0
			}
			x0 := int(math.Floor(sx))
			x1 := x0 + 1
			if x1 > b.Dx()-1 {
				x1 = b.Dx() - 1
			}
			fx := sx - float64(x0)

			// Bilinear blend of the four surrounding source pixels.
			c00 := rgbAt(src, b.Min.X+x0, b.Min.Y+y0)
			c10 := rgbAt(src, b.Min.X+x1, b.Min.Y+y0)
			c01 := rgbAt(src, b.Min.X+x0, b.Min.Y+y1)
			c11 := rgbAt(src, b.Min.X+x1, b.Min.Y+y1)

			top := lerp(c00, c10, fx)
			bottom := lerp(c01, c11, fx)
			pixel := lerp(top, bottom, fy)

			dst.Set(x, y, pixel)
		}
	}
	return dst
}

func rgbAt(src image.Image, x, y int) color.RGBA {
	return color.RGBAModel.Convert(src.At(x, y)).(color.RGBA)
}

func lerp(a, b color.RGBA, t float64) color.RGBA {
	inv := 1 - t
	return color.RGBA{
		R: uint8(float64(a.R)*inv + float64(b.R)*t),
		G: uint8(float64(a.G)*inv + float64(b.G)*t),
		B: uint8(float64(a.B)*inv + float64(b.B)*t),
		A: uint8(float64(a.A)*inv + float64(b.A)*t),
	}
}

// encode renders an image in the same format as the original input.
func encode(img image.Image, format string) ([]byte, error) {
	var buf bytes.Buffer
	switch format {
	case "jpeg":
		err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85})
		return buf.Bytes(), err
	case "png":
		err := png.Encode(&buf, img)
		return buf.Bytes(), err
	default:
		return nil, fmt.Errorf("unsupported image format %q", format)
	}
}
