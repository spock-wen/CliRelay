# 服务器端图像外部化 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 CliRelay 服务器端完整接管图像识别——图片字节不进对话模型上下文，只进服务器识别器；文本模型不 400、视觉模型不被大 base64 撑爆；支持 100 并发。

**Architecture:** 在 `ClaudeExecutor` 转发前拦截含图请求：`PreprocessImage`（Go 复刻 MCP sharp 缩放）→ 识别器（讯飞199 渠道 kimi-k2.6，结构化摘要）→ 用摘要文本替换图片块 → 只转发文本给对话模型。会话注册表用复合键 `auth.ID + "::" + 会话ID` 隔离，杜绝跨用户串号。

**Tech Stack:** Go 1.26、`golang.org/x/image`（新增）、`golang.org/x/sync/semaphore`（已有）、`tidwall/gjson`/`sjson`（已有）、httptest（测试）。

**Spec:** `docs/superpowers/specs/2026-08-04-server-vision-externalization-design.md`

## Global Constraints

- 所有 go 命令必须带代理：`GOPROXY=https://goproxy.cn,direct GOSUMDB=sum.golang.google.cn`（Google 端点被墙，工具链 go1.26.5 已缓存）
- 会话隔离：SessionKey 必须用复合键 `auth.ID + "::" + 客户端会话ID`；`auth` 为 nil / `auth.ID` 为空 → 返回空 key（单次请求不跨轮）
- **不动现有 opencode 路径**：只新增 `ResolveIsolatedSessionKey`，`ResolveSessionKey` 原样保留
- 图片块**永不**转发给对话模型——只转发摘要文本
- 识别超时先定 5s（`analyze_timeout_ms`），模型与超时都必须在 config.yml 可配
- 依赖：新增仅限 `golang.org/x/image`（禁止其它新依赖）
- 复用现有 `internal/vision` 包结构（`processor.go` / `registry.go` / `walk.go` / `session.go` / `types.go` / `analyzer.go`），遵循其命名与模式
- 提交信息用语义化：`feat:` / `test:` / `chore:`（依赖）

---

### Task 1: PreprocessImage —— 服务器端图片预处理

**Files:**
- Create: `internal/vision/preprocess.go`
- Test: `internal/vision/preprocess_test.go`
- Modify: `go.mod`（新增 `golang.org/x/image`）

**Interfaces:**
- Produces:
  - `type PreprocessMode string`；常量 `PreprocessModeStandard`、`PreprocessModeOCR`、`PreprocessModeDiff`
  - `type PreprocessConfig struct { MaxSizeBytes int; StandardMaxDim int; OCRMaxDim int; DiffMaxDim int; JPEGQuality int }`
  - `func DefaultPreprocessConfig() PreprocessConfig`
  - `type ProcessedImage struct { Base64 string; MediaType string; Width int; Height int; Bytes int; Downsized bool }`
  - `func PreprocessImage(data []byte, mode PreprocessMode, cfg PreprocessConfig) (*ProcessedImage, error)`
  - `var ErrImageTooLarge = errors.New("image too large")`、`var ErrUnsupportedFormat = errors.New("unsupported image format")`

- [ ] **Step 1: 新增依赖**

```bash
export GOPROXY=https://goproxy.cn,direct GOSUMDB=sum.golang.google.cn
go get golang.org/x/image@latest
```

- [ ] **Step 2: 写失败测试**

`internal/vision/preprocess_test.go`：

```go
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
```

- [ ] **Step 3: 运行确认失败**

Run: `GOPROXY=https://goproxy.cn,direct GOSUMDB=sum.golang.google.cn go test ./internal/vision/ -run TestPreprocessImage -v`
Expected: FAIL（`preprocess.go` 不存在 / 函数未定义）

- [ ] **Step 4: 实现**

`internal/vision/preprocess.go`：

```go
package vision

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/jpeg"

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
}

func DefaultPreprocessConfig() PreprocessConfig {
	return PreprocessConfig{
		MaxSizeBytes:   10 * 1024 * 1024,
		StandardMaxDim: 2048,
		OCRMaxDim:      4096,
		DiffMaxDim:     2048,
		JPEGQuality:    80,
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
	w, h := bounds.Dx(), bounds.Dy()
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

func decodeImage(data []byte) (image.Image, error) {
	if len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")) {
		return webp.Decode(bytes.NewReader(data))
	}
	if len(data) >= 2 && data[0] == 'B' && data[1] == 'M' {
		return bmp.Decode(bytes.NewReader(data))
	}
	return image.Decode(bytes.NewReader(data))
}
```

- [ ] **Step 5: 运行确认通过**

Run: `GOPROXY=https://goproxy.cn,direct GOSUMDB=sum.golang.google.cn go test ./internal/vision/ -run TestPreprocessImage -v`
Expected: PASS（5 个用例）

- [ ] **Step 6: 提交**

```bash
git add go.mod go.sum internal/vision/preprocess.go internal/vision/preprocess_test.go
git commit -m "feat: add server-side image preprocessing (PreprocessImage)"
```

---

### Task 2: 并发限流器 + 密钥池

**Files:**
- Create: `internal/vision/limiter.go`
- Test: `internal/vision/limiter_test.go`

**Interfaces:**
- Consumes: 无（独立）
- Produces:
  - `type ConcurrencyLimiter struct{ ... }`；`func NewConcurrencyLimiter(max int) *ConcurrencyLimiter`；`func (l *ConcurrencyLimiter) Run(ctx context.Context, fn func() error) error`（并发满时等待，ctx 取消则返回 ctx.Err()）
  - `type KeyPool struct{ ... }`；`func NewKeyPool(keys []string, perKeyConcurrency int, cooldown time.Duration) *KeyPool`；`func (p *KeyPool) Acquire() (key string, release func(), ok bool)`；`func (p *KeyPool) MarkUnavailable(key string)`

- [ ] **Step 1: 写失败测试**

`internal/vision/limiter_test.go`：

