package vision

// User-visible placeholder strings for image recognition.
// Centralised here so Chinese text is not scattered across logic files.
const (
	// PlaceholderImageContent is the prefix for a successfully recognized image.
	PlaceholderImageContent = "[图片内容] "

	// PlaceholderImageRecognitionFailed is used when the analyzer returns an error or empty result.
	PlaceholderImageRecognitionFailed = "[图片识别失败]"

	// PlaceholderHistoricalImageUnrecognizable is used for historical images
	// that have no cached analysis and no inline data to re-analyze.
	PlaceholderHistoricalImageUnrecognizable = "[图片内容（历史图片，无法识别）]"
)
