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

func TestCandidateConfigPathsIncludesHomeAndProjectFile(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	project := filepath.Join(dir, "project")
	if err := os.MkdirAll(filepath.Join(home, ".config", "terminal-ide"), 0o755); err != nil {
		t.Fatalf("MkdirAll home config failed: %v", err)
	}
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatalf("MkdirAll project failed: %v", err)
	}

	t.Setenv("HOME", home)
	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	defer func() {
		_ = os.Chdir(prevWD)
	}()
	if err := os.Chdir(project); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}

	paths := candidateConfigPaths()
	if len(paths) != 2 {
		t.Fatalf("expected 2 candidate paths, got %d: %v", len(paths), paths)
	}
	if got, want := paths[0], filepath.Join(home, ".config", "terminal-ide", "config.toml"); got != want {
		t.Fatalf("expected home config path %q, got %q", want, got)
	}
	if got, want := paths[1], filepath.Join(project, ".terminal-ide.toml"); got != want {
		t.Fatalf("expected project config path %q, got %q", want, got)
	}
}

func TestLoadAppConfigMergesHomeProjectAndEnv(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	project := filepath.Join(dir, "project")
	homeConfigDir := filepath.Join(home, ".config", "terminal-ide")
	if err := os.MkdirAll(homeConfigDir, 0o755); err != nil {
		t.Fatalf("MkdirAll home config failed: %v", err)
	}
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatalf("MkdirAll project failed: %v", err)
	}

	homeConfig := `
openai_api_key = "home-open"
gemini_api_key = "home-gem"
preferred_ai_model = "home-model"
`
	projectConfig := `
openai_api_key = "project-open"
anthropic_api_key = "project-anth"
preferred_ai_model = "project-model"
`
	if err := os.WriteFile(filepath.Join(homeConfigDir, "config.toml"), []byte(homeConfig), 0o644); err != nil {
		t.Fatalf("WriteFile home config failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project, ".terminal-ide.toml"), []byte(projectConfig), 0o644); err != nil {
		t.Fatalf("WriteFile project config failed: %v", err)
	}

	t.Setenv("HOME", home)
	t.Setenv("OPENAI_API_KEY", "env-open")
	t.Setenv("TERMINAL_IDE_AI_MODEL", "env-model")

	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	defer func() {
		_ = os.Chdir(prevWD)
	}()
	if err := os.Chdir(project); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}

	cfg := loadAppConfig()
	if cfg.OpenAIKey != "env-open" {
		t.Fatalf("expected env openai key to win, got %q", cfg.OpenAIKey)
	}
	if cfg.GeminiKey != "home-gem" {
		t.Fatalf("expected home gemini key to persist, got %q", cfg.GeminiKey)
	}
	if cfg.AnthropicKey != "project-anth" {
		t.Fatalf("expected project anthropic key, got %q", cfg.AnthropicKey)
	}
	if cfg.AIPreferredModel != "env-model" {
		t.Fatalf("expected env preferred model to win, got %q", cfg.AIPreferredModel)
	}
}
