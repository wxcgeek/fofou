package main

import (
	"bytes"
	"image/png"
	"os"
)

func main() {
	f, _ := os.Open("table.png")
	of, _ := os.Create("font.go")

	of.WriteString(`
package captcha

const (
	fontWidth  = 17
	fontHeight = 18
	blackChar  = 1
)

var font = [][][]byte {
`)

	img, _ := png.Decode(f)
	for y := 0; y < 10; y++ {

		of.WriteString(`[][]byte {
`)

		for x := 0; x < 5; x++ {
			p := bytes.Buffer{}
			p.WriteString(`[]byte {
`)

			for y0 := y * 18; y0 < y*18+18; y0++ {
				for x0 := x * 17; x0 < x*17+17; x0++ {
					r, _, _, _ := img.At(x0, y0).RGBA()
					if r == 0 {
						p.WriteString("1,")
					} else {
						p.WriteString("0,")
					}
				}
				p.WriteString("\n")
			}

			p.WriteString(`},
`)
			of.Write(p.Bytes())
		}

		of.WriteString(`},
`)
	}

	of.WriteString(`}`)
	of.Close()
	f.Close()
}
