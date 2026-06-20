package services

import "testing"

func TestCLIConfigEmptyDefaults(t *testing.T) {
	svc := &CliConfigService{
		relayAddr: ":18100",
		homeDir:   t.TempDir(),
	}

	codexConfig, err := svc.getCodexConfig()
	if err != nil {
		t.Fatalf("getCodexConfig failed: %v", err)
	}
	if got := codexConfig.Editable["model"]; got != "gpt-5.5" {
		t.Fatalf("codex default model = %v, want gpt-5.5", got)
	}

	geminiConfig, err := svc.getGeminiConfig()
	if err != nil {
		t.Fatalf("getGeminiConfig failed: %v", err)
	}
	if got := geminiConfig.Editable["GEMINI_MODEL"]; got != "gemini-3.1-pro-preview" {
		t.Fatalf("gemini default model = %v, want gemini-3.1-pro-preview", got)
	}
}
