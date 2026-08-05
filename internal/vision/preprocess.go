package vision

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/jpeg"
	// Imported so the stdlib PNG decoder registers with image.Decode /
	// image.DecodeConfig regardless of what the importing binary links in.
	_ "image/png"

	"golang.org/x/image/bmp"
	"golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

var (
	ErrImageTooLarge     = errors.New("image too large")
	ErrUnsupportedFormat = errors.New("unsupported image format")
)

type PreprocessMode string

const (
	PreprocessModeStandard PreprocessMode = "standard"
	PreprocessModeOCR      PreprocessMode = "ocr"
	PreprocessModeDiff     PreprocessMode = "diff"
)

type PreprocessConfig struct {
	MaxSizeBytes   int
	StandardMaxDim int
	OCRMaxDim      int
	DiffMaxDim     int
	JPEGQuality    int
	// MaxPixelCount caps width*height accepted for full decode, bounding
	// decompression-bomb memory (a tiny highly-compressed file can claim huge
	// dimensions). <= 0 falls back to the default.
	MaxPixelCount int
}

func DefaultPreprocessConfig() PreprocessConfig {
	return PreprocessConfig{
		MaxSizeBytes:   10 * 1024 * 1024,
		StandardMaxDim: 2048,
		OCRMaxDim:      4096,
		DiffMaxDim:     2048,
		JPEGQuality:    80,
		// 16M pixels bounds a single full decode to ~64MB RGBA (comfortably
		// covers a 4K screenshot's 8.3M) while keeping a crafted-dimension
		// decompression bomb from claiming ~400MB per request.
		MaxPixelCount: 16_000_000,
	}
}

type ProcessedImage struct {
	Base64    string
	MediaType string
	Width     int
	Height    int
	Bytes     int
	Downsized bool
}

func PreprocessImage(data []byte, mode PreprocessMode, cfg PreprocessConfig) (*ProcessedImage, error) {
	if len(data) > cfg.MaxSizeBytes {
		return nil, ErrImageTooLarge
	}
	maxPixels := cfg.MaxPixelCount
	if maxPixels <= 0 {
		maxPixels = DefaultPreprocessConfig().MaxPixelCount
	}
	// Read only the header (cheap) to bound decode memory BEFORE full decode:
	// a small highly-compressed image can inflate to gigabytes of RGBA on the
	// request hot path, so reject absurd dimensions early (decompression bomb).
	w, h, err := decodeImageConfig(data)
	if err != nil {
		return nil, ErrUnsupportedFormat
	}
	if w > 0 && h > 0 && int64(w)*int64(h) > int64(maxPixels) {
		return nil, ErrImageTooLarge
	}
	src, err := decodeImage(data)
	if err != nil {
		return nil, ErrUnsupportedFormat
	}

	maxDim := cfg.StandardMaxDim
	if mode == PreprocessModeOCR {
		maxDim = cfg.OCRMaxDim
	} else if mode == PreprocessModeDiff {
		maxDim = cfg.DiffMaxDim
	}

	bounds := src.Bounds()
	w, h = bounds.Dx(), bounds.Dy()
	downsized := false
	if w > maxDim || h > maxDim {
		scale := float64(maxDim) / float64(w)
		if float64(h)*scale > float64(maxDim) {
			scale = float64(maxDim) / float64(h)
		}
		nw := int(float64(w) * scale)
		nh := int(float64(h) * scale)
		if nw < 1 {
			nw = 1
		}
		if nh < 1 {
			nh = 1
		}
		dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
		draw.CatmullRom.Scale(dst, dst.Bounds(), src, bounds, draw.Over, nil)
		src = dst
		w, h = nw, nh
		downsized = true
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: cfg.JPEGQuality}); err != nil {
		return nil, err
	}
	return &ProcessedImage{
		Base64:    base64.StdEncoding.EncodeToString(buf.Bytes()),
		MediaType: "image/jpeg",
		Width:     w,
		Height:    h,
		Bytes:     buf.Len(),
		Downsized: downsized,
	}, nil
}

// decodeImageConfig reads only the image header (dimensions) without decoding
// the full pixel data, mirroring decodeImage's format detection for webp/bmp.
func decodeImageConfig(data []byte) (width, height int, err error) {
	if len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")) {
		cfg, err := webp.DecodeConfig(bytes.NewReader(data))
		return cfg.Width, cfg.Height, err
	}
	if len(data) >= 2 && data[0] == 'B' && data[1] == 'M' {
		cfg, err := bmp.DecodeConfig(bytes.NewReader(data))
		return cfg.Width, cfg.Height, err
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	return cfg.Width, cfg.Height, err
}

func decodeImage(data []byte) (image.Image, error) {
	if len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")) {
		return webp.Decode(bytes.NewReader(data))
	}
	if len(data) >= 2 && data[0] == 'B' && data[1] == 'M' {
		return bmp.Decode(bytes.NewReader(data))
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}