```go
package vision

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConcurrencyLimiterCapsInFlight(t *testing.T) {
	lim := NewConcurrencyLimiter(3)
	var maxInFlight atomic.Int32
	var cur atomic.Int32
	var wg sync.WaitGroup
	ctx := context.Background()
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = lim.Run(ctx, func() error {
				v := cur.Add(1)
				for {
					m := maxInFlight.Load()
					if v <= m || maxInFlight.CompareAndSwap(m, v) {
						break
					}
				}
				time.Sleep(5 * time.Millisecond)
				cur.Add(-1)
				return nil
			})
		}()
	}
	wg.Wait()
	if got := maxInFlight.Load(); got > 3 {
		t.Fatalf("max in-flight = %d, want <= 3", got)
	}
}

func TestConcurrencyLimiterContextCancel(t *testing.T) {
	lim := NewConcurrencyLimiter(1)
	blocked := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_ = lim.Run(context.Background(), func() error {
			close(blocked)
			<-release
			return nil
		})
	}()
	<-blocked
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := lim.Run(ctx, func() error { return nil })
	if err == nil {
		t.Fatal("expected context deadline exceeded, got nil")
	}
	close(release)
}

func TestKeyPoolRoundRobinAndConcurrency(t *testing.T) {
	p := NewKeyPool([]string{"k1", "k2"}, 2, time.Minute)
	var got []string
	for i := 0; i < 4; i++ {
		k, release, ok := p.Acquire()
		if !ok {
			t.Fatalf("acquire %d failed", i)
		}
		got = append(got, k)
		release()
	}
	if len(got) != 4 {
		t.Fatalf("acquired %d, want 4", len(got))
	}
	rel1, _, _ := p.Acquire()
	rel2, _, _ := p.Acquire()
	if k, _, ok := p.Acquire(); ok {
		t.Fatalf("expected no key available, got %q", k)
	}
	rel1()
	rel2()
}

func TestKeyPoolMarkUnavailable(t *testing.T) {
	p := NewKeyPool([]string{"k1"}, 2, time.Hour)
	p.MarkUnavailable("k1")
	if _, _, ok := p.Acquire(); ok {
		t.Fatal("expected acquire to fail after mark unavailable")
	}
}

func TestKeyPoolCooldownExpires(t *testing.T) {
	p := NewKeyPool([]string{"k1"}, 2, 10*time.Millisecond)
	p.MarkUnavailable("k1")
	time.Sleep(15 * time.Millisecond)
	if _, _, ok := p.Acquire(); !ok {
		t.Fatal("expected acquire to succeed after cooldown expired")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `GOPROXY=https://goproxy.cn,direct GOSUMDB=sum.golang.google.cn go test ./internal/vision/ -run TestConcurrency -v`
Expected: FAIL（`limiter.go` 不存在）

- [ ] **Step 3: 实现**

`internal/vision/limiter.go`：

```go
package vision

import (
	"context"
	"sync"
	"time"

	"golang.org/x/sync/semaphore"
)

// ConcurrencyLimiter is a bounded semaphore with context-aware acquire.
type ConcurrencyLimiter struct {
	sem *semaphore.Weighted
}

func NewConcurrencyLimiter(max int) *ConcurrencyLimiter {
	if max <= 0 {
		max = 1
	}
	return &ConcurrencyLimiter{sem: semaphore.NewWeighted(int64(max))}
}

func (l *ConcurrencyLimiter) Run(ctx context.Context, fn func() error) error {
	if err := l.sem.Acquire(ctx, 1); err != nil {
		return err
	}
	defer l.sem.Release(1)
	return fn()
}

// KeyPool round-robins across API keys, capping per-key concurrency and
// cooling down keys that hit errors/429s.
type KeyPool struct {
	mu            sync.Mutex
	keys          []string
	inFlight      []int
	cooldownUntil []time.Time
	perKey        int
	cooldown      time.Duration
	cursor        int
}

func NewKeyPool(keys []string, perKeyConcurrency int, cooldown time.Duration) *KeyPool {
	if perKeyConcurrency <= 0 {
		perKeyConcurrency = 1
	}
	return &KeyPool{
		keys:          append([]string(nil), keys...),
		inFlight:      make([]int, len(keys)),
		cooldownUntil: make([]time.Time, len(keys)),
		perKey:        perKeyConcurrency,
		cooldown:      cooldown,
	}
}

func (p *KeyPool) Acquire() (string, func(), bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	n := len(p.keys)
	for i := 0; i < n; i++ {
		idx := (p.cursor + i) % n
		if p.cooldownUntil[idx].After(now) {
			continue
		}
		if p.inFlight[idx] < p.perKey {
			p.inFlight[idx]++
			p.cursor = (idx + 1) % n
			key := p.keys[idx]
			var done bool
			return key, func() {
				p.mu.Lock()
				defer p.mu.Unlock()
				if done {
					return
				}
				done = true
				if p.inFlight[idx] > 0 {
					p.inFlight[idx]--
				}
			}, true
		}
	}
	return "", nil, false
}

func (p *KeyPool) MarkUnavailable(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, k := range p.keys {
		if k == key {
			p.cooldownUntil[i] = time.Now().Add(p.cooldown)
			return
		}
	}
}
```

- [ ] **Step 4: 运行确认通过**

Run: `GOPROXY=https://goproxy.cn,direct GOSUMDB=sum.golang.google.cn go test ./internal/vision/ -run TestConcurrency -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/vision/limiter.go internal/vision/limiter_test.go
git commit -m "feat: add recognition concurrency limiter and kimi key pool"
```

---

### Task 3: 会话隔离复合键

**Files:**
- Modify: `internal/vision/session.go`（新增函数，不动 `ResolveSessionKey`）
- Test: `internal/vision/session_test.go`

**Interfaces:**
- Consumes: `ResolveSessionKey(opts cliproxyexecutor.Options, auth *cliproxyauth.Auth) (SessionKey, bool)`（已有，`session.go:18`）
- Produces:
  - `func ResolveIsolatedSessionKey(opts cliproxyexecutor.Options, auth *cliproxyauth.Auth) (SessionKey, bool)` —— 返回 `auth.ID + "::" + 原SessionKey`；auth 为 nil 或 `auth.ID` 为空或原解析失败 → `("", false)`

- [ ] **Step 1: 写失败测试**

`internal/vision/session_test.go`：

