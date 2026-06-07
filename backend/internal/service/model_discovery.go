package service

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

const upstreamModelDiscoveryTimeout = 8 * time.Second

type AvailableModel struct {
	ID          string `json:"id"`
	Object      string `json:"object,omitempty"`
	Type        string `json:"type"`
	DisplayName string `json:"display_name,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
	OwnedBy     string `json:"owned_by,omitempty"`
}

type modelListResponse struct {
	Data   []modelListItem `json:"data"`
	Models []modelListItem `json:"models"`
}

type modelListItem struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Object       string `json:"object"`
	Type         string `json:"type"`
	DisplayName  string `json:"display_name"`
	DisplayName2 string `json:"displayName"`
	CreatedAt    string `json:"created_at"`
	CreatedAt2   string `json:"createdAt"`
	OwnedBy      string `json:"owned_by"`
}

func (s *adminServiceImpl) GetAccountAvailableModels(ctx context.Context, accountID int64) ([]AvailableModel, error) {
	account, err := s.GetAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	return s.availableModelsForAccount(ctx, account), nil
}

func (s *adminServiceImpl) availableModelsForAccount(ctx context.Context, account *Account) []AvailableModel {
	if account == nil {
		return nil
	}

	platform := account.Platform
	provider := ProviderFromExtra(account.Extra)
	if provider == "" {
		provider = ProviderForVisiblePlatform(platform)
	}
	protocol := PlatformProtocol(platform)

	mapping := account.GetModelMapping()
	if len(mapping) > 0 && (!account.IsOpenAI() || !account.IsOpenAIPassthroughEnabled()) {
		return modelsFromMapping(mapping, providerOrPlatformDefaultModels(provider, protocol))
	}

	if discovered := discoverUpstreamModels(ctx, account); len(discovered) > 0 {
		return discovered
	}

	if provider != "" {
		if models := providerDefaultModels(provider, protocol); len(models) > 0 {
			return models
		}
	}

	return platformDefaultModels(protocol)
}

func discoverUpstreamModels(ctx context.Context, account *Account) []AvailableModel {
	if account == nil || (account.Type != AccountTypeAPIKey && account.Type != AccountTypeUpstream) {
		return nil
	}
	apiKey := strings.TrimSpace(account.GetCredential("api_key"))
	baseURL := strings.TrimSpace(account.GetCredential("base_url"))
	if apiKey == "" || baseURL == "" {
		return nil
	}

	if models := fetchCompatibleModelList(ctx, PlatformProtocol(account.Platform), baseURL, apiKey); len(models) > 0 {
		return models
	}
	return nil
}

func fetchCompatibleModelList(ctx context.Context, platform, baseURL, apiKey string) []AvailableModel {
	endpoint := buildCompatibleModelsURL(baseURL)
	if endpoint == "" {
		return nil
	}
	reqCtx, cancel := context.WithTimeout(ctx, upstreamModelDiscoveryTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	if platform == PlatformAnthropic {
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	}

	client := &http.Client{Timeout: upstreamModelDiscoveryTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil
	}

	var parsed modelListResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil
	}
	return normalizeDiscoveredModels(parsed)
}

func buildCompatibleModelsURL(baseURL string) string {
	normalized := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if normalized == "" {
		return ""
	}
	if strings.HasSuffix(normalized, "/models") {
		return normalized
	}
	if strings.HasSuffix(normalized, "/v1") {
		return normalized + "/models"
	}
	return normalized + "/v1/models"
}

func normalizeDiscoveredModels(resp modelListResponse) []AvailableModel {
	items := resp.Data
	if len(items) == 0 {
		items = resp.Models
	}
	seen := make(map[string]struct{}, len(items))
	models := make([]AvailableModel, 0, len(items))
	for _, item := range items {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			id = strings.TrimSpace(item.Name)
			id = strings.TrimPrefix(id, "models/")
		}
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		displayName := strings.TrimSpace(item.DisplayName)
		if displayName == "" {
			displayName = strings.TrimSpace(item.DisplayName2)
		}
		createdAt := strings.TrimSpace(item.CreatedAt)
		if createdAt == "" {
			createdAt = strings.TrimSpace(item.CreatedAt2)
		}
		models = append(models, AvailableModel{
			ID:          id,
			Object:      defaultString(strings.TrimSpace(item.Object), "model"),
			Type:        defaultString(strings.TrimSpace(item.Type), "model"),
			DisplayName: defaultString(displayName, id),
			CreatedAt:   createdAt,
			OwnedBy:     strings.TrimSpace(item.OwnedBy),
		})
	}
	sort.SliceStable(models, func(i, j int) bool {
		return models[i].ID < models[j].ID
	})
	return models
}

func platformDefaultModels(platform string) []AvailableModel {
	switch platform {
	case PlatformOpenAI:
		out := make([]AvailableModel, 0, len(openai.DefaultModels))
		for _, m := range openai.DefaultModels {
			out = append(out, AvailableModel{ID: m.ID, Object: m.Object, Type: defaultString(m.Type, "model"), DisplayName: m.DisplayName, OwnedBy: m.OwnedBy})
		}
		return out
	case PlatformGemini:
		out := make([]AvailableModel, 0, len(geminicli.DefaultModels))
		for _, m := range geminicli.DefaultModels {
			out = append(out, AvailableModel{ID: m.ID, Type: defaultString(m.Type, "model"), DisplayName: m.DisplayName, CreatedAt: m.CreatedAt})
		}
		return out
	case PlatformAntigravity:
		return antigravityModelsToAvailable(antigravity.DefaultModels())
	default:
		out := make([]AvailableModel, 0, len(claude.DefaultModels))
		for _, m := range claude.DefaultModels {
			out = append(out, AvailableModel{ID: m.ID, Type: defaultString(m.Type, "model"), DisplayName: m.DisplayName, CreatedAt: m.CreatedAt})
		}
		return out
	}
}

func providerDefaultModels(provider, platform string) []AvailableModel {
	switch NormalizeProvider(provider) {
	case ProviderGrok2API:
		return modelIDsToAvailable([]string{
			"grok-4",
			"grok-4-fast",
			"grok-3",
			"grok-3-mini",
			"grok-2",
			"grok-2-vision",
		})
	case ProviderWindsurfPool:
		return modelIDsToAvailable([]string{
			"claude-sonnet-4-5",
			"claude-opus-4-5",
			"gemini-2.5-pro",
			"gemini-2.5-flash",
			"gpt-5.4",
			"gpt-5.4-mini",
			"gpt-5.3-codex",
		})
	case ProviderKiroGo:
		return modelIDsToAvailable([]string{
			"claude-sonnet-4-5",
			"claude-haiku-4-5",
			"claude-opus-4-5",
			"gemini-2.5-pro",
			"gemini-2.5-flash",
		})
	default:
		return platformDefaultModels(platform)
	}
}

func providerOrPlatformDefaultModels(provider, platform string) []AvailableModel {
	if provider != "" {
		if models := providerDefaultModels(provider, platform); len(models) > 0 {
			return models
		}
	}
	return platformDefaultModels(platform)
}

func modelsFromMapping(mapping map[string]string, defaults []AvailableModel) []AvailableModel {
	defaultByID := make(map[string]AvailableModel, len(defaults))
	for _, model := range defaults {
		defaultByID[model.ID] = model
	}
	ids := make([]string, 0, len(mapping))
	for id := range mapping {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out := make([]AvailableModel, 0, len(ids))
	for _, id := range ids {
		if model, ok := defaultByID[id]; ok {
			out = append(out, model)
			continue
		}
		out = append(out, AvailableModel{ID: id, Object: "model", Type: "model", DisplayName: id})
	}
	return out
}

func antigravityModelsToAvailable(in []antigravity.ClaudeModel) []AvailableModel {
	out := make([]AvailableModel, 0, len(in))
	for _, m := range in {
		out = append(out, AvailableModel{ID: m.ID, Type: defaultString(m.Type, "model"), DisplayName: m.DisplayName, CreatedAt: m.CreatedAt})
	}
	return out
}

func modelIDsToAvailable(ids []string) []AvailableModel {
	out := make([]AvailableModel, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		out = append(out, AvailableModel{ID: id, Object: "model", Type: "model", DisplayName: modelDisplayName(id)})
	}
	return out
}

func modelDisplayName(id string) string {
	parts := strings.FieldsFunc(id, func(r rune) bool {
		return r == '-' || r == '_' || r == '.'
	})
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	if len(parts) == 0 {
		return id
	}
	return strings.Join(parts, " ")
}

func defaultString(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
