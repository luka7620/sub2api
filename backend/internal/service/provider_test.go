package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUsesNativeMessagesGateway(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		want     bool
	}{
		{"anthropic", PlatformAnthropic, true},
		{"kiro", PlatformKiro, true},
		{"openai", PlatformOpenAI, false},
		{"windsurf", PlatformWindsurf, false},
		{"grok2api", PlatformGrok2API, true},
		{"unknown", "unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, UsesNativeMessagesGateway(tt.platform))
		})
	}
}
