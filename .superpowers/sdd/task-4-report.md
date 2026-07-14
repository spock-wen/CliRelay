# Task 4 Report: 识图回填编排 Recognizer

## What was implemented

- `internal/vision/recognizer.go` — two exported functions + one helper:
  - `ResolveRecognitionTarget(cfg *config.Config, spec string) (*RecognitionTarget, bool)`
    Parses `"provider-name/model-name"` against `cfg.OpenAICompatibility[]` (case-insensitive match by Name). Returns BaseURL, APIKey (first non-empty entry), and Model. Returns `ok=false` for empty spec, no slash, unknown provider, nil cfg, or no usable API key.
  - `RecognizeCurrentTurnImages(ctx, analyzer, payload, currentModel) RecognizeImagesResult`
    Orchestrates image recognition backfill. Skips if analyzer nil, no current-turn images, or current model is vision-capable. Walks payload, calls `analyzer.Analyze` for each current-turn image, replaces the image part with `"[图片内容] " + RenderSummary(resp.Summary)` on success, or `"[图片识别失败]"` on error. Returns modified payload with `Applied=true` and `FallbackModel=analyzer.Name()`.
  - `recognizeOneImage` — helper that builds `AnalyzeRequest` with `ImageSourceUserUpload` (corrected from the brief's nonexistent `ImageSourceKindInline`).

- `internal/vision/recognizer_test.go` — two test functions with 11 sub-tests total:
  - `TestResolveRecognitionTarget` — 6 sub-tests covering valid spec, unknown provider, no slash, empty spec, nil cfg, no usable API key.
  - `TestRecognizeCurrentTurnImages` — 5 sub-tests covering replacement, vision-model skip, no-image skip, nil-analyzer skip, and analyze-failure degradation.

## TDD evidence

### RED (before implementation)
```
$ go test ./internal/vision/ -run TestResolveRecognitionTarget -v
# github.com/router-for-me/CLIProxyAPI/v6/internal/vision [github.com/router-for-me/CLIProxyAPI/v6/internal/vision.test]
internal\vision\recognizer_test.go:26:14: undefined: ResolveRecognitionTarget
...
FAIL
```

### GREEN (after implementation)
```
$ go test ./internal/vision/ -run "TestRecognizeCurrentTurnImages|TestResolveRecognitionTarget" -v
=== RUN   TestResolveRecognitionTarget
=== RUN   TestResolveRecognitionTarget/valid_spec
=== RUN   TestResolveRecognitionTarget/unknown_provider
=== RUN   TestResolveRecognitionTarget/no_slash
=== RUN   TestResolveRecognitionTarget/empty_spec
=== RUN   TestResolveRecognitionTarget/nil_cfg
=== RUN   TestResolveRecognitionTarget/no_usable_api_key
--- PASS: TestResolveRecognitionTarget (0.00s)
=== RUN   TestRecognizeCurrentTurnImages
=== RUN   TestRecognizeCurrentTurnImages/replaces_image_with_summary
=== RUN   TestRecognizeCurrentTurnImages/skips_when_current_model_is_vision
=== RUN   TestRecognizeCurrentTurnImages/skips_when_no_image
=== RUN   TestRecognizeCurrentTurnImages/nil_analyzer_skips
=== RUN   TestRecognizeCurrentTurnImages/analyze_failure_degrades_to_placeholder
--- PASS: TestRecognizeCurrentTurnImages (0.01s)
PASS
ok      github.com/router-for-me/CLIProxyAPI/v6/internal/vision       0.043s
```

Full vision package test suite (38 tests) also passes with no regressions.

## Files changed

- `internal/vision/recognizer.go` (new)
- `internal/vision/recognizer_test.go` (new)

## Self-review findings

- **Signatures match Task 5 expectations**: `ResolveRecognitionTarget`, `RecognizeCurrentTurnImages`, and `RecognizeImagesResult{Payload, Applied, FallbackModel}` are all exported and correctly typed.
- **`ImageSourceUserUpload` used correctly**: The brief referenced a nonexistent `ImageSourceKindInline`; the controller correction directed using `ImageSourceUserUpload`, which is what the implementation uses.
- **Error path degrades gracefully**: Analyze failures and empty summaries both return `"[图片识别失败]"` without aborting the loop or the whole request.
- **Multi-image replacement concern investigated**: I verified whether replacing multiple images in the same message causes index shifting. `ReplaceImagePartEx` uses sjson to mutate the element at the given index in place (transforming an `image_url` object into a `text` object), preserving array length. Therefore indices do not shift, and the brief's loop approach is safe for multi-image messages. No concern remains.
- **Trailing newlines**: Both new files end with a trailing newline.

## Concerns

None. The multi-image replacement path is safe because `ReplaceImagePartEx` performs an in-place element mutation rather than a delete/insert operation.
