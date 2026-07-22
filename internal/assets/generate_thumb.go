//go:build ignore

package main

import (
	"image"
	"image/color"
	"image/jpeg"
	"os"
)

func main() {
	width, height := 200, 200
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	
	// Fill with blue color
	blue := color.RGBA{74, 144, 226, 255}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, blue)
		}
	}
	
	f, err := os.Create("default_thumb.jpg")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	
	jpeg.Encode(f, img, &jpeg.Options{Quality: 90})
}