```go
package vision

import (
	"net/http"
	"testing"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

func TestResolveIsolatedSessionKey(t *testing.T) {
	opts := cliproxyexecutor.Options{
		Headers: http.Header{"Session-Id": []string{"sess-1"}},
	}
	auth := &cliproxyauth.Auth{ID: "user-A"}

	key, ok := ResolveIsolatedSessionKey(opts, auth)
	if !ok {
		t.Fatal("expected ok")
	}
	if want := SessionKey("user-A::sess-1"); key != want {
		t.Fatalf("key = %q, want %q", key, want)
	}
}

func TestResolveIsolatedSessionKeySameSessionDifferentUsers(t *testing.T) {
	mkOpts := func(sid string) cliproxyexecutor.Options {
		return cliproxyexecutor.Options{Headers: http.Header{"Session-Id": []string{sid}}}
	}
	a := &cliproxyauth.Auth{ID: "user-A"}
	b := &cliproxyauth.Auth{ID: "user-B"}
	kA, _ := ResolveIsolatedSessionKey(mkOpts("shared"), a)
	kB, _ := ResolveIsolatedSessionKey(mkOpts("shared"), b)
	if kA == kB {
		t.Fatalf("same session id across users collided: %q == %q", kA, kB)
	}
}

func TestResolveIsolatedSessionKeySameUserDifferentSessions(t *testing.T) {
	mkOpts := func(sid string) cliproxyexecutor.Options {
		return cliproxyexecutor.Options{Headers: http.Header{"Session-Id": []string{sid}}}
	}
	u := &cliproxyauth.Auth{ID: "user-A"}
	k1, _ := ResolveIsolatedSessionKey(mkOpts("s1"), u)
	k2, _ := ResolveIsolatedSessionKey(mkOpts("s2"), u)
	if k1 == k2 {
		t.Fatalf("same user sessions collided: %q == %q", k1, k2)
	}
}

func TestResolveIsolatedSessionKeyNilAuth(t *testing.T) {
	opts := cliproxyexecutor.Options{Headers: http.Header{"Session-Id": []string{"sess-1"}}}
	if _, ok := ResolveIsolatedSessionKey(opts, nil); ok {
		t.Fatal("expected !ok for nil auth")
	}
}

func TestResolveIsolatedSessionKeyEmptyAuthID(t *testing.T) {
	opts := cliproxyexecutor.Options{Headers: http.Header{"Session-Id": []string{"sess-1"}}}
	if _, ok := ResolveIsolatedSessionKey(opts, &cliproxyauth.Auth{ID: ""}); ok {
		t.Fatal("expected !ok for empty auth ID")
	}
}

func TestResolveIsolatedSessionKeyNoSession(t *testing.T) {
	if _, ok := ResolveIsolatedSessionKey(cliproxyexecutor.Options{}, &cliproxyauth.Auth{ID: "user-A"}); ok {
		t.Fatal("expected !ok when no session id present")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `GOPROXY=https://goproxy.cn,direct GOSUMDB=sum.golang.google.cn go test ./internal/vision/ -run TestResolveIsolatedSessionKey -v`
Expected: FAIL（函数未定义）

- [ ] **Step 3: 实现**

在 `internal/vision/session.go` 末尾追加：

```go
// ResolveIsolatedSessionKey returns an auth-namespaced session key,
// guaranteeing no cross-user image-memory leakage even when two users send
// the same client-supplied session id. Never fallback to a bare session id:
// if auth is unavailable, cross-turn memory is disabled (single-request only).
func ResolveIsolatedSessionKey(opts cliproxyexecutor.Options, auth *cliproxyauth.Auth) (SessionKey, bool) {
	base, ok := ResolveSessionKey(opts, auth)
	if !ok {
		return "", false
	}
	if auth == nil || auth.ID == "" {
		return "", false
	}
	return SessionKey(auth.ID + "::" + string(base)), true
}
```

（若 `cliproxyauth.Auth` 的 ID 字段名不是 `ID`，先读 `sdk/cliproxy/auth` 包确认字段名，测试随之调整。）

- [ ] **Step 4: 运行确认通过**

Run: `GOPROXY=https://goproxy.cn,direct GOSUMDB=sum.golang.google.cn go test ./internal/vision/ -run TestResolveIsolatedSessionKey -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/vision/session.go internal/vision/session_test.go
git commit -m "feat: add auth-namespaced isolated session key"
```

---

### Task 4: 可配置识别器（Recognizer）

**Files:**
- Create: `internal/vision/recognizer.go`
- Test: `internal/vision/recognizer_test.go`

**Interfaces:**
- Consumes: `PreprocessImage`（Task 1）、`ConcurrencyLimiter`/`KeyPool`（Task 2）、`AnalyzeRequest`/`AnalyzeResponse`/`ImageSummary`/`ImageAnalyzer`（已有，`types.go`/`analyzer.go`）
- Produces:
  - `type RecognizerConfig struct { BaseURL string; APIKeys []string; Model string; MaxConcurrency int; PerKeyConcurrency int; KeyCooldown time.Duration; Timeout time.Duration; Retries int; Preprocess PreprocessConfig; AnalyzeTimeout time.Duration }`
  - `func NewRecognizer(cfg RecognizerConfig) *Recognizer`
  - `func (r *Recognizer) Analyze(ctx context.Context, req AnalyzeRequest) (AnalyzeResponse, error)` —— 实现 `ImageAnalyzer`
  - `func (r *Recognizer) Name() string`
  - 内部：发送 OpenAI 兼容 `POST {BaseURL}/chat/completions`，消息里 `image_url` 用 `PreprocessImage` 处理后的 base64；结构化 prompt 用 `SUMMARY/OCR/LAYOUT/DETAILS`（与 MCP 对齐）

- [ ] **Step 1: 写失败测试**

`internal/vision/recognizer_test.go`（复用 `makeTestJPEG`/`base64Of`，来自 `preprocess_test.go` 同包）：

