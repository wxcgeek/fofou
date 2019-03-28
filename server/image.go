package server

import (
	"image"
	"image/gif"
	"image/jpeg"
	_ "image/png"
	"math"
	"os"
	"strings"
	"time"
)

type ImageQueue struct {
	q       chan string
	sizeBox int
	*Logger
}

func NewImageQueue(l *Logger, sizeBox int, workers int) *ImageQueue {
	iq := &ImageQueue{
		q:       make(chan string, 256),
		Logger:  l,
		sizeBox: sizeBox,
	}

	for i := 0; i < workers; i++ {
		go iq.job()
	}
	return iq
}

func (iq *ImageQueue) Len() int {
	return len(iq.q)
}

func (iq *ImageQueue) Push(path string) {
	select {
	case iq.q <- path:
	default:
	}
}

func (iq *ImageQueue) job() {
	for {
		select {
		case path := <-iq.q:
			err := naiveDownscale(path, iq.sizeBox)
			if err != nil {
				iq.Error("downscale %s: %v", path, err)
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func naiveDownscale(path string, sizeBox int) error {
	if _, err := os.Stat(path + ".thumb.jpg"); err == nil {
		return nil
	}

	if _, err := os.Stat(path); err != nil {
		return nil
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	var img image.Image
	if strings.HasSuffix(path, ".gif") {
		img, err = gif.Decode(f)
	} else {
		img, _, err = image.Decode(f)
	}
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
