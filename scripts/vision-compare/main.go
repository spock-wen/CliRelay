// Command vision-compare runs the same sample images through two recognition
// channels and produces a per-image comparison report:
//
//  1. Server pipeline — vision.NewRecognizer + Analyze (OpenAI-compatible
//     /chat/completions upstream), which returns a structured ImageSummary.
//  2. Client MCP — the image_analysis tool's model endpoint (Anthropic Messages
//     API /v1/messages) called directly with the same image + question
//     semantics the MCP server would build, and the same preprocessing
//     (resize to <=2048px, JPEG q80).
//
// The report (JSON + console table) is used to validate the server reaches
// MCP-level quality before the MCP is retired.
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/vision"
)

const (
	// defaultQuestion is used for samples that have no sidecar <base>.txt
	// question. It is deliberately OCR-inclusive so the MCP channel's free-form
	// answer carries the visible text needed for a meaningful OCR-F1.
	defaultQuestion = "Describe this image in detail. List ALL visible text verbatim (error messages, code, UI labels, numbers), then describe the layout and notable details."

	// anthropicVersion is the header value the MCP's ModelClient sends.
	anthropicVersion = "2023-06-01"

	// mcpSystemPrompt mirrors SYSTEM_PROMPTS.imageAnalysis in the vision MCP.
	mcpSystemPrompt = "你是一名专业的图像分析助手。请根据用户的问题，对提供的图片进行准确、客观的分析。回答使用中文，结构清晰，先给出结论再补充细节。只描述图片中确实可见的内容，不要臆测。"
)

var imageExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".bmp": true, ".gif": true,
}

type options struct {
	samples     string
	baseURL     string
	keys        string
	model       string
	mcpBaseURL  string
	mcpKey      string
	mcpModel    string
	report      string
	question    string
	timeout     time.Duration
	concurrency int
	judgeURL    string
	judgeKey    string
	judgeModel  string
}

type harness struct {
	opts   options
	rec    *vision.Recognizer
	client *http.Client
}

type imageReport struct {
	Image          string   `json:"image"`
	Question       string   `json:"question"`
	ServerOCR      []string `json:"server_ocr"`
	McpOCR         []string `json:"mcp_ocr"`
	McpOCRFallback bool     `json:"mcp_ocr_fallback"`
	OCRF1          float64  `json:"ocr_f1"`
	ServerSummary  string   `json:"server_summary"`
	McpSummary     string   `json:"mcp_summary"`
	ServerOK       bool     `json:"server_ok"`
	McpOK          bool     `json:"mcp_ok"`
	ServerError    string   `json:"server_error,omitempty"`
	McpError       string   `json:"mcp_error,omitempty"`
	JudgeWinner    string   `json:"judge_winner"`
	SizeBytes      int64    `json:"size_bytes"`
}

type configInfo struct {
	Samples      string `json:"samples"`
	BaseURL      string `json:"base_url"`
	Model        string `json:"model"`
	McpBaseURL   string `json:"mcp_base_url"`
	McpModel     string `json:"mcp_model"`
	JudgeEnabled bool   `json:"judge_enabled"`
	JudgeModel   string `json:"judge_model,omitempty"`
}

type aggregate struct {
	ImagesTotal         int     `json:"images_total"`
	ImagesServerOK      int     `json:"images_server_ok"`
	ImagesMcpOK         int     `json:"images_mcp_ok"`
	ImagesBothOK        int     `json:"images_both_ok"`
	BothOKPassRate      float64 `json:"both_ok_pass_rate"` // robustness / extreme-size pass line (spec §8: 100%)
	AvgOCRF1            float64 `json:"avg_ocr_f1"`        // over images both channels handled; upper-bound heuristic when McpOCRFallbackCount > 0
	McpOCRFallbackCount int     `json:"mcp_ocr_fallback_count"`
	JudgeServerWins     int     `json:"judge_server_wins"`
	JudgeMcpWins        int     `json:"judge_mcp_wins"`
	JudgeTies           int     `json:"judge_ties"`
	JudgePending        int     `json:"judge_pending"`
	LLMJudgePassRate    float64 `json:"llm_judge_pass_rate"` // server wins / judged images (spec §8: >= 50%)
}