```go
package vision

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRecognizerSendsPreprocessedImageAndParsesSummary(t *testing.T) {
	var gotImageURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if msgs, ok := body["messages"].([]any); ok && len(msgs) > 0 {
			if user, ok := msgs[len(msgs)-1].(map[string]any); ok {
				if content, ok := user["content"].([]any); ok {
					for _, c := range content {
						cm, ok := c.(map[string]any)
						if !ok {
							continue
						}
						if cm["type"] == "image_url" {
							if iu, ok := cm["image_url"].(map[string]any); ok {
								gotImageURL, _ = iu["url"].(string)
							}
						}
					}
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"SUMMARY: A red button.\nOCR: SUBMIT\nLAYOUT: Button center\nDETAILS: highlighted"}}]}`))
	}))
	defer srv.Close()

	r := NewRecognizer(RecognizerConfig{
		BaseURL:            srv.URL,
		APIKeys:            []string{"k1"},
		Model:              "kimi-k2.6",
		MaxConcurrency:     10,
		PerKeyConcurrency:  5,
		KeyCooldown:        time.Minute,
		Timeout:            5 * time.Second,
		Retries:            0,
		Preprocess:         DefaultPreprocessConfig(),
		AnalyzeTimeout:     5 * time.Second,
	})
	resp, err := r.Analyze(context.Background(), AnalyzeRequest{
		ImageData: base64Of(t, makeTestJPEG(t, 1024, 512)),
		MIMEType:  "image/jpeg",
	})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !strings.Contains(resp.Summary.Summary, "red button") {
		t.Fatalf("summary = %q, want it to mention red button", resp.Summary.Summary)
	}
	if len(resp.Summary.OCRHints) == 0 || resp.Summary.OCRHints[0] != "SUBMIT" {
		t.Fatalf("OCR hints = %v, want [SUBMIT]", resp.Summary.OCRHints)
	}
	if gotImageURL == "" {
		t.Fatal("no image_url sent")
	}
	if len(gotImageURL) > 2000 {
		t.Fatal("image_url appears to carry the full-size image")
	}
}

func TestRecognizerExhaustedKeysReturnsError(t *testing.T) {
	// Single key with per-key concurrency 1; the first request blocks the key,
	// so the second must fail fast with "no kimi key available".
	blocked := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blocked
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"SUMMARY: x"}}]}`))
	}))
	defer srv.Close()

	r := NewRecognizer(RecognizerConfig{
		BaseURL: srv.URL, APIKeys: []string{"k1"},
		Model: "m", MaxConcurrency: 10, PerKeyConcurrency: 1, KeyCooldown: time.Minute,
		Timeout: 5 * time.Second, Retries: 0, Preprocess: DefaultPreprocessConfig(), AnalyzeTimeout: 5 * time.Second,
	})
	img := base64Of(t, makeTestJPEG(t, 64, 64))
	ctx := context.Background()
	firstDone := make(chan struct{})
	go func() {
		_, _ = r.Analyze(ctx, AnalyzeRequest{ImageData: img, MIMEType: "image/jpeg"})
		close(firstDone)
	}()
	// Give the first request time to acquire the key.
	time.Sleep(30 * time.Millisecond)
	if _, err := r.Analyze(ctx, AnalyzeRequest{ImageData: img, MIMEType: "image/jpeg"}); err == nil {
		t.Fatal("expected error when no key available")
	}
	close(blocked)
	<-firstDone
}
```

- [ ] **Step 2: 运行确认失败**

Run: `GOPROXY=https://goproxy.cn,direct GOSUMDB=sum.golang.google.cn go test ./internal/vision/ -run TestRecognizer -v`
Expected: FAIL（`recognizer.go` 不存在）

- [ ] **Step 3: 实现**

`internal/vision/recognizer.go`：

```go
package vision

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type RecognizerConfig struct {
	BaseURL           string
	APIKeys           []string
	Model             string
	MaxConcurrency    int
	PerKeyConcurrency int
	KeyCooldown       time.Duration
	Timeout           time.Duration
	Retries           int
	Preprocess        PreprocessConfig
	AnalyzeTimeout    time.Duration
}

type Recognizer struct {
	cfg     RecognizerConfig
	limiter *ConcurrencyLimiter
	pool    *KeyPool
	client  *http.Client
}

func NewRecognizer(cfg RecognizerConfig) *Recognizer {
	limMax := cfg.MaxConcurrency
	if limMax <= 0 {
		limMax = 100
	}
	return &Recognizer{
		cfg:     cfg,
		limiter: NewConcurrencyLimiter(limMax),
		pool:    NewKeyPool(cfg.APIKeys, cfg.PerKeyConcurrency, cfg.KeyCooldown),
		client:  &http.Client{Timeout: cfg.Timeout},
	}
}

func (r *Recognizer) Name() string { return "recognizer" }

func (r *Recognizer) Analyze(ctx context.Context, req AnalyzeRequest) (AnalyzeResponse, error) {
	raw, err := base64.StdEncoding.DecodeString(req.ImageData)
	if err != nil {
		return AnalyzeResponse{}, fmt.Errorf("decode image data: %w", err)
	}
	proc, err := PreprocessImage(raw, PreprocessModeStandard, r.cfg.Preprocess)
	if err != nil {
		return AnalyzeResponse{}, fmt.Errorf("preprocess image: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, r.cfg.AnalyzeTimeout)
	defer cancel()

	var resp AnalyzeResponse
	err = r.limiter.Run(ctx, func() error {
		key, release, ok := r.pool.Acquire()
		if !ok {
			return fmt.Errorf("no kimi key available")
		}
		defer release()

		body := r.buildBody(proc.Base64, req)
		var lastErr error
		for attempt := 0; attempt <= r.cfg.Retries; attempt++ {
			out, err := r.doRequest(ctx, key, body)
			if err == nil {
				parsed, perr := r.parseSummary(out, req)
				if perr != nil {
					return perr
				}
				resp = parsed
				return nil
			}
			lastErr = err
			if isRetryable(err) {
				r.pool.MarkUnavailable(key)
			}
		}
		return lastErr
	})
	return resp, err
}

func (r *Recognizer) buildBody(imageData string, req AnalyzeRequest) []byte {
	prompt := "You are an image analyzer for a coding assistant. Describe this image in detail, focusing on:\n\nSUMMARY: 1-2 sentence overall description of what this image shows.\nOCR: Any text visible in the image (error messages, code, UI labels).\nLAYOUT: The layout structure, key visual elements, their relative positions.\nDETAILS: Any other notable details (colors, icons, UI state, highlighted elements).\n\nBe thorough — the model receiving this data cannot see the original image."
	if req.IsFollowUp && req.Existing.Summary != "" {
		prompt = fmt.Sprintf("You previously analyzed this image:\nSUMMARY: %s\nOCR: %s\nLAYOUT: %s\nDETAILS: %s\n\nThe user asks: %q\nProvide ONLY new supplementary info not covered above.", req.Existing.Summary, strings.Join(req.Existing.OCRHints, "; "), strings.Join(req.Existing.LayoutHints, "; "), strings.Join(req.Existing.DetailHints, "; "), req.Query)
	}
	dataURL := "data:image/jpeg;base64," + imageData
	body := map[string]any{
		"model": r.cfg.Model,
		"messages": []map[string]any{
			{"role": "system", "content": "You are an expert image analyst. Provide structured, detailed descriptions."},
			{"role": "user", "content": []map[string]any{
				{"type": "text", "text": prompt},
				{"type": "image_url", "image_url": map[string]any{"url": dataURL}},
			}},
		},
		"max_tokens": 1024,
	}
	out, _ := json.Marshal(body)
	return out
}

func (r *Recognizer) doRequest(ctx context.Context, apiKey string, body []byte) ([]byte, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(r.cfg.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := r.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API status %d: %s", resp.StatusCode, string(out))
	}
	return out, nil
}

func (r *Recognizer) parseSummary(data []byte, req AnalyzeRequest) (AnalyzeResponse, error) {
	var raw struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return AnalyzeResponse{}, err
	}
	if len(raw.Choices) == 0 {
		return AnalyzeResponse{}, fmt.Errorf("empty choices")
	}
	return AnalyzeResponse{Summary: parseStructuredResponse(raw.Choices[0].Message.Content, req)}, nil
}

func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "429") || strings.Contains(s, "502") || strings.Contains(s, "503")
}
```

