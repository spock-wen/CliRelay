package executor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
	"github.com/tidwall/gjson"
)

func TestOpenAICompatResponsesWireAPIForwardsResponsesBody(t *testing.T) {
	var gotPath string
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","object":"response","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`))
	}))
	defer server.Close()

	payload := []byte(`{
		"model":"gpt-6.1-terra",
		"reasoning":{"effort":"max"},
		"tools":[{"type":"namespace","name":"functions","tools":[{"type":"custom","name":"exec","description":"run code"}]}],
		"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"读取项目"}]}]
	}`)
	_, err := NewOpenAICompatExecutor("gpt", &config.Config{}).Execute(context.Background(), &cliproxyauth.Auth{
		Attributes: map[string]string{
			"base_url": server.URL + "/v1",
			"api_key":  "test",
			"wire_api": config.OpenAICompatWireAPIResponses,
		},
	}, cliproxyexecutor.Request{Model: "gpt-6.1-terra", Payload: payload}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FormatOpenAIResponse,
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/responses" {
		t.Fatalf("path = %q", gotPath)
	}
	if gjson.GetBytes(gotBody, "tools.0.type").String() != "namespace" || gjson.GetBytes(gotBody, "tools.0.tools.0.name").String() != "exec" {
		t.Fatalf("tools were rewritten: %s", gotBody)
	}
	if gjson.GetBytes(gotBody, "reasoning.effort").String() != "max" {
		t.Fatalf("reasoning was stripped: %s", gotBody)
	}
	if strings.Contains(string(gotBody), "mcp__") {
		t.Fatalf("bridge tool names were injected: %s", gotBody)
	}
}

func TestOpenAICompatResponsesWireAPIKeepsChatCompletions(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_1","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer server.Close()

	_, err := NewOpenAICompatExecutor("gpt", &config.Config{}).Execute(context.Background(), &cliproxyauth.Auth{
		Attributes: map[string]string{
			"base_url": server.URL + "/v1",
			"api_key":  "test",
			"wire_api": config.OpenAICompatWireAPIResponses,
		},
	}, cliproxyexecutor.Request{
		Model:   "gpt-6.1-terra",
		Payload: []byte(`{"model":"gpt-6.1-terra","messages":[{"role":"user","content":"hi"}]}`),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai")})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("path = %q", gotPath)
	}
}

func TestOpenAICompatResponsesWireAPIStreamsEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n")
	}))
	defer server.Close()

	result, err := NewOpenAICompatExecutor("gpt", &config.Config{}).ExecuteStream(context.Background(), &cliproxyauth.Auth{
		Attributes: map[string]string{
			"base_url": server.URL + "/v1",
			"api_key":  "test",
			"wire_api": config.OpenAICompatWireAPIResponses,
		},
	}, cliproxyexecutor.Request{
		Model:   "gpt-6.1-terra",
		Payload: []byte(`{"model":"gpt-6.1-terra","stream":true,"input":"hi","tools":[{"type":"namespace","name":"functions","tools":[{"type":"custom","name":"exec"}]}]}`),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse, Stream: true})
	if err != nil {
		t.Fatal(err)
	}
	var merged strings.Builder
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatal(chunk.Err)
		}
		merged.Write(chunk.Payload)
	}
	if !strings.Contains(merged.String(), "event: response.output_text.delta") || !strings.Contains(merged.String(), `"delta":"hi"`) {
		t.Fatalf("stream = %s", merged.String())
	}
}

func TestOpenAICompatResponsesWireAPIRecordsStreamUsage(t *testing.T) {
	plugin := &tenantUsageCapturePlugin{records: make(chan coreusage.Record, 8)}
	coreusage.RegisterPlugin(plugin)

	for _, eventType := range []string{"response.completed", "response.incomplete"} {
		t.Run(eventType, func(t *testing.T) {
			model := "gpt-wire-usage-" + eventType
			usageJSON := `{"input_tokens":81029,"output_tokens":317,"total_tokens":81346,"input_tokens_details":{"cached_tokens":56192},"output_tokens_details":{"reasoning_tokens":12}}`
			body := "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n" +
				"event: " + eventType + "\ndata: {\"type\":\"" + eventType + "\",\"response\":{\"usage\":" + usageJSON + "}}\n\n"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, body)
			}))
			defer server.Close()

			result, err := NewOpenAICompatExecutor("gpt", &config.Config{}).ExecuteStream(context.Background(), &cliproxyauth.Auth{
				Attributes: map[string]string{
					"base_url": server.URL + "/v1",
					"api_key":  "test",
					"wire_api": config.OpenAICompatWireAPIResponses,
				},
			}, cliproxyexecutor.Request{
				Model:   model,
				Payload: []byte(`{"model":"` + model + `","stream":true,"input":"hi"}`),
			}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAIResponse, Stream: true})
			if err != nil {
				t.Fatal(err)
			}
			var merged strings.Builder
			for chunk := range result.Chunks {
				if chunk.Err != nil {
					t.Fatal(chunk.Err)
				}
				merged.Write(chunk.Payload)
			}
			if !strings.Contains(merged.String(), "event: "+eventType) || !strings.Contains(merged.String(), usageJSON) {
				t.Fatalf("stream was rewritten: %s", merged.String())
			}
			record, published := awaitUsageRecord(t, plugin.records, model, 2*time.Second)
			if !published {
				t.Fatal("usage record was not published")
			}
			if !record.Streaming || record.Failed {
				t.Fatalf("streaming/failed = %v/%v", record.Streaming, record.Failed)
			}
			detail := record.Detail
			if detail.InputTokens != 81029 || detail.OutputTokens != 317 || detail.TotalTokens != 81346 || detail.ReasoningTokens != 12 {
				t.Fatalf("tokens = %+v", detail)
			}
			if !detail.CacheReported || detail.CacheReadTokens != 56192 || detail.CachedTokens != 56192 || !detail.CacheReadIncludedInInput {
				t.Fatalf("cache = %+v", detail)
			}
		})
	}
}

func TestOpenAICompatResponsesWireAPIRequestsChatUsage(t *testing.T) {
	plugin := &tenantUsageCapturePlugin{records: make(chan coreusage.Record, 4)}
	coreusage.RegisterPlugin(plugin)

	const model = "gpt-chat-usage"
	var gotPath string
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n"+
			"data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"choices\":[],\"usage\":{\"prompt_tokens\":23981,\"completion_tokens\":47,\"total_tokens\":24028,\"prompt_tokens_details\":{\"cached_tokens\":22400}}}\n\n"+
			"data: [DONE]\n\n")
	}))
	defer server.Close()

	payload := []byte(`{"model":"` + model + `","stream":true,"max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`)
	result, err := NewOpenAICompatExecutor("gpt", &config.Config{}).ExecuteStream(context.Background(), &cliproxyauth.Auth{
		Attributes: map[string]string{
			"base_url": server.URL + "/v1",
			"api_key":  "test",
			"wire_api": config.OpenAICompatWireAPIResponses,
		},
	}, cliproxyexecutor.Request{Model: model, Payload: payload}, cliproxyexecutor.Options{
		OriginalRequest: payload,
		SourceFormat:    sdktranslator.FormatClaude,
		Stream:          true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatal(chunk.Err)
		}
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("path = %q", gotPath)
	}
	if !gjson.GetBytes(gotBody, "stream_options.include_usage").Bool() {
		t.Fatalf("stream_options.include_usage missing: %s", gotBody)
	}
	record, published := awaitUsageRecord(t, plugin.records, model, 2*time.Second)
	if !published {
		t.Fatal("usage record was not published")
	}
	detail := record.Detail
	if detail.InputTokens != 23981 || detail.OutputTokens != 47 || detail.TotalTokens != 24028 {
		t.Fatalf("tokens = %+v", detail)
	}
	if !detail.CacheReported || detail.CacheReadTokens != 22400 || detail.CachedTokens != 22400 || !detail.CacheReadIncludedInInput {
		t.Fatalf("cache = %+v", detail)
	}
}
