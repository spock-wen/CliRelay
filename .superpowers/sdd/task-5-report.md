# Task 5 Report: OpenAI 兼容 executor 接入 vision 识图回填

## What was implemented

1. Added `resolveRecognitionAnalyzer()` and `recognizeCurrentTurnImages()` helper methods to `OpenAICompatExecutor` in `internal/runtime/executor/openai_compat_executor.go`.
2. Inserted the vision recognition block into both `Execute` and `ExecuteStream` methods, after the existing `cline` fallback block and before `to := sdktranslator.FromString("openai")`.
3. Added import for `github.com/router-for-me/CLIProxyAPI/v6/internal/vision`.
4. Created `internal/runtime/executor/openai_compat_vision_test.go` with two tests.
5. Fixed a Go typed-nil interface gotcha in `recognizeCurrentTurnImages` to prevent panics when no vision recognition is configured.

## What was tested + results

### TDD Evidence

**RED (Step 2):**
```
FAIL	github.com/router-for-me/CLIProxyAPI/v6/internal/runtime/executor [build failed]
openai_compat_vision_test.go:39:9: e.resolveRecognitionAnalyzer undefined
```

**GREEN (Step 4):**
```
=== RUN   TestResolveRecognitionTargetFromConfig
--- PASS: TestResolveRecognitionTargetFromConfig (0.00s)
PASS
```

**GREEN (Step 7):**
All executor tests PASS, including the new `TestExecuteVisionRecognitionEndToEnd`.

**GREEN (Step 10 - Full regression):**
```
ok  github.com/router-for-me/CLIProxyAPI/v6/internal/vision      0.060s
ok  github.com/router-for-me/CLIProxyAPI/v6/internal/config        0.045s
ok  github.com/router-for-me/CLIProxyAPI/v6/internal/runtime/executor  0.708s
```

### E2E Test Path

**Path taken:** Full `e.Execute(...)` end-to-end test.

The `TestExecuteVisionRecognitionEndToEnd` test passed reliably on the first run. It verifies:
- A mock vision endpoint returns a summary.
- A mock upstream text model receives the payload.
- The upstream body contains the summary text (`"a red box"`).
- The upstream body does NOT contain `image_url`.
- The response is non-empty.

No flakiness was observed; the translator layer did not interfere with the assertions in this test setup.

## Files changed

- `internal/runtime/executor/openai_compat_executor.go`
  - Added `vision` import.
  - Added `resolveRecognitionAnalyzer()` method.
  - Added `recognizeCurrentTurnImages()` method with typed-nil safety.
  - Inserted recognition block in `Execute` (after cline block, before `sdktranslator.FromString`).
  - Inserted recognition block in `ExecuteStream` (after cline block, before `sdktranslator.FromString`).
- `internal/runtime/executor/openai_compat_vision_test.go` (new)
  - `TestResolveRecognitionTargetFromConfig`
  - `TestExecuteVisionRecognitionEndToEnd`

## Self-review findings

- Recognition block inserted into BOTH `Execute` and `ExecuteStream` in the correct location (after cline block, before `to := sdktranslator.FromString("openai")`).
- `contextWithVisionFallbackLog` called when `recog.Applied`.
- `req.Payload = recog.Payload` applied correctly.
- Compiles cleanly, no unused imports.
- All tests pass, output pristine.
- Trailing newline present on new test file.
- The recognition block is reached before translation/upstream call, so it is live code.
- **Typed-nil safety:** Added an explicit `if analyzer == nil` guard in `recognizeCurrentTurnImages` before passing to `vision.RecognizeCurrentTurnImages` to avoid a Go interface nil gotcha that caused a panic in existing tests when `VisionRecognitionModel` was not configured.

## Concerns

None. The implementation is clean, all tests pass, and the typed-nil safety guard prevents regressions in configurations without vision recognition enabled.

---

## Post-review fix (amended to a9c0ed52)

### Changes applied

**Finding 2 — Fix fragile body read in e2e test**
- File: `internal/runtime/executor/openai_compat_vision_test.go`, lines 57-59
- Before:
  ```go
  buf := make([]byte, 1<<16)
  n, _ := r.Body.Read(buf)
  upstreamBody = string(buf[:n])
  ```
- After:
  ```go
  b, _ := io.ReadAll(r.Body)
  upstreamBody = string(b)
  ```
- Added `"io"` to imports.

**Finding 3 — Remove unused `visionMockServer` helper**
- File: `internal/runtime/executor/openai_compat_vision_test.go`, lines 16-23
- Removed the unused `visionMockServer` function entirely.

**Finding 1 — Remove redundant typed-nil guard (NOT APPLIED)**
- File: `internal/runtime/executor/openai_compat_executor.go`, lines 548-552
- The guard `if analyzer == nil { return vision.RecognizeImagesResult{Payload: payload} }` was **kept**.
- Reason: After removal, `go test ./internal/runtime/executor/ -run "Vision"` panics with a nil-pointer dereference in `vision.(*OpenCodeGoAnalyzer).Analyze` (analyzer.go:82). This occurs because `resolveRecognitionAnalyzer()` returns `*vision.OpenCodeGoAnalyzer` (concrete pointer). When nil, it is passed to `vision.RecognizeCurrentTurnImages` which accepts `ImageAnalyzer` (interface). In Go, a nil concrete pointer wrapped in an interface is non-nil, so the `if analyzer == nil` check inside `RecognizeCurrentTurnImages` passes as false, and the subsequent method call panics on nil receiver. The typed-nil interface trap exists here; the reviewer's claim that it does not was incorrect.

### Test results

```
=== RUN   TestExecutionContextReporterKeepsVisionFallbackSeparateFromModelMapping
--- PASS: TestExecutionContextReporterKeepsVisionFallbackSeparateFromModelMapping (0.00s)
=== RUN   TestOpenAICompatExecutorClineUsesVisionFallbackForImageRequests
--- PASS: TestOpenAICompatExecutorClineUsesVisionFallbackForImageRequests (0.00s)
=== RUN   TestExecuteVisionRecognitionEndToEnd
--- PASS: TestExecuteVisionRecognitionEndToEnd (0.00s)
=== RUN   TestOpenCodeGoExecutorUsesVisionFallbackForImageRequests
--- PASS: TestOpenCodeGoExecutorUsesVisionFallbackForImageRequests (0.00s)
=== RUN   TestOpenCodeGoExecutorUsesVisionFallbackForResponsesFunctionCallOutputImage
--- PASS: TestOpenCodeGoExecutorUsesVisionFallbackForResponsesFunctionCallOutputImage (0.00s)
=== RUN   TestOpenCodeGoExecutorUsesConfiguredNonQwenVisionFallback
--- PASS: TestOpenCodeGoExecutorUsesConfiguredNonQwenVisionFallback (0.00s)
=== RUN   TestOpenCodeGoExecutorStreamRewritesVisionFallbackModel
--- PASS: TestOpenCodeGoExecutorStreamRewritesVisionFallbackModel (0.00s)
=== RUN   TestOpenCodeGoExecutorIgnoresExcludedVisionFallback
--- PASS: TestOpenCodeGoExecutorIgnoresExcludedVisionFallback (0.00s)
PASS
ok      github.com/router-for-me/CLIProxyAPI/v6/internal/runtime/executor      0.057s
```

Build: `go build ./...` — clean (no output).

### New HEAD SHA

`7854e99a`
