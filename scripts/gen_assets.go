// Command gen_assets writes the fixture files scripts/smoke.sh needs
// (a valid PNG, a valid JPEG, an invalid file with a .png extension, an
// oversized file, and a truncated/broken JPEG) using only the Go standard
// library. It replaces a Python + Pillow dependency that smoke.sh used to
// require.
package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: gen_assets <output_dir>")
		os.Exit(1)
	}
	dir := os.Args[1]

	// A plain solid-color 1200x800 image is enough to exercise validation
	// and storage; the smoke test never inspects pixel content.
	img := image.NewRGBA(image.Rect(0, 0, 1200, 800))
	fill := color.RGBA{R: 30, G: 90, B: 200, A: 255}
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			img.Set(x, y, fill)
		}
	}

	// photo.png: valid PNG.
	pngFile, err := os.Create(filepath.Join(dir, "photo.png"))
	must(err)
	must(png.Encode(pngFile, img))
	must(pngFile.Close())

	// photo.jpg: valid JPEG. Kept in memory too, so broken.jpg can be cut
	// from the same encoded bytes.
	var jpgBuf bytes.Buffer
	must(jpeg.Encode(&jpgBuf, img, &jpeg.Options{Quality: 90}))
	must(os.WriteFile(filepath.Join(dir, "photo.jpg"), jpgBuf.Bytes(), 0o644))

	// fake.png: plain text with a .png extension. Must be rejected by the
	// server's content-sniffing, not accepted based on the extension.
	must(os.WriteFile(filepath.Join(dir, "fake.png"), []byte("this is not an image"), 0o644))

	// big.bin: 11 MB, one MB past the server's 10 MB limit.
	big := make([]byte, 11*1024*1024)
	must(os.WriteFile(filepath.Join(dir, "big.bin"), big, 0o644))

	// broken.jpg: the first 400 bytes of a real JPEG, truncated so it looks
	// like a JPEG by header but fails to decode.
	n := 400
	if jpgBytes := jpgBuf.Bytes(); len(jpgBytes) < n {
		n = len(jpgBytes)
	}
	must(os.WriteFile(filepath.Join(dir, "broken.jpg"), jpgBuf.Bytes()[:n], 0o644))

	fmt.Println("test assets written to", dir)
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "gen_assets error:", err)
		os.Exit(1)
	}
}