- [ ] **Step 4: 运行确认通过**

Run: `GOPROXY=https://goproxy.cn,direct GOSUMDB=sum.golang.google.cn go test ./internal/vision/ -run TestRecognizer -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/vision/recognizer.go internal/vision/recognizer_test.go
git commit -m "feat: add configurable kimi recognizer with preprocess, limiter, key pool"
```

---

### Task 5: `vision` 配置段 + 渠道解析 + 识别器构建

**Files:**
- Modify: `internal/config/sections.go`（新增 `VisionConfig` 结构体）
- Modify: `internal/config/config.go`（`Config` 新增 `Vision VisionConfig` 字段）
- Create: `internal/runtime/executor/vision_channels.go`（`visionChannelBaseURL` + `newVisionRecognizer`）
- Test: `internal/runtime/executor/vision_channels_test.go`

**Interfaces:**
- Consumes: `RecognizerConfig`（Task 4）；`config.ClaudeKey`（已有：`Name`/`BaseURL`/`APIKey`）
- Produces:
  - `type VisionConfig struct { Enabled bool; Channel string; Model string; MaxSizeMB int; MaxDimension int; OCRMaxDimension int; JPEGQuality int; MaxConcurrency int; PerKeyConcurrency int; KeyCooldownMs int; AnalyzeTimeoutMs int; Retries int }`（yaml 标签蛇形）
  - `func DefaultVisionConfig() VisionConfig`
  - `func visionChannelBaseURL(cfg *config.Config, channel string) (string, []string)` —— 遍历 `cfg.ClaudeKey`，收集 `Name == channel` 的 baseURL（第一个非空）与全部 APIKey
  - `func (e *ClaudeExecutor) newVisionRecognizer() *vision.Recognizer`（nil 表示未启用或渠道缺失）

- [ ] **Step 1: 写失败测试**

`internal/runtime/executor/vision_channels_test.go`：

```go
package executor

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/vision"
)

func TestVisionChannelBaseURLCollectsKeysByChannelName(t *testing.T) {
	cfg := &config.Config{
		ClaudeKey: []config.ClaudeKey{
			{Name: "xunfei-199", BaseURL: "https://kimi.example.com", APIKey: "k1"},
			{Name: "xunfei-199", BaseURL: "https://kimi.example.com", APIKey: "k2"},
			{Name: "other", BaseURL: "https://other.example.com", APIKey: "k9"},
		},
	}
	baseURL, keys := visionChannelBaseURL(cfg, "xunfei-199")
	if baseURL != "https://kimi.example.com" {
		t.Fatalf("baseURL = %q", baseURL)
	}
	if len(keys) != 2 || keys[0] != "k1" || keys[1] != "k2" {
		t.Fatalf("keys = %v, want [k1 k2]", keys)
	}
}

func TestVisionChannelBaseURLUnknownChannel(t *testing.T) {
	cfg := &config.Config{ClaudeKey: []config.ClaudeKey{{Name: "x", BaseURL: "u", APIKey: "k"}}}
	baseURL, keys := visionChannelBaseURL(cfg, "missing")
	if baseURL != "" || len(keys) != 0 {
		t.Fatalf("expected empty, got %q %v", baseURL, keys)
	}
}

func TestNewVisionRecognizerDisabled(t *testing.T) {
	v := config.DefaultVisionConfig()
	v.Enabled = false
	e := NewClaudeExecutor(&config.Config{Vision: v})
	if r := e.newVisionRecognizer(); r != nil {
		t.Fatal("expected nil recognizer when disabled")
	}
}

func TestNewVisionRecognizerMissingChannel(t *testing.T) {
	v := config.DefaultVisionConfig()
	v.Channel = "missing"
	e := NewClaudeExecutor(&config.Config{
		Vision:    v,
		ClaudeKey: []config.ClaudeKey{{Name: "x", BaseURL: "u", APIKey: "k"}},
	})
	if r := e.newVisionRecognizer(); r != nil {
		t.Fatal("expected nil recognizer when channel not found")
	}
}

func TestNewVisionRecognizerBuilds(t *testing.T) {
	v := config.DefaultVisionConfig()
	v.Channel = "xunfei-199"
	e := NewClaudeExecutor(&config.Config{
		Vision:    v,
		ClaudeKey: []config.ClaudeKey{{Name: "xunfei-199", BaseURL: "https://kimi.example.com", APIKey: "k1"}},
	})
	r := e.newVisionRecognizer()
	if r == nil {
		t.Fatal("expected non-nil recognizer")
	}
	var _ *vision.Recognizer = r // compile-time type check
}
```

- [ ] **Step 2: 运行确认失败**

Run: `GOPROXY=https://goproxy.cn,direct GOSUMDB=sum.golang.google.cn go test ./internal/runtime/executor/ -run "VisionChannel|NewVisionRecognizer" -v`
Expected: FAIL（`VisionConfig` / `visionChannelBaseURL` / `newVisionRecognizer` 未定义）

- [ ] **Step 3: 实现配置结构**