type report struct {
	GeneratedAt string        `json:"generated_at"`
	Config      configInfo    `json:"config"`
	Results     []imageReport `json:"results"`
	Aggregate   aggregate     `json:"aggregate"`
}

func main() {
	opts := parseFlags()
	if err := opts.validate(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		flag.Usage()
		os.Exit(2)
	}

	images, err := listImages(opts.samples)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	if len(images) == 0 {
		fmt.Fprintln(os.Stderr, "no sample images found in", opts.samples)
		os.Exit(1)
	}

	rec := vision.NewRecognizer(vision.RecognizerConfig{
		BaseURL:           opts.baseURL,
		APIKeys:           splitKeys(opts.keys),
		Model:             opts.model,
		MaxConcurrency:    opts.concurrency,
		PerKeyConcurrency: 4,
		KeyCooldown:       time.Minute,
		Timeout:           opts.timeout,
		Retries:           2,
		Preprocess:        vision.DefaultPreprocessConfig(),
		AnalyzeTimeout:    opts.timeout,
	})
	h := &harness{opts: opts, rec: rec, client: &http.Client{Timeout: opts.timeout}}

	ctx := context.Background()
	rep := report{
		GeneratedAt: time.Now().Format(time.RFC3339),
		Config: configInfo{
			Samples:      opts.samples,
			BaseURL:      opts.baseURL,
			Model:        opts.model,
			McpBaseURL:   opts.mcpBaseURL,
			McpModel:     opts.mcpModel,
			JudgeEnabled: opts.judgeURL != "" && opts.judgeKey != "",
			JudgeModel:   opts.judgeModel,
		},
	}
	for _, img := range images {
		rep.Results = append(rep.Results, h.runImage(ctx, img))
	}
	rep.Aggregate = summarize(rep.Results)

	if err := writeReport(opts.report, rep); err != nil {
		fmt.Fprintln(os.Stderr, "error writing report:", err)
		os.Exit(1)
	}
	fmt.Println("wrote report to", opts.report)
	printTable(rep.Results, rep.Aggregate)
}

func parseFlags() options {
	var o options
	flag.StringVar(&o.samples, "samples", "scripts/vision-compare/samples", "directory of test images")
	flag.StringVar(&o.baseURL, "base-url", "", "server recognizer upstream base URL (OpenAI-compatible /chat/completions)")
	flag.StringVar(&o.keys, "keys", "", "comma-separated server recognizer API keys")
	flag.StringVar(&o.model, "model", "", "server recognizer model")
	flag.StringVar(&o.mcpBaseURL, "mcp-base-url", "", "MCP image_analysis model endpoint base URL (Anthropic /v1/messages)")
	flag.StringVar(&o.mcpKey, "mcp-key", "", "MCP API key")
	flag.StringVar(&o.mcpModel, "mcp-model", "xopkimik26", "MCP model id")
	flag.StringVar(&o.report, "report", "vision-compare-report.json", "output JSON report path")
	flag.StringVar(&o.question, "question", defaultQuestion, "default question for samples without a sidecar <base>.txt")
	flag.DurationVar(&o.timeout, "timeout", 90*time.Second, "per-call HTTP timeout")
	flag.IntVar(&o.concurrency, "concurrency", 8, "server recognizer max concurrency")
	flag.StringVar(&o.judgeURL, "judge-url", "", "optional LLM judge OpenAI-compatible base URL (empty = stubbed, judge_winner=pending)")
	flag.StringVar(&o.judgeKey, "judge-key", "", "optional LLM judge API key")
	flag.StringVar(&o.judgeModel, "judge-model", "", "optional LLM judge model")
	flag.Parse()
	return o
}

