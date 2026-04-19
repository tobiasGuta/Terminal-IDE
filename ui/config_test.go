package ui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMergeConfigFileParsesTomlStyleKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := `
gemini_api_key = "gem-key"
openai_api_key = "open-key"
anthropic_api_key = "anth-key"
preferred_ai_model = "claude-3-5-sonnet-latest"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var cfg appConfig
	mergeConfigFile(&cfg, path)

	if cfg.GeminiKey != "gem-key" || cfg.OpenAIKey != "open-key" || cfg.AnthropicKey != "anth-key" {
		t.Fatalf("unexpected parsed config: %+v", cfg)
	}
	if cfg.AIPreferredModel != "claude-3-5-sonnet-latest" {
		t.Fatalf("unexpected preferred model: %q", cfg.AIPreferredModel)
	}
}

func TestMergeConfigEnvOverridesFileValues(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "env-open")
	t.Setenv("GEMINI_API_KEY", "env-gem")
	t.Setenv("ANTHROPIC_API_KEY", "env-anth")
	t.Setenv("TERMINAL_IDE_AI_MODEL", "gpt-4o")

	cfg := appConfig{
		OpenAIKey:        "file-open",
		GeminiKey:        "file-gem",
		AnthropicKey:     "file-anth",
		AIPreferredModel: "file-model",
	}
	mergeConfigEnv(&cfg)

	if cfg.OpenAIKey != "env-open" || cfg.GeminiKey != "env-gem" || cfg.AnthropicKey != "env-anth" {
		t.Fatalf("env vars did not override config: %+v", cfg)
	}
	if cfg.AIPreferredModel != "gpt-4o" {
		t.Fatalf("expected env model override, got %q", cfg.AIPreferredModel)
	}
}
