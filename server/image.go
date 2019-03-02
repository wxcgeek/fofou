package server

import (
	"image"
	"image/jpeg"
	_ "image/png"
	"math"
	"os"
	"strings"
)

func NaiveDownscale(path string, sizeBox int) error {
	if strings.HasSuffix(path, ".gif") {
		return nil
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return err
	}

	b := img.Bounds()
	if b.Dx() <= sizeBox && b.Dy() <= sizeBox {
		return nil
	}

	w, h := float64(b.Dx()), float64(b.Dy())
	k := math.Max(w, h) / float64(sizeBox)

	canvas := image.NewRGBA(image.Rect(0, 0, int(w/k), int(h/k)))
	for x := 0.0; x < w/k; x++ {
		for y := 0.0; y < h/k; y++ {
			canvas.Set(int(x), int(y), img.At(int(x*k), int(y*k)))
		}
	}

	of, err := os.Create(path + ".thumb.jpg")
	if err != nil {
		return err
	}
	defer of.Close()

	return jpeg.Encode(of, canvas, &jpeg.Options{Quality: 70})
}
