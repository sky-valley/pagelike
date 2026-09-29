package hosting

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func TestUploadedPhotoIsDecodedBoundedAndMetadataFree(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 3))
	img.Set(1, 1, color.RGBA{R: 255, A: 255})
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, img, nil); err != nil {
		t.Fatal(err)
	}
	// A syntactically valid JPEG APP1 segment can carry GPS/EXIF data. None of
	// its bytes may survive the decode/re-encode path that actually stores it.
	metadata := []byte("Exif\x00\x00GPS-test-location")
	segment := append([]byte{0xff, 0xe1, byte((len(metadata) + 2) >> 8), byte(len(metadata) + 2)}, metadata...)
	photo := append([]byte{0xff, 0xd8}, segment...)
	photo = append(photo, encoded.Bytes()[2:]...)
	cleaned, err := contributionBody("/uploads/photo.jpg", "image/jpeg", photo)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cleaned, metadata) {
		t.Fatal("image retained uploaded metadata")
	}
	cfg, kind, err := image.DecodeConfig(bytes.NewReader(cleaned))
	if err != nil || kind != "jpeg" || cfg.Width != 4 || cfg.Height != 3 {
		t.Fatal("cleaned photo no longer decodes")
	}
	for _, c := range []struct {
		path, ct string
		data     []byte
	}{{"/uploads/image.svg", "image/svg+xml", []byte(`<svg onload="bad"/>`)}, {"/uploads/image.jpg", "image/jpeg", []byte("not an image")}, {"/data/code.js", "text/javascript", []byte("bad()")}} {
		if _, err := contributionBody(c.path, c.ct, c.data); err == nil {
			t.Fatalf("accepted %s", c.path)
		}
	}
}