func (o options) validate() error {
	var missing []string
	for _, f := range []struct {
		name, val string
	}{
		{"-samples", o.samples},
		{"-base-url", o.baseURL},
		{"-keys", o.keys},
		{"-model", o.model},
		{"-mcp-base-url", o.mcpBaseURL},
		{"-mcp-key", o.mcpKey},
		{"-report", o.report},
	} {
		if f.val == "" {
			missing = append(missing, f.name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required flag(s): %s", strings.Join(missing, ", "))
	}
	return nil
}

func splitKeys(raw string) []string {
	var keys []string
	for _, k := range strings.Split(raw, ",") {
		if k = strings.TrimSpace(k); k != "" {
			keys = append(keys, k)
		}
	}
	return keys
}

func listImages(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var imgs []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if imageExts[strings.ToLower(filepath.Ext(e.Name()))] {
			imgs = append(imgs, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(imgs)
	return imgs, nil
}

func (h *harness) runImage(ctx context.Context, path string) imageReport {
	base := filepath.Base(path)
	question := h.questionFor(path)
	data, err := os.ReadFile(path)
	ir := imageReport{
		Image:       base,
		Question:    question,
		SizeBytes:   int64(len(data)),
		JudgeWinner: "skip",
	}
	if err != nil {
		msg := "read file: " + err.Error()
		ir.ServerError, ir.McpError = msg, msg
		return ir
	}

	// Channel 1: server pipeline.
	srvResp, srvErr := h.rec.Analyze(ctx, vision.AnalyzeRequest{
		SessionKey: vision.SessionKey("vision-compare-" + base),
		Query:      question,
		ImageData:  base64.StdEncoding.EncodeToString(data),
		MIMEType:   "image/png",
		SourceKind: vision.ImageSourceUserUpload,
	})
	if srvErr != nil {
		ir.ServerError = srvErr.Error()
	} else {
		ir.ServerOK = true
		ir.ServerSummary = srvResp.Summary.Summary
		ir.ServerOCR = append([]string(nil), srvResp.Summary.OCRHints...)
	}

	// Channel 2: client MCP image_analysis (direct model endpoint call).
	mcpText, mcpErr := h.mcpAnalyze(ctx, data, question)
	if mcpErr != nil {
		ir.McpError = mcpErr.Error()
	} else {
		ir.McpOK = true
		ir.McpSummary = mcpText
		ir.McpOCR, ir.McpOCRFallback = extractOCR(mcpText)
	}

	if ir.ServerOK && ir.McpOK {
		ir.OCRF1 = ocrF1(ir.ServerOCR, ir.McpOCR)
		ir.JudgeWinner = h.judge(ir.ServerSummary, ir.McpSummary)
	}
	return ir
}

// questionFor returns the per-image question from a sidecar <base>.txt file
// next to the image, or the -question default when no sidecar exists.
func (h *harness) questionFor(imgPath string) string {
	sidecar := strings.TrimSuffix(imgPath, filepath.Ext(imgPath)) + ".txt"
	data, err := os.ReadFile(sidecar)
	if err != nil {
		return h.opts.question
	}
	if q := strings.TrimSpace(string(data)); q != "" {
		return q
	}
	return h.opts.question
}

// mcpAnalyze emulates the MCP image_analysis tool by calling the MCP model
// endpoint directly. It mirrors the MCP's ImageProcessor preprocessing
// (resize inside 2048, no enlargement, JPEG q80) and its Anthropic Messages
// request shape.
func (h *harness) mcpAnalyze(ctx context.Context, data []byte, question string) (string, error) {
	proc, err := vision.PreprocessImage(data, vision.PreprocessModeStandard, vision.DefaultPreprocessConfig())
	if err != nil {
		return "", fmt.Errorf("mcp preprocess: %w", err)
	}

	body := map[string]any{
		"model":      h.opts.mcpModel,
		"max_tokens": 4096,
		"system":     mcpSystemPrompt,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "image", "source": map[string]any{"type": "base64", "media_type": proc.MediaType, "data": proc.Base64}},
					{"type": "text", "text": question},
				},
			},
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("mcp marshal: %w", err)
	}

	url := strings.TrimRight(h.opts.mcpBaseURL, "/") + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("mcp request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("x-api-key", h.opts.mcpKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	resp, err := h.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("mcp call: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("mcp read: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("mcp API status %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}

	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("mcp parse: %w", err)
	}
	var sb strings.Builder
	for _, c := range parsed.Content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	if sb.Len() == 0 {
		return "", fmt.Errorf("mcp response has no text content")
	}
	return sb.String(), nil
}

// judge asks an optional LLM (OpenAI-compatible /chat/completions) which of the
// two summaries is more complete/accurate. When no -judge-* flags are given it
// is stubbed to "pending" — both summaries are still written to the report so a
// human/LLM can judge them offline.
func (h *harness) judge(serverSummary, mcpSummary string) string {
	if h.opts.judgeURL == "" || h.opts.judgeKey == "" {
		return "pending"
	}
	prompt := fmt.Sprintf(`以下是同一张图片的两份图像分析摘要。请判断哪一份更完整、更准确、更贴近图片的真实内容。

--- 摘要 A（服务器识别管线）---
%s

--- 摘要 B（客户端 MCP image_analysis）---
%s

只回答一个词：A 或 B 或 TIE。`, serverSummary, mcpSummary)

	body := map[string]any{
		"model":      h.opts.judgeModel,
		"messages":   []map[string]any{{"role": "user", "content": prompt}},
		"max_tokens": 16,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "pending"
	}
	url := strings.TrimRight(h.opts.judgeURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "pending"
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+h.opts.judgeKey)

	resp, err := h.client.Do(httpReq)
	if err != nil {
		return "pending"
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil || len(parsed.Choices) == 0 {
		return "pending"
	}
	ans := strings.ToUpper(strings.TrimSpace(parsed.Choices[0].Message.Content))
	switch {
	case strings.HasPrefix(ans, "A"):
		return "server"
	case strings.HasPrefix(ans, "B"):
		return "mcp"
	case strings.Contains(ans, "TIE") || strings.Contains(ans, "平"):
		return "tie"
	}
	return "unknown"
}

// extractOCR pulls OCR fragments out of a free-form analysis response and
// reports whether it had to fall back to the whole text.
//
// When the response contains an OCR-marked section (lines under an
// OCR/文字/文本/TEXT header), ONLY those lines are returned (fallback=false) so
// summary prose does not pollute the OCR token set. When no such section is
// found, the whole text is treated as the OCR candidate (fallback=true) — that
// is an upper-bound heuristic, since prose tokens inflate the MCP side and
// depress ocr_f1 regardless of how completely the server OCR is contained.
func extractOCR(text string) ([]string, bool) {
	var ocr []string
	inOCR := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if inOCR {
				break // OCR section ends at a blank line
			}
			continue
		}
		upper := strings.ToUpper(trimmed)
		if isOCRSectionHeader(upper) {
			inOCR = true
			if v := afterColon(trimmed); v != "" {
				ocr = append(ocr, v)
			}
			continue
		}
		if !inOCR {
			continue // skip prose before any OCR header
		}
		if isOtherSectionHeader(upper) {
			break // reached SUMMARY/LAYOUT/DETAIL etc after the OCR section
		}
		if len(ocr) > 0 {
			ocr[len(ocr)-1] += " " + trimmed
		} else {
			ocr = append(ocr, trimmed)
		}
	}
	if inOCR {
		return ocr, false
	}
	return []string{text}, true
}

