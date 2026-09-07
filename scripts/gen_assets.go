// Command gen_assets writes the ImageLab smoke-test image fixtures into the
// directory given as its first argument. It uses only the Go standard library
// so the smoke test has no Python or Pillow dependency.
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run scripts/gen_assets.go <output-dir>")
		os.Exit(2)
	}
	dir := os.Args[1]
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fatal(err)
	}

	// A real 1200x800 image backing the valid PNG and JPEG fixtures.
	img := newTestImage(1200, 800)

	writePNG(filepath.Join(dir, "photo.png"), img)
	writeJPEG(filepath.Join(dir, "photo.jpg"), img)

	// A text file named .png: server must reject it even though the extension
	// (and a client Content-Type) would claim it is an image.
	if err := os.WriteFile(filepath.Join(dir, "fake.png"), []byte("this is not an image"), 0o644); err != nil {
		fatal(err)
	}

	// Eleven MB of garbage: server must reject it as oversize before decoding.
	if err := os.WriteFile(filepath.Join(dir, "big.bin"), make([]byte, 11*1024*1024), 0o644); err != nil {
		fatal(err)
	}

	// A truncated JPEG: valid header, broken body, so decoding must fail.
	jpg, err := os.ReadFile(filepath.Join(dir, "photo.jpg"))
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.jpg"), jpg[:400], 0o644); err != nil {
		fatal(err)
	}
}

func newTestImage(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{30, 90, 200, 255})
		}
	}
	return img
}

func writePNG(path string, img image.Image) {
	f, err := os.Create(path)
	if err != nil {
		fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		fatal(err)
	}
}

func writeJPEG(path string, img image.Image) {
	f, err := os.Create(path)
	if err != nil {
		fatal(err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 85}); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}