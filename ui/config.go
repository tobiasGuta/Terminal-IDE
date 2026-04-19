package ui

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

type appConfig struct {
	OpenAIKey        string
	GeminiKey        string
	AnthropicKey     string
	AIPreferredModel string
}

func loadAppConfig() appConfig {
	cfg := appConfig{}
	for _, path := range candidateConfigPaths() {
		mergeConfigFile(&cfg, path)
	}
	mergeConfigEnv(&cfg)
	return cfg
}

func candidateConfigPaths() []string {
	var paths []string
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		paths = append(paths, filepath.Join(home, ".config", "terminal-ide", "config.toml"))
	}
	if wd, err := os.Getwd(); err == nil && wd != "" {
		paths = append(paths, filepath.Join(wd, ".terminal-ide.toml"))
	}
	return paths
}

func mergeConfigFile(cfg *appConfig, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(strings.ToLower(key))
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		switch key {
		case "openai_api_key":
			cfg.OpenAIKey = value
		case "gemini_api_key":
			cfg.GeminiKey = value
		case "anthropic_api_key":
			cfg.AnthropicKey = value
		case "ai_model", "preferred_ai_model":
			cfg.AIPreferredModel = value
		}
	}
}

func mergeConfigEnv(cfg *appConfig) {
	if value := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); value != "" {
		cfg.OpenAIKey = value
	}
	if value := strings.TrimSpace(os.Getenv("GEMINI_API_KEY")); value != "" {
		cfg.GeminiKey = value
	}
	if value := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")); value != "" {
		cfg.AnthropicKey = value
	}
	if value := strings.TrimSpace(os.Getenv("TERMINAL_IDE_AI_MODEL")); value != "" {
		cfg.AIPreferredModel = value
	}
}