`internal/config/sections.go` 追加：

```go
// VisionConfig holds server-side image externalization settings.
type VisionConfig struct {
	Enabled           bool   `yaml:"enabled" json:"enabled"`
	Channel           string `yaml:"channel" json:"channel"`
	Model             string `yaml:"model" json:"model"`
	MaxSizeMB         int    `yaml:"max-size-mb" json:"max-size-mb"`
	MaxDimension      int    `yaml:"max-dimension" json:"max-dimension"`
	OCRMaxDimension   int    `yaml:"ocr-max-dimension" json:"ocr-max-dimension"`
	JPEGQuality       int    `yaml:"jpeg-quality" json:"jpeg-quality"`
	MaxConcurrency    int    `yaml:"max-concurrency" json:"max-concurrency"`
	PerKeyConcurrency int    `yaml:"per-key-concurrency" json:"per-key-concurrency"`
	KeyCooldownMs     int    `yaml:"key-cooldown-ms" json:"key-cooldown-ms"`
	AnalyzeTimeoutMs  int    `yaml:"analyze-timeout-ms" json:"analyze-timeout-ms"`
	Retries           int    `yaml:"retries" json:"retries"`
}

// DefaultVisionConfig returns the defaults. Model and analyze-timeout are the
// two fields the ops team will tune first.
func DefaultVisionConfig() VisionConfig {
	return VisionConfig{
		Enabled:           true,
		Channel:           "xunfei-199",
		Model:             "kimi-k2.6",
		MaxSizeMB:         10,
		MaxDimension:      2048,
		OCRMaxDimension:   4096,
		JPEGQuality:       80,
		MaxConcurrency:    100,
		PerKeyConcurrency: 20,
		KeyCooldownMs:     60000,
		AnalyzeTimeoutMs:  5000,
		Retries:           2,
	}
}
```

`internal/config/config.go` 的 `Config` 结构体追加字段（放在 `RequestBody` 字段附近）：

```go
	// Vision controls server-side image externalization and recognition.
	Vision VisionConfig `yaml:"vision" json:"vision"`
```

- [ ] **Step 4: 实现渠道解析 + 识别器构建**

`internal/runtime/executor/vision_channels.go`：

```go
package executor

import (
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/vision"
)

// visionChannelBaseURL resolves the shared base URL and all API keys of a
// named channel (e.g. "xunfei-199", which carries 10 kimi keys).
func visionChannelBaseURL(cfg *config.Config, channel string) (string, []string) {
	if cfg == nil {
		return "", nil
	}
	var keys []string
	baseURL := ""
	for _, k := range cfg.ClaudeKey {
		if !strings.EqualFold(strings.TrimSpace(k.Name), channel) {
			continue
		}
		if baseURL == "" && k.BaseURL != "" {
			baseURL = k.BaseURL
		}
		if k.APIKey != "" {
			keys = append(keys, k.APIKey)
		}
	}
	return baseURL, keys
}

// newVisionRecognizer builds the kimi recognizer from the vision config.
// Returns nil when vision is disabled or the configured channel is missing.
func (e *ClaudeExecutor) newVisionRecognizer() *vision.Recognizer {
	if e == nil || e.cfg == nil || !e.cfg.Vision.Enabled {
		return nil
	}
	v := e.cfg.Vision
	baseURL, keys := visionChannelBaseURL(e.cfg, v.Channel)
	if baseURL == "" || len(keys) == 0 {
		return nil
	}
	return vision.NewRecognizer(vision.RecognizerConfig{
		BaseURL:           baseURL,
		APIKeys:           keys,
		Model:             v.Model,
		MaxConcurrency:    v.MaxConcurrency,
		PerKeyConcurrency: v.PerKeyConcurrency,
		KeyCooldown:       time.Duration(v.KeyCooldownMs) * time.Millisecond,
		Timeout:           30 * time.Second,
		Retries:           v.Retries,
		Preprocess: vision.PreprocessConfig{
			MaxSizeBytes:   v.MaxSizeMB * 1024 * 1024,
			StandardMaxDim: v.MaxDimension,
			OCRMaxDim:      v.OCRMaxDimension,
			DiffMaxDim:     v.MaxDimension,
			JPEGQuality:    v.JPEGQuality,
		},
		AnalyzeTimeout: time.Duration(v.AnalyzeTimeoutMs) * time.Millisecond,
	})
}
```

- [ ] **Step 5: 运行确认通过**

Run: `GOPROXY=https://goproxy.cn,direct GOSUMDB=sum.golang.google.cn go test ./internal/runtime/executor/ -run "VisionChannel|NewVisionRecognizer" -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/config/sections.go internal/config/config.go internal/runtime/executor/vision_channels.go internal/runtime/executor/vision_channels_test.go
git commit -m "feat: add vision config section and channel-based recognizer construction"
```

---

### Task 6: ClaudeExecutor 图像外部化接线

**Files:**
- Modify: `internal/vision/walk.go`（新增 `ReplaceAllImages`）
- Modify: `internal/runtime/executor/claude_executor.go`（`Execute` `:97` 前、`ExecuteStream`、`count_tokens`）
- Test: `internal/runtime/executor/claude_executor_test.go`

**Interfaces:**
- Consumes: `ResolveIsolatedSessionKey`（Task 3）、`Recognizer`（Task 4）、`newVisionRecognizer`（Task 5）、`vision.Processor`/`A3ProcessCurrentTurn`/`CurrentTurnHasImages`（已有）、`ReplaceCurrentTurnImages`（已有）
- Produces:
  - `func ReplaceAllImages(payload []byte, placeholder string) ([]byte, error)`（`internal/vision`，替换所有图块）
  - 私有方法 `func (e *ClaudeExecutor) externalizeImages(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options, payload []byte) []byte`

- [ ] **Step 1: 在 vision 包加 `ReplaceAllImages` + 测试**

`internal/vision/walk.go` 追加：

```go
// ReplaceAllImages replaces every image content part (current and historical)
// with a text placeholder. Used when no recognizer is available so image bytes
// never reach the upstream model.
func ReplaceAllImages(payload []byte, placeholder string) ([]byte, error) {
	walk := WalkPayload(payload)
	arrayType := detectArrayType(payload)
	for _, part := range walk.Parts {
		var err error
		payload, err = ReplaceImagePartEx(payload, part, placeholder, arrayType)
		if err != nil {
			return payload, err
		}
	}
	return payload, nil
}
```

