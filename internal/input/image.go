package input

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"

	_ "image/gif"
	_ "image/png"

	_ "golang.org/x/image/webp"
)

const normalizedJPEGQuality = 90

// NormalizeImageToJPEG decodes supported image formats and re-encodes them as JPEG.
func NormalizeImageToJPEG(data []byte) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	bounds := img.Bounds()
	flattened := image.NewRGBA(bounds)
	draw.Draw(flattened, bounds, &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(flattened, bounds, img, bounds.Min, draw.Over)

	var out bytes.Buffer
	if err := jpeg.Encode(&out, flattened, &jpeg.Options{Quality: normalizedJPEGQuality}); err != nil {
		return nil, fmt.Errorf("encode jpeg: %w", err)
	}
	return out.Bytes(), nil
}
