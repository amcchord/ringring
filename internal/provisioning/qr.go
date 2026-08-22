package provisioning

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"

	qrcode "github.com/yeqown/go-qrcode/v2"
)

const (
	qrQuietModules = 4
	qrModulePixels = 6
)

type pngQRWriter struct {
	buffer bytes.Buffer
}

func (w *pngQRWriter) Write(matrix qrcode.Matrix) error {
	bitmap := matrix.Bitmap()
	if len(bitmap) == 0 || len(bitmap) != len(bitmap[0]) {
		return errors.New("invalid QR matrix")
	}
	side := (len(bitmap) + 2*qrQuietModules) * qrModulePixels
	canvas := image.NewGray(image.Rect(0, 0, side, side))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	for y, row := range bitmap {
		if len(row) != len(bitmap) {
			return errors.New("invalid QR matrix")
		}
		for x, dark := range row {
			if !dark {
				continue
			}
			left := (x + qrQuietModules) * qrModulePixels
			top := (y + qrQuietModules) * qrModulePixels
			draw.Draw(canvas, image.Rect(left, top, left+qrModulePixels, top+qrModulePixels), &image.Uniform{C: color.Black}, image.Point{}, draw.Src)
		}
	}
	return png.Encode(&w.buffer, canvas)
}

func (w *pngQRWriter) Close() error { return nil }

// QRCodeDataURI returns a self-contained PNG with a four-module quiet zone. It
// deliberately has no network writer or external rendering service.
func QRCodeDataURI(value string) (string, error) {
	if value == "" || len(value) > 2048 {
		return "", errors.New("invalid QR value")
	}
	code, err := qrcode.NewWith(value, qrcode.WithErrorCorrectionLevel(qrcode.ErrorCorrectionMedium))
	if err != nil {
		return "", err
	}
	writer := &pngQRWriter{}
	if err := code.Save(writer); err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(writer.buffer.Bytes()), nil
}