在 `internal/vision/registry_test.go` 追加（或新建 `walk_all_test.go`）：

```go
func TestReplaceAllImages(t *testing.T) {
	img := base64Of(t, makeTestJPEG(t, 64, 64))
	payload := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"hi"},{"type":"image","source":{"type":"base64","data":"` + img + `","media_type":"image/jpeg"}}]},{"role":"user","content":[{"type":"image","source":{"type":"base64","data":"` + img + `","media_type":"image/jpeg"}}]}]}`)
	out, err := ReplaceAllImages(payload, "[Image Registry] placeholder")
	if err != nil {
		t.Fatalf("ReplaceAllImages: %v", err)
	}
	if got := string(out); strings.Contains(got, `"type":"image"`) {
		t.Fatalf("image blocks remain: %s", got)
	}
}
```

（需要 import `strings`。运行 `-run TestReplaceAllImages` 确认通过后继续。）

- [ ] **Step 2: 写失败测试（executor 外部化）**

`internal/runtime/executor/claude_executor_test.go` 追加（复用该文件现有 import 与模式）：

```go
func testSmallJPEGBase64(t *testing.T) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestClaudeExecutorExternalizesImages(t *testing.T) {
	var upstreamBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			// Recognizer path: return a structured summary.
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"SUMMARY: a red button.\nOCR: SUBMIT\nLAYOUT: center"}}]}`))
		default:
			// Upstream Claude path: capture body, return a Claude message.
			body, _ := io.ReadAll(r.Body)
			upstreamBody = body
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","model":"m","role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`))
		}
	}))
	defer srv.Close()

	v := config.DefaultVisionConfig()
	v.Channel = "xunfei-199"
	executor := NewClaudeExecutor(&config.Config{
		Vision:    v,
		ClaudeKey: []config.ClaudeKey{{Name: "xunfei-199", BaseURL: srv.URL, APIKey: "k1"}},
	})
	auth := &cliproxyauth.Auth{
		ID:         "user-A",
		Attributes: map[string]string{"api_key": "k1", "base_url": srv.URL},
	}

	imgB64 := testSmallJPEGBase64(t)
	payload := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"what is this?"},{"type":"image","source":{"type":"base64","data":"` + imgB64 + `","media_type":"image/jpeg"}}]}]}`)

	if _, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "kimi-k2.6",
		Payload: payload,
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("claude"),
	}); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if len(upstreamBody) == 0 {
		t.Fatal("upstream never received request")
	}
	bodyStr := string(upstreamBody)
	if strings.Contains(bodyStr, `"type":"image"`) {
		t.Fatal("image block still present in upstream body")
	}
	if !strings.Contains(bodyStr, "[Image Registry - Text Summary]") {
		t.Fatalf("expected summary placeholder in upstream body, got: %s", bodyStr)
	}
}

func TestClaudeExecutorVisionDisabledLeavesImage(t *testing.T) {
	var upstreamBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		upstreamBody = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","model":"m","role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	v := config.DefaultVisionConfig()
	v.Enabled = false
	executor := NewClaudeExecutor(&config.Config{
		Vision:    v,
		ClaudeKey: []config.ClaudeKey{{Name: "xunfei-199", BaseURL: srv.URL, APIKey: "k1"}},
	})
	auth := &cliproxyauth.Auth{
		ID:         "user-A",
		Attributes: map[string]string{"api_key": "k1", "base_url": srv.URL},
	}

	imgB64 := testSmallJPEGBase64(t)
	payload := []byte(`{"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","data":"` + imgB64 + `","media_type":"image/jpeg"}}]}]}`)

	if _, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "m",
		Payload: payload,
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("claude"),
	}); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !strings.Contains(string(upstreamBody), `"type":"image"`) {
		t.Fatal("image block should remain when vision disabled")
	}
}
```

（需要确保测试文件顶部 import 含 `bytes`、`encoding/base64`、`image`、`image/jpeg`；与现有 import 合并。）

- [ ] **Step 3: 运行确认失败**

Run: `GOPROXY=https://goproxy.cn,direct GOSUMDB=sum.golang.google.cn go test ./internal/runtime/executor/ -run TestClaudeExecutorExternalize -v`
Expected: FAIL（`externalizeImages` 不存在，上游 body 仍含 image 块）

- [ ] **Step 4: 实现 `externalizeImages`**

在 `internal/runtime/executor/claude_executor.go` 新增：

```go
// externalizeImages replaces all image content blocks with text summaries so
// the upstream model never receives raw image bytes. Disabled vision is a
// strict no-op; enabled vision with no recognizer degrades to placeholders.
func (e *ClaudeExecutor) externalizeImages(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options, payload []byte) []byte {
	if e == nil || e.cfg == nil || !e.cfg.Vision.Enabled {
		return payload
	}
	analyzer := e.newVisionRecognizer()
	sessionKey, _ := vision.ResolveIsolatedSessionKey(opts, auth)

	if analyzer == nil {
		payload, _ = vision.ReplaceAllImages(payload, "[Image Registry] 无可用的图片分析模型。")
		return payload
	}

	proc := vision.NewProcessor(analyzer)
	procRes, _ := proc.Process(ctx, payload, sessionKey, 0)
	payload = procRes.Payload

	if vision.CurrentTurnHasImages(payload) {
		a3, err := proc.A3ProcessCurrentTurn(ctx, payload, sessionKey, 0)
		if err == nil {
			payload = a3
		} else {
			payload, _ = vision.ReplaceAllImages(payload, "[Image Registry] 图片分析失败，无法提供文本摘要。")
		}
	}
	return payload
}
```

- [ ] **Step 5: 接入三个入口**

- `Execute`（`claude_executor.go:78`）：在 `body, originalTranslated := execCtx.TranslateRequestPair(req.Payload)`（`:97`）**之前**插入一行：

```go
	req.Payload = e.externalizeImages(ctx, auth, req, opts, req.Payload)
```

- `ExecuteStream`（`:215`）：在流式 body 构建前的对应 `TranslateRequestPair`（或 `req.Payload` 首次被消费处）之前插入同一行
- `count_tokens`（`:419`）：在请求体构建前插入同一行

- [ ] **Step 6: 运行确认通过**

