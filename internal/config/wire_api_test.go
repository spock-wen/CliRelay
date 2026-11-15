package config

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOpenAICompatWireAPIRoundTrip(t *testing.T) {
	for _, value := range []string{"", "responses", "chat", " Responses "} {
		input := `{"name":"gpt","base-url":"http://10.20.23.251:8317/v1","wire-api":"` + value + `"}`
		var entry OpenAICompatibility
		if err := json.Unmarshal([]byte(input), &entry); err != nil {
			t.Fatal(err)
		}
		want := NormalizeOpenAICompatWireAPI(value)
		sanitized := Config{OpenAICompatibility: []OpenAICompatibility{entry}}
		sanitized.SanitizeOpenAICompatibility()
		if got := sanitized.OpenAICompatibility[0].WireAPI; got != want {
			t.Fatalf("sanitize %q = %q, want %q", value, got, want)
		}
		encoded, err := yaml.Marshal(sanitized.OpenAICompatibility[0])
		if err != nil {
			t.Fatal(err)
		}
		if want == "" && strings.Contains(string(encoded), "wire-api:") {
			t.Fatalf("empty wire-api was written: %s", encoded)
		}
		if want == OpenAICompatWireAPIResponses && !strings.Contains(string(encoded), "wire-api: responses") {
			t.Fatalf("responses wire-api was lost: %s", encoded)
		}
	}
}
