package providers

import (
	"errors"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

var ErrItemNotFound = errors.New("item not found")

type Validator func() error

type Service struct {
	cfg      *config.Config
	validate Validator
}

func NewService(cfg *config.Config, validate Validator) *Service {
	return &Service{cfg: cfg, validate: validate}
}

type OpenAICompatibilityPatch struct {
	Name          *string                             `json:"name"`
	Disabled      *bool                               `json:"disabled"`
	Prefix        *string                             `json:"prefix"`
	BaseURL       *string                             `json:"base-url"`
	APIKeyEntries *[]config.OpenAICompatibilityAPIKey `json:"api-key-entries"`
	Models        *[]config.OpenAICompatibilityModel  `json:"models"`
	Headers       *map[string]string                  `json:"headers"`
}

func (s *Service) OpenAICompatibility() []config.OpenAICompatibility {
	if s == nil || s.cfg == nil {
		return nil
	}
	return NormalizedOpenAICompatibilityEntries(s.cfg.OpenAICompatibility)
}

func (s *Service) ReplaceOpenAICompatibility(entries []config.OpenAICompatibility) error {
	if s == nil || s.cfg == nil {
		return nil
	}
	filtered := make([]config.OpenAICompatibility, 0, len(entries))
	for i := range entries {
		NormalizeOpenAICompatibilityEntry(&entries[i])
		if strings.TrimSpace(entries[i].BaseURL) != "" {
			filtered = append(filtered, entries[i])
		}
	}
	prev := append([]config.OpenAICompatibility(nil), s.cfg.OpenAICompatibility...)
	s.cfg.OpenAICompatibility = filtered
	s.cfg.SanitizeOpenAICompatibility()
	if err := s.runValidator(); err != nil {
		s.cfg.OpenAICompatibility = prev
		return err
	}
	return nil
}

func (s *Service) PatchOpenAICompatibility(index *int, name *string, patch OpenAICompatibilityPatch) error {
	if s == nil || s.cfg == nil {
		return ErrItemNotFound
	}
	targetIndex := -1
	if index != nil && *index >= 0 && *index < len(s.cfg.OpenAICompatibility) {
		targetIndex = *index
	}
	if targetIndex == -1 && name != nil {
		match := strings.TrimSpace(*name)
		for i := range s.cfg.OpenAICompatibility {
			if s.cfg.OpenAICompatibility[i].Name == match {
				targetIndex = i
				break
			}
		}
	}
	if targetIndex == -1 {
		return ErrItemNotFound
	}

	entry := s.cfg.OpenAICompatibility[targetIndex]
	if patch.Name != nil {
		entry.Name = strings.TrimSpace(*patch.Name)
	}
	if patch.Disabled != nil {
		entry.Disabled = *patch.Disabled
	}
	if patch.Prefix != nil {
		entry.Prefix = strings.TrimSpace(*patch.Prefix)
	}
	if patch.BaseURL != nil {
		trimmed := strings.TrimSpace(*patch.BaseURL)
		if trimmed == "" {
			s.cfg.OpenAICompatibility = append(s.cfg.OpenAICompatibility[:targetIndex], s.cfg.OpenAICompatibility[targetIndex+1:]...)
			s.cfg.SanitizeOpenAICompatibility()
			return nil
		}
		entry.BaseURL = trimmed
	}
	if patch.APIKeyEntries != nil {
		entry.APIKeyEntries = append([]config.OpenAICompatibilityAPIKey(nil), (*patch.APIKeyEntries)...)
	}
	if patch.Models != nil {
		entry.Models = append([]config.OpenAICompatibilityModel(nil), (*patch.Models)...)
	}
	if patch.Headers != nil {
		entry.Headers = config.NormalizeHeaders(*patch.Headers)
	}
	NormalizeOpenAICompatibilityEntry(&entry)
	prev := append([]config.OpenAICompatibility(nil), s.cfg.OpenAICompatibility...)
	s.cfg.OpenAICompatibility[targetIndex] = entry
	s.cfg.SanitizeOpenAICompatibility()
	if err := s.runValidator(); err != nil {
		s.cfg.OpenAICompatibility = prev
		return err
	}
	return nil
}

func (s *Service) DeleteOpenAICompatibilityByName(name string) {
	if s == nil || s.cfg == nil {
		return
	}
	out := make([]config.OpenAICompatibility, 0, len(s.cfg.OpenAICompatibility))
	for _, entry := range s.cfg.OpenAICompatibility {
		if entry.Name != name {
			out = append(out, entry)
		}
	}
	s.cfg.OpenAICompatibility = out
	s.cfg.SanitizeOpenAICompatibility()
}

func (s *Service) DeleteOpenAICompatibilityByIndex(index int) bool {
	if s == nil || s.cfg == nil || index < 0 || index >= len(s.cfg.OpenAICompatibility) {
		return false
	}
	s.cfg.OpenAICompatibility = append(s.cfg.OpenAICompatibility[:index], s.cfg.OpenAICompatibility[index+1:]...)
	s.cfg.SanitizeOpenAICompatibility()
	return true
}

type VertexCompatPatch struct {
	APIKey   *string                     `json:"api-key"`
	Prefix   *string                     `json:"prefix"`
	BaseURL  *string                     `json:"base-url"`
	ProxyURL *string                     `json:"proxy-url"`
	ProxyID  *string                     `json:"proxy-id"`
	Headers  *map[string]string          `json:"headers"`
	Models   *[]config.VertexCompatModel `json:"models"`
}

type GeminiKeyPatch struct {
	APIKey         *string            `json:"api-key"`
	Prefix         *string            `json:"prefix"`
	BaseURL        *string            `json:"base-url"`
	ProxyURL       *string            `json:"proxy-url"`
	ProxyID        *string            `json:"proxy-id"`
	Headers        *map[string]string `json:"headers"`
	ExcludedModels *[]string          `json:"excluded-models"`
}

type ClaudeKeyPatch struct {
	Name           *string               `json:"name"`
	APIKey         *string               `json:"api-key"`
	Prefix         *string               `json:"prefix"`
	BaseURL        *string               `json:"base-url"`
	ProxyURL       *string               `json:"proxy-url"`
	ProxyID        *string               `json:"proxy-id"`
	Models         *[]config.ClaudeModel `json:"models"`
	Headers        *map[string]string    `json:"headers"`
	ExcludedModels *[]string             `json:"excluded-models"`
}

type CodexKeyPatch struct {
	APIKey         *string              `json:"api-key"`
	Prefix         *string              `json:"prefix"`
	BaseURL        *string              `json:"base-url"`
	ProxyURL       *string              `json:"proxy-url"`
	ProxyID        *string              `json:"proxy-id"`
	Models         *[]config.CodexModel `json:"models"`
	Headers        *map[string]string   `json:"headers"`
	ExcludedModels *[]string            `json:"excluded-models"`
}

func (s *Service) GeminiKeys() []config.GeminiKey {
	if s == nil || s.cfg == nil {
		return nil
	}
	return s.cfg.GeminiKey
}

func (s *Service) ReplaceGeminiKeys(entries []config.GeminiKey) error {
	if s == nil || s.cfg == nil {
		return nil
	}
	prev := append([]config.GeminiKey(nil), s.cfg.GeminiKey...)
	s.cfg.GeminiKey = append([]config.GeminiKey(nil), entries...)
	s.cfg.SanitizeGeminiKeys()
	if err := s.runValidator(); err != nil {
		s.cfg.GeminiKey = prev
		return err
	}
	return nil
}

func (s *Service) PatchGeminiKey(index *int, match *string, patch GeminiKeyPatch) error {
	if s == nil || s.cfg == nil {
		return ErrItemNotFound
	}
	targetIndex := -1
	if index != nil && *index >= 0 && *index < len(s.cfg.GeminiKey) {
		targetIndex = *index
	}
	if targetIndex == -1 && match != nil {
		matchValue := strings.TrimSpace(*match)
		if matchValue != "" {
			for i := range s.cfg.GeminiKey {
				if s.cfg.GeminiKey[i].APIKey == matchValue {
					targetIndex = i
					break
				}
			}
		}
	}
	if targetIndex == -1 {
		return ErrItemNotFound
	}

	entry := s.cfg.GeminiKey[targetIndex]
	if patch.APIKey != nil {
		trimmed := strings.TrimSpace(*patch.APIKey)
		if trimmed == "" {
			s.deleteGeminiKeyByIndex(targetIndex)
			return nil
		}
		entry.APIKey = trimmed
	}
	if patch.Prefix != nil {
		entry.Prefix = strings.TrimSpace(*patch.Prefix)
	}
	if patch.BaseURL != nil {
		entry.BaseURL = strings.TrimSpace(*patch.BaseURL)
	}
	if patch.ProxyURL != nil {
		entry.ProxyURL = strings.TrimSpace(*patch.ProxyURL)
	}
	if patch.ProxyID != nil {
		entry.ProxyID = strings.TrimSpace(*patch.ProxyID)
	}
	if patch.Headers != nil {
		entry.Headers = config.NormalizeHeaders(*patch.Headers)
	}
	if patch.ExcludedModels != nil {
		entry.ExcludedModels = config.NormalizeExcludedModels(*patch.ExcludedModels)
	}
	prev := append([]config.GeminiKey(nil), s.cfg.GeminiKey...)
	s.cfg.GeminiKey[targetIndex] = entry
	s.cfg.SanitizeGeminiKeys()
	if err := s.runValidator(); err != nil {
		s.cfg.GeminiKey = prev
		return err
	}
	return nil
}

func (s *Service) DeleteGeminiKeyByAPIKey(apiKey string) bool {
	if s == nil || s.cfg == nil {
		return false
	}
	out := make([]config.GeminiKey, 0, len(s.cfg.GeminiKey))
	for _, entry := range s.cfg.GeminiKey {
		if entry.APIKey != apiKey {
			out = append(out, entry)
		}
	}
	if len(out) == len(s.cfg.GeminiKey) {
		return false
	}
	s.cfg.GeminiKey = out
	s.cfg.SanitizeGeminiKeys()
	return true
}

func (s *Service) DeleteGeminiKeyByIndex(index int) bool {
	if s == nil || s.cfg == nil || index < 0 || index >= len(s.cfg.GeminiKey) {
		return false
	}
	s.deleteGeminiKeyByIndex(index)
	return true
}

func (s *Service) deleteGeminiKeyByIndex(index int) {
	s.cfg.GeminiKey = append(s.cfg.GeminiKey[:index], s.cfg.GeminiKey[index+1:]...)
	s.cfg.SanitizeGeminiKeys()
}

func (s *Service) ClaudeKeys() []config.ClaudeKey {
	if s == nil || s.cfg == nil {
		return nil
	}
	return s.cfg.ClaudeKey
}

func (s *Service) ReplaceClaudeKeys(entries []config.ClaudeKey) error {
	if s == nil || s.cfg == nil {
		return nil
	}
	normalized := append([]config.ClaudeKey(nil), entries...)
	for i := range normalized {
		NormalizeClaudeKey(&normalized[i])
	}
	prev := append([]config.ClaudeKey(nil), s.cfg.ClaudeKey...)
	s.cfg.ClaudeKey = normalized
	s.cfg.SanitizeClaudeKeys()
	if err := s.runValidator(); err != nil {
		s.cfg.ClaudeKey = prev
		return err
	}
	return nil
}

func (s *Service) PatchClaudeKey(index *int, match *string, patch ClaudeKeyPatch) error {
	if s == nil || s.cfg == nil {
		return ErrItemNotFound
	}
	targetIndex := -1
	if index != nil && *index >= 0 && *index < len(s.cfg.ClaudeKey) {
		targetIndex = *index
	}
	if targetIndex == -1 && match != nil {
		matchValue := strings.TrimSpace(*match)
		for i := range s.cfg.ClaudeKey {
			if s.cfg.ClaudeKey[i].APIKey == matchValue {
				targetIndex = i
				break
			}
		}
	}
	if targetIndex == -1 {
		return ErrItemNotFound
	}

	entry := s.cfg.ClaudeKey[targetIndex]
	if patch.Name != nil {
		entry.Name = strings.TrimSpace(*patch.Name)
	}
	if patch.APIKey != nil {
		entry.APIKey = strings.TrimSpace(*patch.APIKey)
	}
	if patch.Prefix != nil {
		entry.Prefix = strings.TrimSpace(*patch.Prefix)
	}
	if patch.BaseURL != nil {
		entry.BaseURL = strings.TrimSpace(*patch.BaseURL)
	}
	if patch.ProxyURL != nil {
		entry.ProxyURL = strings.TrimSpace(*patch.ProxyURL)
	}
	if patch.ProxyID != nil {
		entry.ProxyID = strings.TrimSpace(*patch.ProxyID)
	}
	if patch.Models != nil {
		entry.Models = append([]config.ClaudeModel(nil), (*patch.Models)...)
	}
	if patch.Headers != nil {
		entry.Headers = config.NormalizeHeaders(*patch.Headers)
	}
	if patch.ExcludedModels != nil {
		entry.ExcludedModels = config.NormalizeExcludedModels(*patch.ExcludedModels)
	}
	NormalizeClaudeKey(&entry)
	prev := append([]config.ClaudeKey(nil), s.cfg.ClaudeKey...)
	s.cfg.ClaudeKey[targetIndex] = entry
	s.cfg.SanitizeClaudeKeys()
	if err := s.runValidator(); err != nil {
		s.cfg.ClaudeKey = prev
		return err
	}
	return nil
}

func (s *Service) DeleteClaudeKeyByAPIKey(apiKey string) {
	if s == nil || s.cfg == nil {
		return
	}
	out := make([]config.ClaudeKey, 0, len(s.cfg.ClaudeKey))
	for _, entry := range s.cfg.ClaudeKey {
		if entry.APIKey != apiKey {
			out = append(out, entry)
		}
	}
	s.cfg.ClaudeKey = out
	s.cfg.SanitizeClaudeKeys()
}

func (s *Service) DeleteClaudeKeyByIndex(index int) bool {
	if s == nil || s.cfg == nil || index < 0 || index >= len(s.cfg.ClaudeKey) {
		return false
	}
	s.cfg.ClaudeKey = append(s.cfg.ClaudeKey[:index], s.cfg.ClaudeKey[index+1:]...)
	s.cfg.SanitizeClaudeKeys()
	return true
}

func (s *Service) CodexKeys() []config.CodexKey {
	if s == nil || s.cfg == nil {
		return nil
	}
	return s.cfg.CodexKey
}

func (s *Service) ReplaceCodexKeys(entries []config.CodexKey) error {
	if s == nil || s.cfg == nil {
		return nil
	}
	filtered := make([]config.CodexKey, 0, len(entries))
	for i := range entries {
		entry := entries[i]
		NormalizeCodexKey(&entry)
		if entry.BaseURL == "" {
			continue
		}
		filtered = append(filtered, entry)
	}
	prev := append([]config.CodexKey(nil), s.cfg.CodexKey...)
	s.cfg.CodexKey = filtered
	s.cfg.SanitizeCodexKeys()
	if err := s.runValidator(); err != nil {
		s.cfg.CodexKey = prev
		return err
	}
	return nil
}

func (s *Service) PatchCodexKey(index *int, match *string, patch CodexKeyPatch) error {
	if s == nil || s.cfg == nil {
		return ErrItemNotFound
	}
	targetIndex := -1
	if index != nil && *index >= 0 && *index < len(s.cfg.CodexKey) {
		targetIndex = *index
	}
	if targetIndex == -1 && match != nil {
		matchValue := strings.TrimSpace(*match)
		for i := range s.cfg.CodexKey {
			if s.cfg.CodexKey[i].APIKey == matchValue {
				targetIndex = i
				break
			}
		}
	}
	if targetIndex == -1 {
		return ErrItemNotFound
	}

	entry := s.cfg.CodexKey[targetIndex]
	if patch.APIKey != nil {
		entry.APIKey = strings.TrimSpace(*patch.APIKey)
	}
	if patch.Prefix != nil {
		entry.Prefix = strings.TrimSpace(*patch.Prefix)
	}
	if patch.BaseURL != nil {
		trimmed := strings.TrimSpace(*patch.BaseURL)
		if trimmed == "" {
			s.deleteCodexKeyByIndex(targetIndex)
			return nil
		}
		entry.BaseURL = trimmed
	}
	if patch.ProxyURL != nil {
		entry.ProxyURL = strings.TrimSpace(*patch.ProxyURL)
	}
	if patch.ProxyID != nil {
		entry.ProxyID = strings.TrimSpace(*patch.ProxyID)
	}
	if patch.Models != nil {
		entry.Models = append([]config.CodexModel(nil), (*patch.Models)...)
	}
	if patch.Headers != nil {
		entry.Headers = config.NormalizeHeaders(*patch.Headers)
	}
	if patch.ExcludedModels != nil {
		entry.ExcludedModels = config.NormalizeExcludedModels(*patch.ExcludedModels)
	}
	NormalizeCodexKey(&entry)
	prev := append([]config.CodexKey(nil), s.cfg.CodexKey...)
	s.cfg.CodexKey[targetIndex] = entry
	s.cfg.SanitizeCodexKeys()
	if err := s.runValidator(); err != nil {
		s.cfg.CodexKey = prev
		return err
	}
	return nil
}

func (s *Service) DeleteCodexKeyByAPIKey(apiKey string) {
	if s == nil || s.cfg == nil {
		return
	}
	out := make([]config.CodexKey, 0, len(s.cfg.CodexKey))
	for _, entry := range s.cfg.CodexKey {
		if entry.APIKey != apiKey {
			out = append(out, entry)
		}
	}
	s.cfg.CodexKey = out
	s.cfg.SanitizeCodexKeys()
}

func (s *Service) DeleteCodexKeyByIndex(index int) bool {
	if s == nil || s.cfg == nil || index < 0 || index >= len(s.cfg.CodexKey) {
		return false
	}
	s.deleteCodexKeyByIndex(index)
	return true
}

func (s *Service) deleteCodexKeyByIndex(index int) {
	s.cfg.CodexKey = append(s.cfg.CodexKey[:index], s.cfg.CodexKey[index+1:]...)
	s.cfg.SanitizeCodexKeys()
}

func (s *Service) VertexCompatKeys() []config.VertexCompatKey {
	if s == nil || s.cfg == nil {
		return nil
	}
	return s.cfg.VertexCompatAPIKey
}

func (s *Service) ReplaceVertexCompatKeys(entries []config.VertexCompatKey) {
	if s == nil || s.cfg == nil {
		return
	}
	for i := range entries {
		NormalizeVertexCompatKey(&entries[i])
	}
	s.cfg.VertexCompatAPIKey = entries
	s.cfg.SanitizeVertexCompatKeys()
}

func (s *Service) PatchVertexCompatKey(index *int, match *string, patch VertexCompatPatch) error {
	if s == nil || s.cfg == nil {
		return ErrItemNotFound
	}
	targetIndex := -1
	if index != nil && *index >= 0 && *index < len(s.cfg.VertexCompatAPIKey) {
		targetIndex = *index
	}
	if targetIndex == -1 && match != nil {
		matchValue := strings.TrimSpace(*match)
		if matchValue != "" {
			for i := range s.cfg.VertexCompatAPIKey {
				if s.cfg.VertexCompatAPIKey[i].APIKey == matchValue {
					targetIndex = i
					break
				}
			}
		}
	}
	if targetIndex == -1 {
		return ErrItemNotFound
	}

	entry := s.cfg.VertexCompatAPIKey[targetIndex]
	if patch.APIKey != nil {
		trimmed := strings.TrimSpace(*patch.APIKey)
		if trimmed == "" {
			s.deleteVertexCompatKeyByIndex(targetIndex)
			return nil
		}
		entry.APIKey = trimmed
	}
	if patch.Prefix != nil {
		entry.Prefix = strings.TrimSpace(*patch.Prefix)
	}
	if patch.BaseURL != nil {
		trimmed := strings.TrimSpace(*patch.BaseURL)
		if trimmed == "" {
			s.deleteVertexCompatKeyByIndex(targetIndex)
			return nil
		}
		entry.BaseURL = trimmed
	}
	if patch.ProxyURL != nil {
		entry.ProxyURL = strings.TrimSpace(*patch.ProxyURL)
	}
	if patch.ProxyID != nil {
		entry.ProxyID = strings.TrimSpace(*patch.ProxyID)
	}
	if patch.Headers != nil {
		entry.Headers = config.NormalizeHeaders(*patch.Headers)
	}
	if patch.Models != nil {
		entry.Models = append([]config.VertexCompatModel(nil), (*patch.Models)...)
	}
	NormalizeVertexCompatKey(&entry)
	s.cfg.VertexCompatAPIKey[targetIndex] = entry
	s.cfg.SanitizeVertexCompatKeys()
	return nil
}

func (s *Service) DeleteVertexCompatKeyByAPIKey(apiKey string) {
	if s == nil || s.cfg == nil {
		return
	}
	out := make([]config.VertexCompatKey, 0, len(s.cfg.VertexCompatAPIKey))
	for _, entry := range s.cfg.VertexCompatAPIKey {
		if entry.APIKey != apiKey {
			out = append(out, entry)
		}
	}
	s.cfg.VertexCompatAPIKey = out
	s.cfg.SanitizeVertexCompatKeys()
}

func (s *Service) DeleteVertexCompatKeyByIndex(index int) bool {
	if s == nil || s.cfg == nil || index < 0 || index >= len(s.cfg.VertexCompatAPIKey) {
		return false
	}
	s.deleteVertexCompatKeyByIndex(index)
	return true
}

func (s *Service) deleteVertexCompatKeyByIndex(index int) {
	s.cfg.VertexCompatAPIKey = append(s.cfg.VertexCompatAPIKey[:index], s.cfg.VertexCompatAPIKey[index+1:]...)
	s.cfg.SanitizeVertexCompatKeys()
}

func (s *Service) runValidator() error {
	if s == nil || s.validate == nil {
		return nil
	}
	return s.validate()
}

func NormalizeOpenAICompatibilityEntry(entry *config.OpenAICompatibility) {
	if entry == nil {
		return
	}
	entry.BaseURL = strings.TrimSpace(entry.BaseURL)
	entry.Headers = config.NormalizeHeaders(entry.Headers)
	for i := range entry.APIKeyEntries {
		entry.APIKeyEntries[i].APIKey = strings.TrimSpace(entry.APIKeyEntries[i].APIKey)
		entry.APIKeyEntries[i].ProxyURL = strings.TrimSpace(entry.APIKeyEntries[i].ProxyURL)
		entry.APIKeyEntries[i].ProxyID = strings.TrimSpace(entry.APIKeyEntries[i].ProxyID)
	}
}

func NormalizedOpenAICompatibilityEntries(entries []config.OpenAICompatibility) []config.OpenAICompatibility {
	if len(entries) == 0 {
		return nil
	}
	out := make([]config.OpenAICompatibility, len(entries))
	for i := range entries {
		copyEntry := entries[i]
		if len(copyEntry.APIKeyEntries) > 0 {
			copyEntry.APIKeyEntries = append([]config.OpenAICompatibilityAPIKey(nil), copyEntry.APIKeyEntries...)
		}
		NormalizeOpenAICompatibilityEntry(&copyEntry)
		out[i] = copyEntry
	}
	return out
}

func NormalizeVertexCompatKey(entry *config.VertexCompatKey) {
	if entry == nil {
		return
	}
	entry.APIKey = strings.TrimSpace(entry.APIKey)
	entry.Prefix = strings.TrimSpace(entry.Prefix)
	entry.BaseURL = strings.TrimSpace(entry.BaseURL)
	entry.ProxyURL = strings.TrimSpace(entry.ProxyURL)
	entry.ProxyID = strings.TrimSpace(entry.ProxyID)
	entry.Headers = config.NormalizeHeaders(entry.Headers)
	if len(entry.Models) == 0 {
		return
	}
	normalized := make([]config.VertexCompatModel, 0, len(entry.Models))
	for i := range entry.Models {
		model := entry.Models[i]
		model.Name = strings.TrimSpace(model.Name)
		model.Alias = strings.TrimSpace(model.Alias)
		if model.Name == "" || model.Alias == "" {
			continue
		}
		normalized = append(normalized, model)
	}
	entry.Models = normalized
}

func NormalizeClaudeKey(entry *config.ClaudeKey) {
	if entry == nil {
		return
	}
	entry.Name = strings.TrimSpace(entry.Name)
	entry.APIKey = strings.TrimSpace(entry.APIKey)
	entry.BaseURL = strings.TrimSpace(entry.BaseURL)
	entry.ProxyURL = strings.TrimSpace(entry.ProxyURL)
	entry.ProxyID = strings.TrimSpace(entry.ProxyID)
	entry.Headers = config.NormalizeHeaders(entry.Headers)
	entry.ExcludedModels = config.NormalizeExcludedModels(entry.ExcludedModels)
	if len(entry.Models) == 0 {
		return
	}
	normalized := make([]config.ClaudeModel, 0, len(entry.Models))
	for i := range entry.Models {
		model := entry.Models[i]
		model.Name = strings.TrimSpace(model.Name)
		model.Alias = strings.TrimSpace(model.Alias)
		if model.Name == "" && model.Alias == "" {
			continue
		}
		normalized = append(normalized, model)
	}
	entry.Models = normalized
}

func NormalizeCodexKey(entry *config.CodexKey) {
	if entry == nil {
		return
	}
	entry.APIKey = strings.TrimSpace(entry.APIKey)
	entry.Prefix = strings.TrimSpace(entry.Prefix)
	entry.BaseURL = strings.TrimSpace(entry.BaseURL)
	entry.ProxyURL = strings.TrimSpace(entry.ProxyURL)
	entry.ProxyID = strings.TrimSpace(entry.ProxyID)
	entry.Headers = config.NormalizeHeaders(entry.Headers)
	entry.ExcludedModels = config.NormalizeExcludedModels(entry.ExcludedModels)
	if len(entry.Models) == 0 {
		return
	}
	normalized := make([]config.CodexModel, 0, len(entry.Models))
	for i := range entry.Models {
		model := entry.Models[i]
		model.Name = strings.TrimSpace(model.Name)
		model.Alias = strings.TrimSpace(model.Alias)
		if model.Name == "" && model.Alias == "" {
			continue
		}
		normalized = append(normalized, model)
	}
	entry.Models = normalized
}
