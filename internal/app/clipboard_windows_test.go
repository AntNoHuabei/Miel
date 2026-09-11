//go:build windows

package app

import (
	"bytes"
	"encoding/binary"
	"errors"
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

func TestReadClipboardWithRetryRecoversFromDelayedData(t *testing.T) {
	attempts := 0
	payload, err := readClipboardWithRetry(func() (clipboardPayload, error) {
		attempts++
		if attempts < 3 {
			return clipboardPayload{}, errClipboardDataUnavailable
		}
		return clipboardPayload{kind: "clipboard_text", text: "ready"}, nil
	}, 3, 0)
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 3 || payload.text != "ready" {
		t.Fatalf("attempts = %d, payload = %#v", attempts, payload)
	}
}

func TestReadClipboardWithRetryDoesNotRepeatPermanentError(t *testing.T) {
	attempts := 0
	want := errors.New("permanent")
	_, err := readClipboardWithRetry(func() (clipboardPayload, error) {
		attempts++
		return clipboardPayload{}, want
	}, 3, 0)
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}