func isOCRSectionHeader(upper string) bool {
	return strings.HasPrefix(upper, "OCR") || strings.HasPrefix(upper, "TEXT") ||
		strings.HasPrefix(upper, "文字") || strings.HasPrefix(upper, "文本")
}

func isOtherSectionHeader(upper string) bool {
	return strings.HasPrefix(upper, "SUMMARY") || strings.HasPrefix(upper, "LAYOUT") ||
		strings.HasPrefix(upper, "DETAIL") || strings.HasPrefix(upper, "总结") ||
		strings.HasPrefix(upper, "描述")
}

func afterColon(line string) string {
	for i, r := range line {
		if r == ':' || r == '：' {
			return strings.TrimSpace(line[i+len(string(r)):])
		}
	}
	return ""
}

// ocrF1 returns token-level set F1 between two OCR fragment lists. Tokens are
// normalized (lowercase, punctuation stripped) and compared as a word set, per
// the spec's "词集合 F1".
func ocrF1(server, mcp []string) float64 {
	s := tokenSet(server)
	m := tokenSet(mcp)
	if len(s) == 0 && len(m) == 0 {
		return 1.0
	}
	if len(s) == 0 || len(m) == 0 {
		return 0.0
	}
	inter := 0
	for t := range s {
		if _, ok := m[t]; ok {
			inter++
		}
	}
	if inter == 0 {
		return 0.0
	}
	prec := float64(inter) / float64(len(m))
	rec := float64(inter) / float64(len(s))
	return 2 * prec * rec / (prec + rec)
}

