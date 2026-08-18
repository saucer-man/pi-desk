//go:build ignore

// This generator creates the reviewed platform-specific file-manager fallback
// artwork. It intentionally uses no copied OS artwork or trademarked assets.
package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
)

const size = 256

func main() {
	write("file-manager-windows.png", windowsFolder())
	write("file-manager-darwin.png", finderFallback())
	write("file-manager-linux.png", linuxFolder())
}

func write(name string, img image.Image) {
	file, err := os.Create(name)
	if err != nil {
		panic(err)
	}
	defer file.Close()
	if err := png.Encode(file, img); err != nil {
		panic(err)
	}
}

func canvas() *image.NRGBA { return image.NewNRGBA(image.Rect(0, 0, size, size)) }

func windowsFolder() *image.NRGBA {
	img := canvas()
	fillRounded(img, 28, 62, 226, 205, 22, func(_, y int) color.NRGBA {
		return color.NRGBA{R: 255, G: uint8(194 + y/14), B: 54, A: 255}
	})
	fillRounded(img, 42, 48, 124, 102, 16, solid(color.NRGBA{R: 251, G: 190, B: 52, A: 255}))
	fillRounded(img, 34, 94, 232, 211, 18, func(_, y int) color.NRGBA {
		return color.NRGBA{R: 255, G: uint8(211 + (y-94)/8), B: 70, A: 255}
	})
	fillRounded(img, 57, 119, 210, 181, 9, solid(color.NRGBA{R: 87, G: 163, B: 230, A: 255}))
	fillRounded(img, 66, 128, 201, 174, 5, solid(color.NRGBA{R: 235, G: 247, B: 255, A: 230}))
	return img
}

func finderFallback() *image.NRGBA {
	img := canvas()
	fillRounded(img, 28, 28, 228, 228, 40, func(x, y int) color.NRGBA {
		if x < 128 {
			return color.NRGBA{R: 91, G: uint8(190 + y/16), B: 246, A: 255}
		}
		return color.NRGBA{R: 35, G: uint8(126 + y/18), B: 220, A: 255}
	})
	stroke(img, 128, 45, 114, 130, 5, color.NRGBA{R: 12, G: 72, B: 132, A: 255})
	stroke(img, 114, 130, 133, 155, 5, color.NRGBA{R: 12, G: 72, B: 132, A: 255})
	stroke(img, 82, 91, 82, 116, 6, color.NRGBA{R: 9, G: 62, B: 117, A: 255})
	stroke(img, 174, 91, 174, 116, 6, color.NRGBA{R: 245, G: 251, B: 255, A: 255})
	stroke(img, 69, 168, 108, 181, 5, color.NRGBA{R: 9, G: 62, B: 117, A: 255})
	stroke(img, 108, 181, 163, 180, 5, color.NRGBA{R: 9, G: 62, B: 117, A: 255})
	stroke(img, 163, 180, 190, 164, 5, color.NRGBA{R: 9, G: 62, B: 117, A: 255})
	return img
}

func linuxFolder() *image.NRGBA {
	img := canvas()
	fillRounded(img, 29, 61, 225, 207, 23, solid(color.NRGBA{R: 98, G: 65, B: 156, A: 255}))
	fillRounded(img, 43, 48, 124, 104, 16, solid(color.NRGBA{R: 133, G: 94, B: 190, A: 255}))
	fillRounded(img, 35, 94, 231, 211, 19, func(_, y int) color.NRGBA {
		return color.NRGBA{R: uint8(145 + (y-94)/7), G: uint8(101 + (y-94)/9), B: 203, A: 255}
	})
	fillRounded(img, 57, 121, 209, 180, 10, solid(color.NRGBA{R: 229, G: 218, B: 246, A: 238}))
	return img
}

func solid(value color.NRGBA) func(int, int) color.NRGBA {
	return func(int, int) color.NRGBA { return value }
}

func fillRounded(img *image.NRGBA, x0, y0, x1, y1, radius int, paint func(int, int) color.NRGBA) {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			cx := max(x0+radius, min(x, x1-radius-1))
			cy := max(y0+radius, min(y, y1-radius-1))
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= radius*radius {
				img.SetNRGBA(x, y, paint(x, y))
			}
		}
	}
}

func stroke(img *image.NRGBA, x0, y0, x1, y1, width int, value color.NRGBA) {
	dx, dy := x1-x0, y1-y0
	steps := max(abs(dx), abs(dy))
	for step := 0; step <= steps; step++ {
		x := x0 + dx*step/max(1, steps)
		y := y0 + dy*step/max(1, steps)
		fillRounded(img, x-width, y-width, x+width+1, y+width+1, width, solid(value))
	}
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
