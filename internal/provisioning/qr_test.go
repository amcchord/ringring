package provisioning

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"strings"
	"testing"
)

func TestQRCodeDataURIIsSquarePNGWithQuietZone(t *testing.T) {
	encoded, err := QRCodeDataURI("https://ringring.live/provision/linphone/example-token")
	if err != nil {
		t.Fatal(err)
	}
	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(encoded, prefix) {
		t.Fatalf("unexpected data URI prefix: %q", encoded[:min(len(encoded), 32)])
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encoded, prefix))
	if err != nil {
		t.Fatal(err)
	}
	image, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if image.Bounds().Dx() != image.Bounds().Dy() || image.Bounds().Dx() < 200 {
		t.Fatalf("unexpected QR bounds: %v", image.Bounds())
	}
	for _, point := range [][2]int{{0, 0}, {image.Bounds().Max.X - 1, 0}, {0, image.Bounds().Max.Y - 1}, {image.Bounds().Max.X - 1, image.Bounds().Max.Y - 1}} {
		r, g, b, _ := image.At(point[0], point[1]).RGBA()
		if r != 0xffff || g != 0xffff || b != 0xffff {
			t.Fatalf("quiet-zone corner %v was not white", point)
		}
	}
}

func TestQRCodeDataURIRejectsEmptyAndOversizedValues(t *testing.T) {
	if _, err := QRCodeDataURI(""); err == nil {
		t.Fatal("empty QR value was accepted")
	}
	if _, err := QRCodeDataURI(strings.Repeat("x", 2049)); err == nil {
		t.Fatal("oversized QR value was accepted")
	}
}
