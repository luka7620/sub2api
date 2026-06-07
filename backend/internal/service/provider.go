package service

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
)

const (
	ProviderGrok2API     = domain.ProviderGrok2API
	ProviderWindsurfPool = domain.ProviderWindsurfPool
	ProviderKiroGo       = domain.ProviderKiroGo
)

const accountProviderExtraKey = "provider"

type UpstreamProvider struct {
	Key             string
	DisplayName     string
	DefaultPlatform string
	Platforms       []string
}

var upstreamProviders = map[string]UpstreamProvider{
	ProviderGrok2API: {
		Key:             ProviderGrok2API,
		DisplayName:     "Grok2API",
		DefaultPlatform: PlatformGrok2API,
		Platforms:       []string{PlatformGrok2API, PlatformOpenAI},
	},
	ProviderWindsurfPool: {
		Key:             ProviderWindsurfPool,
		DisplayName:     "WindsurfPoolAPI",
		DefaultPlatform: PlatformWindsurf,
		Platforms:       []string{PlatformWindsurf, PlatformOpenAI, PlatformAnthropic},
	},
	ProviderKiroGo: {
		Key:             ProviderKiroGo,
		DisplayName:     "Kiro-Go",
		DefaultPlatform: PlatformKiro,
		Platforms:       []string{PlatformKiro, PlatformAnthropic, PlatformOpenAI},
	},
}

func IsSupportedPlatform(platform string) bool {
	switch platform {
	case PlatformAnthropic, PlatformOpenAI, PlatformGemini, PlatformAntigravity, PlatformGrok2API, PlatformWindsurf, PlatformKiro:
		return true
	default:
		return false
	}
}

func PlatformProtocol(platform string) string {
	switch platform {
	case PlatformGrok2API, PlatformWindsurf:
		return PlatformOpenAI
	case PlatformKiro:
		return PlatformAnthropic
	default:
		return platform
	}
}

func IsOpenAIProtocolPlatform(platform string) bool {
	return PlatformProtocol(platform) == PlatformOpenAI
}

func IsAnthropicProtocolPlatform(platform string) bool {
	return PlatformProtocol(platform) == PlatformAnthropic
}

// UsesNativeMessagesGateway reports whether /v1/messages should be handled by
// the native Anthropic-compatible gateway instead of the OpenAI compatibility
// gateway. Some providers, such as grok2api, expose OpenAI-compatible endpoints
// for /responses and /chat/completions but also provide a native /v1/messages
// endpoint that should not be converted to OpenAI Responses.
func UsesNativeMessagesGateway(platform string) bool {
	switch platform {
	case PlatformGrok2API:
		return true
	default:
		return IsAnthropicProtocolPlatform(platform)
	}
}

func ProviderForVisiblePlatform(platform string) string {
	switch platform {
	case PlatformGrok2API:
		return ProviderGrok2API
	case PlatformWindsurf:
		return ProviderWindsurfPool
	case PlatformKiro:
		return ProviderKiroGo
	default:
		return ""
	}
}

func NormalizeProvider(provider string) string {
	key := strings.ToLower(strings.TrimSpace(provider))
	key = strings.ReplaceAll(key, "_", "-")
	switch key {
	case "", "default", "official":
		return ""
	case "grok2api", "grok-2-api", "grok2-api":
		return ProviderGrok2API
	case "windsurf", "windsurfpool", "windsurfpoolapi", "windsurf-pool", "windsurf-pool-api":
		return ProviderWindsurfPool
	case "kiro", "kirogo", "kiro-go":
		return ProviderKiroGo
	default:
		return key
	}
}

func ProviderForPlatform(provider, platform string) string {
	provider = NormalizeProvider(provider)
	if provider == "" {
		return ""
	}
	meta, ok := upstreamProviders[provider]
	if !ok || len(meta.Platforms) == 0 {
		return provider
	}
	for _, allowed := range meta.Platforms {
		if allowed == platform {
			return provider
		}
	}
	return ""
}

func NormalizeProviderForPlatform(provider, platform string) (string, bool) {
	provider = NormalizeProvider(provider)
	if visibleProvider := ProviderForVisiblePlatform(platform); visibleProvider != "" {
		if provider == "" || provider == visibleProvider {
			return visibleProvider, true
		}
		return provider, false
	}
	if provider == "" {
		return "", true
	}
	provider = ProviderForPlatform(provider, platform)
	return provider, provider != ""
}

func NormalizeAccountProvider(provider, platform, accountType string) (string, bool) {
	provider = NormalizeProvider(provider)
	if provider == "" {
		return "", true
	}
	if accountType != AccountTypeAPIKey && accountType != AccountTypeUpstream {
		return "", false
	}
	return NormalizeProviderForPlatform(provider, platform)
}

func DefaultProviderPlatform(provider string) string {
	provider = NormalizeProvider(provider)
	if meta, ok := upstreamProviders[provider]; ok {
		return meta.DefaultPlatform
	}
	return ""
}

func ProviderFromExtra(extra map[string]any) string {
	if len(extra) == 0 {
		return ""
	}
	if raw, ok := extra[accountProviderExtraKey]; ok {
		if provider, ok := raw.(string); ok {
			return NormalizeProvider(provider)
		}
	}
	return ""
}

func SetProviderInExtra(extra map[string]any, provider string) map[string]any {
	if extra == nil {
		extra = map[string]any{}
	}
	provider = NormalizeProvider(provider)
	if provider == "" {
		delete(extra, accountProviderExtraKey)
		return extra
	}
	extra[accountProviderExtraKey] = provider
	return extra
}

func cloneMapAny(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