Run: `GOPROXY=https://goproxy.cn,direct GOSUMDB=sum.golang.google.cn go test ./internal/runtime/executor/ -run TestClaudeExecutorExternalize -v`
Expected: PASS

- [ ] **Step 7: 回归现有测试**

Run: `GOPROXY=https://goproxy.cn,direct GOSUMDB=sum.golang.google.cn go test ./internal/vision/... ./internal/runtime/executor/...`
Expected: 全部 PASS（现有 opencode 路径不受影响）

- [ ] **Step 8: 提交**

```bash
git add internal/vision/walk.go internal/runtime/executor/claude_executor.go internal/runtime/executor/claude_executor_test.go
git commit -m "feat: externalize images in Claude executor before forwarding"
```

---

### Task 7: 对比验证门测试集 Harness

**Files:**
- Create: `scripts/vision-compare/main.go`
- Create: `scripts/vision-compare/README.md`（用法）
- Create: `scripts/vision-compare/samples/`（测试图，人工放入）

**Interfaces:**
- Consumes: `vision.NewRecognizer`（Task 4）+ MCP `image_analysis`（HTTP 直调）
- Produces: 逐图对比报告 JSON（每张：`image`, `server_ocr`, `mcp_ocr`, `ocr_f1`, `server_summary`, `mcp_summary`, `server_ok`, `mcp_ok`）

- [ ] **Step 1: 写 harness 骨架**

`scripts/vision-compare/main.go`（独立 `main` 包，读 `-samples` 目录）：
- 对每张图跑两条通道：
  1. 服务器管线：`vision.NewRecognizer(...)`（baseURL/keys 从 `-base-url`/`-keys` flag 取）+ `Analyze`
  2. 客户端 MCP：HTTP 直调 MCP 分析端点（参考 `C:\Users\wensp\.claude\mcp-servers\vision-mcp-server\src\tools\image-analysis.ts` 的入参：`image.path` 或 `image.base64` + `question`）
- OCR 归一化 + token 级 F1
- 摘要：把两份喂给 kimi 让 LLM 评判胜者，同时落盘供人工抽检
- 汇总输出 JSON 报告 + 控制台表格

- [ ] **Step 2: 放测试集**

`scripts/vision-compare/samples/` 放入按 spec §8 分类的图（极限尺寸 / OCR 密集 / 结构化），每张配 `question`（可空）。

- [ ] **Step 3: 实现 `ocrF1` 与摘要评分**

`ocrF1(server, mcp []string) float64`：两端片段归一化（去空白/小写/去标点）→ 词集合 F1。摘要评分：`server_summary` 与 `mcp_summary` 作为 user 文本发给 kimi（`Recognizer.Analyze` 的文本-only 变体或直调），prompt「哪份更全更准」，记录胜者。

- [ ] **Step 4: 运行并产出报告**

Run: `go run ./scripts/vision-compare -samples scripts/vision-compare/samples -report out.json`
对照 spec §8 通过线（OCR F1≥0.9、LLM-judge≥50%、极限尺寸 100%）人工判定。

- [ ] **Step 5: 提交**

```bash
git add scripts/vision-compare/
git commit -m "test: add server-vs-MCP vision comparison harness"
```

---

### Task 8: 100 并发负载测试

**Files:**
- Create: `internal/vision/load_test.go`（`//go:build load` 标签，不进常规 CI）

**Interfaces:**
- Consumes: `Recognizer`（Task 4）
- Produces: 断言 100 并发下无挂起、in-flight ≤ 上限

- [ ] **Step 1: 写测试**

`internal/vision/load_test.go`：

```go
//go:build load

package vision

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRecognizer100Concurrent(t *testing.T) {
	var inFlight atomic.Int64
	var maxInFlight atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			m := maxInFlight.Load()
			if v <= m || maxInFlight.CompareAndSwap(m, v) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"SUMMARY: ok"}}]}`))
	}))
	defer srv.Close()

	r := NewRecognizer(RecognizerConfig{
		BaseURL: srv.URL, APIKeys: []string{"k1", "k2", "k3", "k4", "k5"},
		Model: "m", MaxConcurrency: 100, PerKeyConcurrency: 20, KeyCooldown: time.Minute,
		Timeout: 30 * time.Second, Retries: 0,
		Preprocess: DefaultPreprocessConfig(), AnalyzeTimeout: 5 * time.Second,
	})
	var wg sync.WaitGroup
	errs := make(chan error, 100)
	img := base64Of(t, makeTestJPEG(t, 512, 512))
	ctx := context.Background()
	start := time.Now()
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := r.Analyze(ctx, AnalyzeRequest{ImageData: img, MIMEType: "image/jpeg"})
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent analyze failed: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Fatalf("100 concurrent took %v, too slow", elapsed)
	}
	if got := maxInFlight.Load(); got > 100 {
		t.Fatalf("max in-flight = %d > 100", got)
	}
}
```

- [ ] **Step 2: 运行确认**

Run: `GOPROXY=https://goproxy.cn,direct GOSUMDB=sum.golang.google.cn go test ./internal/vision/ -tags load -run TestRecognizer100Concurrent -v`
Expected: PASS（100 并发全部成功，in-flight ≤ 100，15s 内完成）

- [ ] **Step 3: 提交**

```bash
git add internal/vision/load_test.go
git commit -m "test: add 100-concurrent recognition load test"
```

---

## 执行顺序与依赖

```
Task 1 (preprocess) → Task 4 (recognizer) ─┐
Task 2 (limiter) ──────────────────────────┘
Task 3 (isolation) ──────────────────────┐
Task 5 (config) ─────────────────────────┤→ Task 6 (executor wiring) → Task 7 (comparison) / Task 8 (load)
```

每个 Task 独立可测试、可提交。Task 7 需真实 kimi key 与 MCP；Task 8 用 mock 上游。

## 验收标准（对照 spec §2）

- [ ] 文本模型收到图不再 400（Task 6 测试）
- [ ] 图片不进对话模型上下文（Task 6：转发 body 无 image 块）
- [ ] 会话隔离：同会话 ID 不同用户不串（Task 3 测试）
- [ ] 100 并发通过（Task 8）
- [ ] 现有 opencode 路径测试全绿（Task 6 Step 7 回归）
- [ ] 对比验证门报告产出、判定达标后弃用 MCP（Task 7，人工判定）
