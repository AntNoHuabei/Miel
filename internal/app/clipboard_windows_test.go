//go:build windows

package app

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"testing"
)

func TestDIBToPNGDecodesBottomUpBitmap(t *testing.T) {
	// Two 32-bit BGRA pixels, stored bottom-up in a BITMAPINFOHEADER DIB.
	dib := make([]byte, 40+8)
	binary.LittleEndian.PutUint32(dib[0:4], 40)
	binary.LittleEndian.PutUint32(dib[4:8], 2)
	binary.LittleEndian.PutUint32(dib[8:12], 1)
	binary.LittleEndian.PutUint16(dib[12:14], 1)
	binary.LittleEndian.PutUint16(dib[14:16], 32)
	dib[40], dib[41], dib[42], dib[43] = 0, 0, 255, 255
	dib[44], dib[45], dib[46], dib[47] = 0, 255, 0, 255

	encoded, err := dibToPNG(dib)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := img.At(0, 0).RGBA()
	if r <= g || r <= b {
		t.Fatalf("first pixel = (%d,%d,%d), want red", r, g, b)
	}
	r, g, b, _ = img.At(1, 0).RGBA()
	if g <= r || g <= b {
		t.Fatalf("second pixel = (%d,%d,%d), want green", r, g, b)
	}
}

func TestDIBV5ToPNGDecodesBitmap(t *testing.T) {
	dib := make([]byte, 124+4)
	binary.LittleEndian.PutUint32(dib[0:4], 124)
	binary.LittleEndian.PutUint32(dib[4:8], 1)
	binary.LittleEndian.PutUint32(dib[8:12], 1)
	binary.LittleEndian.PutUint16(dib[12:14], 1)
	binary.LittleEndian.PutUint16(dib[14:16], 32)
	dib[124], dib[125], dib[126], dib[127] = 255, 0, 0, 255

	encoded, err := dibToPNG(dib)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	_, _, blue, _ := img.At(0, 0).RGBA()
	if blue == 0 {
		t.Fatal("DIBV5 pixel was not decoded")
	}
}

func TestDIBToPNGRejectsMalformedData(t *testing.T) {
	for _, dib := range [][]byte{
		make([]byte, 8),
		append([]byte{200, 0, 0, 0}, make([]byte, 36)...),
	} {
		if _, err := dibToPNG(dib); err == nil {
			t.Fatalf("dibToPNG(%d bytes) returned no error", len(dib))
		}
	}
}
