package webassets

import (
	"bytes"
	"encoding/binary"
	"image"
	_ "image/png"
	"testing"
)

func TestWP826ThemeAssetsEmbedded(t *testing.T) {
	t.Parallel()

	wallpapers := []string{
		"static/wp826/wallpapers/ringring-memphis-day.png",
		"static/wp826/wallpapers/ringring-memphis-twilight.png",
		"static/wp826/wallpapers/ringring-memphis-party.png",
	}
	for _, path := range wallpapers {
		path := path
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			content, err := Files.ReadFile(path)
			if err != nil {
				t.Fatalf("read embedded wallpaper: %v", err)
			}
			config, _, err := image.DecodeConfig(bytes.NewReader(content))
			if err != nil {
				t.Fatalf("decode embedded wallpaper: %v", err)
			}
			if config.Width != 240 || config.Height != 320 {
				t.Fatalf("wallpaper dimensions = %dx%d, want 240x320", config.Width, config.Height)
			}
			if len(content) > 500*1024 {
				t.Fatalf("wallpaper size = %d bytes, want at most 500 KiB", len(content))
			}
		})
	}

	for slot := byte('1'); slot <= '4'; slot++ {
		path := "static/wp826/ringtones/ring" + string(slot) + ".bin"
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			content, err := Files.ReadFile(path)
			if err != nil {
				t.Fatalf("read embedded ringtone: %v", err)
			}
			if len(content) <= 512 || len(content) > 65_536 || len(content)%2 != 0 {
				t.Fatalf("ringtone size = %d bytes, want an even size in (512, 65536]", len(content))
			}
			if got := int(binary.BigEndian.Uint32(content[:4])) * 2; got != len(content) {
				t.Fatalf("ringtone header size = %d bytes, file size = %d", got, len(content))
			}
			if got := binary.BigEndian.Uint32(content[6:10]); got != 0x01000000 {
				t.Fatalf("ringtone format version = %#x", got)
			}
			if !bytes.Equal(bytes.TrimRight(content[16:32], "\x00"), []byte("ring.bin")) {
				t.Fatalf("ringtone header filename is invalid")
			}
			if got := binary.BigEndian.Uint16(content[32:34]); got != 0 {
				t.Fatalf("ringtone codec = %d, want G.711 mu-law codec 0", got)
			}
			var checksum uint32
			for offset := 0; offset < len(content); offset += 2 {
				checksum += uint32(binary.BigEndian.Uint16(content[offset : offset+2]))
			}
			if checksum&0xffff != 0 {
				t.Fatalf("ringtone checksum = %#x, want zero", checksum&0xffff)
			}
		})
	}
}