func tokenSet(fragments []string) map[string]struct{} {
	toks := map[string]struct{}{}
	for _, f := range fragments {
		for _, tok := range tokenize(f) {
			toks[tok] = struct{}{}
		}
	}
	return toks
}

func tokenize(s string) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

func summarize(results []imageReport) aggregate {
	a := aggregate{ImagesTotal: len(results)}
	var f1Sum float64
	for _, r := range results {
		if r.ServerOK {
			a.ImagesServerOK++
		}
		if r.McpOK {
			a.ImagesMcpOK++
		}
		if r.ServerOK && r.McpOK {
			a.ImagesBothOK++
			f1Sum += r.OCRF1
		}
		if r.McpOCRFallback {
			a.McpOCRFallbackCount++
		}
		switch r.JudgeWinner {
		case "server":
			a.JudgeServerWins++
		case "mcp":
			a.JudgeMcpWins++
		case "tie":
			a.JudgeTies++
		default:
			a.JudgePending++
		}
	}
	if a.ImagesBothOK > 0 {
		a.AvgOCRF1 = f1Sum / float64(a.ImagesBothOK)
	}
	a.BothOKPassRate = float64(a.ImagesBothOK) / float64(a.ImagesTotal)
	judged := a.JudgeServerWins + a.JudgeMcpWins + a.JudgeTies
	if judged > 0 {
		a.LLMJudgePassRate = float64(a.JudgeServerWins) / float64(judged)
	}
	return a
}

func writeReport(path string, rep report) error {
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func printTable(results []imageReport, agg aggregate) {
	fmt.Println()
	fmt.Printf("%-40s %8s %8s %8s %10s\n", "IMAGE", "OCR-F1", "SERVER", "MCP", "JUDGE")
	fmt.Println(strings.Repeat("-", 82))
	for _, r := range results {
		fmt.Printf("%-40s %8.3f %8t %8t %10s\n", r.Image, r.OCRF1, r.ServerOK, r.McpOK, r.JudgeWinner)
	}
	fmt.Println(strings.Repeat("-", 82))
	fmt.Printf("%-40s %8s %8s %8s %10s\n", "TOTAL", "", fmt.Sprintf("%d/%d", agg.ImagesServerOK, agg.ImagesTotal), fmt.Sprintf("%d/%d", agg.ImagesMcpOK, agg.ImagesTotal), "")
	fmt.Printf("avg OCR-F1: %.3f   both-ok pass rate: %.0f%%   LLM-judge server win rate: %.0f%% (server=%d mcp=%d tie=%d pending=%d)  mcp-ocr-fallback=%d\n",
		agg.AvgOCRF1, agg.BothOKPassRate*100, agg.LLMJudgePassRate*100,
		agg.JudgeServerWins, agg.JudgeMcpWins, agg.JudgeTies, agg.JudgePending, agg.McpOCRFallbackCount)
	if agg.McpOCRFallbackCount > 0 {
		fmt.Printf("note: %d image(s) used whole-text MCP OCR fallback — avg OCR-F1 is an upper-bound heuristic; use the human/LLM judge as the gate for the spec §8 OCR-accuracy line.\n",
			agg.McpOCRFallbackCount)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
