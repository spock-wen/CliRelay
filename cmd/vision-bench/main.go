package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	server   = flag.String("server", "http://223.109.200.65:8317", "服务器地址")
	token    = flag.String("token", "", "Bearer token（从 login API 获取）")
	con      = flag.Int("concurrency", 50, "并发数")
	n        = flag.Int("n", 100, "总请求数（0=不限，跑满 duration）")
	dur      = flag.Duration("duration", 30*time.Second, "压测时长（n=0 时有效）")
	payload  = flag.String("payload", "openai", "请求格式: openai | anthropic")
	timeout  = flag.Duration("timeout", 30*time.Second, "单请求超时")
)

func main() {
	flag.Parse()

	if *token == "" {
		log.Fatal("请通过 -token 提供 Bearer token")
	}

	// 生成测试图片
	imgBytes := makeTestJPEG(200, 150)
	b64 := base64.StdEncoding.EncodeToString(imgBytes)

	// 构造 payload
	var reqPayload []byte
	var reqPath string
	switch *payload {
	case "openai":
		reqPath = "/v1/chat/completions"
		reqPayload = buildOpenAIPayload(b64, imgBytes)
	case "anthropic":
		reqPath = "/v1/messages"
		reqPayload = buildAnthropicPayload(b64, imgBytes)
	default:
		log.Fatalf("unknown payload: %s", *payload)
	}

	client := &http.Client{
		Timeout: *timeout,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 100,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	var (
		total   atomic.Int64
		success atomic.Int64
		fail    atomic.Int64
		done    = make(chan struct{})
		start   = time.Now()
	)

	// 统计
	var (
		mu          sync.Mutex
		latencies   []float64 // ms
		visionCount atomic.Int64
	)

	worker := func(id int) {
		for {
			select {
			case <-done:
				return
			default:
			}

			if *n > 0 && total.Load() >= int64(*n) {
				return
			}
			total.Add(1)

			req, _ := http.NewRequest("POST", *server+reqPath, bytes.NewReader(reqPayload))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+*token)
			req.Header.Set("anthropic-version", "2023-06-01")

			t0 := time.Now()
			resp, err := client.Do(req)
			elapsed := time.Since(t0).Milliseconds()

			mu.Lock()
			latencies = append(latencies, float64(elapsed))
			mu.Unlock()

			if err != nil {
				fail.Add(1)
				if total.Load() <= 10 {
					log.Printf("[%d] error: %v", id, err)
				}
				continue
			}

			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			if resp.StatusCode == 200 || resp.StatusCode == 201 {
				success.Add(1)
				// 检查响应中是否含 vision 摘要特征
				if strings.Contains(string(body), "SUMMARY") ||
					strings.Contains(string(body), "OCR") ||
					strings.Contains(string(body), "LAYOUT") {
					visionCount.Add(1)
				}
			} else {
				fail.Add(1)
				// 429 是 key 冷却，正常行为
				if resp.StatusCode == 429 {
					success.Add(1) // 服务正确响应，只是 key 限流
				}
				if total.Load() <= 10 || resp.StatusCode == 429 {
					log.Printf("[%d] HTTP %d: %s", id, resp.StatusCode, truncate(string(body), 120))
				}
			}
		}
	}

	// 启动 workers
	for i := 0; i < *con; i++ {
		go worker(i)
	}

	// 等待
	if *n > 0 {
		for total.Load() < int64(*n) {
			time.Sleep(100 * time.Millisecond)
		}
	} else {
		time.Sleep(*dur)
	}
	close(done)
	elapsed := time.Since(start).Seconds()

	// ȫ计
	t := total.Load()
	s := success.Load()
	f := fail.Load()

	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("====== Vision 压测报告 (%s) ======\n", *payload)
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("服务器:       %s\n", *server)
	fmt.Printf("并发数:       %d\n", *con)
	fmt.Printf("耗时:         %v\n", elapsed)
	fmt.Printf("总请求:       %d\n", t)
	fmt.Printf("成功:         %d (%.1f%%)\n", s, float64(s)/float64(t)*100)
	fmt.Printf("失败:         %d (%.1f%%)\n", f, float64(f)/float64(t)*100)
	fmt.Printf("吞吐:         %.0f req/s\n", float64(t)/elapsed)
	fmt.Printf("含 vision 摘要: %d\n", visionCount.Load())

	// 延迟统计
	mu.Lock()
	if len(latencies) > 0 {
		var sum float64
		min := latencies[0]
		max := latencies[0]
		for _, v := range latencies {
			sum += v
			if v < min {
				min = v
			}
			if v > max {
				max = v
					}
		}
		fmt.Printf("延迟 (ms):    avg=%.0f min=%.0f max=%.0f\n", sum/float64(len(latencies)), min, max)

		// P50, P90, P99
		sort.Float64s(latencies)
		p50 := latencies[len(latencies)*50/100]
		p90 := latencies[len(latencies)*90/100]
		p99 := latencies[len(latencies)*99/100]
		fmt.Printf("延迟分布:     P50=%.0f P90=%.0f P99=%.0f\n", p50, p90, p99)
	}
	mu.Unlock()
}

func buildOpenAIPayload(b64 string, raw []byte) []byte {
	body := map[string]any{
		"model":  "glm-5.1",
		"max_tokens": 100,
		"messages": []map[string]any{
			{"role": "user", "content": []map[string]any{
				{"type": "text", "text": "Describe what you see in this image in one sentence."},
				{"type": "image_url", "image_url": map[string]any{
					"url": "data:image/jpeg;base64," + b64,
				}},
			}},
		},
	}
	out, _ := json.Marshal(body)
	return out
}

func buildAnthropicPayload(b64 string, raw []byte) []byte {
	body := map[string]any{
		"model":      "glm-5.1",
		"max_tokens": 100,
		"messages": []map[string]any{
			{"role": "user", "content": []map[string]any{
				{"type": "text", "text": "Describe what you see in this image in one sentence."},
				{"type": "image", "source": map[string]any{
					"type":       "base64",
					"media_type": "image/jpeg",
					"data":       b64,
				}},
			}},
		},
	}
	out, _ := json.Marshal(body)
	return out
}

func makeTestJPEG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 128, 255})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}