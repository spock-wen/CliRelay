package vision

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

// makeTestJPEG is also used by recognizer_test.go and load_test.go (same package).
func makeTestJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 128, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

func base64Of(t *testing.T, raw []byte) string {
	t.Helper()
	return base64.StdEncoding.EncodeToString(raw)
}

func decodeJPEGDims(t *testing.T, b64 string) (int, int) {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("decode base64: %v", err)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode config: %v", err)
	}
	return cfg.Width, cfg.Height
}

func TestPreprocessImageDownscalesOversized(t *testing.T) {
	cfg := DefaultPreprocessConfig()
	cfg.StandardMaxDim = 64
	out, err := PreprocessImage(makeTestJPEG(t, 1024, 512), PreprocessModeStandard, cfg)
	if err != nil {
		t.Fatalf("PreprocessImage: %v", err)
	}
	w, h := decodeJPEGDims(t, out.Base64)
	if w > 64 || h > 64 {
		t.Fatalf("dimensions %dx%d not downscaled to <=64", w, h)
	}
	if out.MediaType != "image/jpeg" {
		t.Fatalf("media type = %q, want image/jpeg", out.MediaType)
	}
	if !out.Downsized {
		t.Fatal("expected Downsized=true")
	}
	if out.Bytes <= 0 {
		t.Fatal("bytes <= 0")
	}
}

func TestPreprocessImageSmallKeptAsIs(t *testing.T) {
	cfg := DefaultPreprocessConfig()
	cfg.StandardMaxDim = 2048
	out, err := PreprocessImage(makeTestJPEG(t, 100, 80), PreprocessModeStandard, cfg)
	if err != nil {
		t.Fatalf("PreprocessImage: %v", err)
	}
	w, h := decodeJPEGDims(t, out.Base64)
	if w != 100 || h != 80 {
		t.Fatalf("small image resized to %dx%d, want 100x80", w, h)
	}
	if out.Downsized {
		t.Fatal("expected Downsized=false for small image")
	}
}

func TestPreprocessImageRejectsOversizedBytes(t *testing.T) {
	cfg := DefaultPreprocessConfig()
	cfg.MaxSizeBytes = 100
	_, err := PreprocessImage(makeTestJPEG(t, 200, 200), PreprocessModeStandard, cfg)
	if err != ErrImageTooLarge {
		t.Fatalf("err = %v, want ErrImageTooLarge", err)
	}
}

func TestPreprocessImageRejectsGarbage(t *testing.T) {
	_, err := PreprocessImage([]byte("not an image"), PreprocessModeStandard, DefaultPreprocessConfig())
	if err != ErrUnsupportedFormat {
		t.Fatalf("err = %v, want ErrUnsupportedFormat", err)
	}
}

func TestPreprocessImageOCRUsesLargerDim(t *testing.T) {
	cfg := DefaultPreprocessConfig()
	cfg.StandardMaxDim = 64
	cfg.OCRMaxDim = 256
	out, err := PreprocessImage(makeTestJPEG(t, 512, 256), PreprocessModeOCR, cfg)
	if err != nil {
		t.Fatalf("PreprocessImage: %v", err)
	}
	w, h := decodeJPEGDims(t, out.Base64)
	if w > 256 || h > 256 {
		t.Fatalf("OCR mode dims %dx%d exceed 256", w, h)
	}
}
