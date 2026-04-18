package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/alecthomas/chroma"
	"github.com/alecthomas/chroma/styles"
)

type themeConfig struct {
	Background string `json:"background"`
	Keyword    string `json:"keyword"`
	String     string `json:"string"`
	Comment    string `json:"comment"`
	Function   string `json:"function"`
	Number     string `json:"number"`
	Operator   string `json:"operator"`
	Default    string `json:"default"`
}

func loadCustomTheme() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}

	path := filepath.Join(home, ".config", "termide", "theme.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	var cfg themeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return false
	}

	entries := chroma.StyleEntries{}
	if backgroundEntry := backgroundStyleEntry(cfg.Default, cfg.Background); backgroundEntry != "" {
		entries[chroma.Background] = backgroundEntry
	}
	if value := strings.TrimSpace(cfg.Keyword); value != "" {
		entries[chroma.Keyword] = value
	}
	if value := strings.TrimSpace(cfg.String); value != "" {
		entries[chroma.LiteralString] = value
	}
	if value := strings.TrimSpace(cfg.Comment); value != "" {
		entries[chroma.Comment] = value
	}
	if value := strings.TrimSpace(cfg.Function); value != "" {
		entries[chroma.NameFunction] = value
	}
	if value := strings.TrimSpace(cfg.Number); value != "" {
		entries[chroma.LiteralNumber] = value
	}
	if value := strings.TrimSpace(cfg.Operator); value != "" {
		entries[chroma.Operator] = value
	}

	styles.Register(chroma.MustNewStyle("custom", entries))
	return true
}

func backgroundStyleEntry(defaultColor, backgroundColor string) string {
	defaultColor = strings.TrimSpace(defaultColor)
	backgroundColor = strings.TrimSpace(backgroundColor)

	switch {
	case defaultColor != "" && backgroundColor != "":
		return defaultColor + " bg:" + backgroundColor
	case defaultColor != "":
		return defaultColor
	case backgroundColor != "":
		return "bg:" + backgroundColor
	default:
		return ""
	}
}

func registerBuiltInThemeAliases() {
	styles.Register(chroma.MustNewStyle("github-dark", chroma.StyleEntries{
		chroma.Background:    "#c9d1d9 bg:#0d1117",
		chroma.Keyword:       "#ff7b72",
		chroma.LiteralString: "#a5d6ff",
		chroma.Comment:       "italic #8b949e",
		chroma.NameFunction:  "#d2a8ff",
		chroma.LiteralNumber: "#79c0ff",
		chroma.Operator:      "#ff7b72",
	}))
	styles.Register(chroma.MustNewStyle("one-dark", chroma.StyleEntries{
		chroma.Background:    "#abb2bf bg:#282c34",
		chroma.Keyword:       "#c678dd",
		chroma.LiteralString: "#98c379",
		chroma.Comment:       "italic #5c6370",
		chroma.NameFunction:  "#61afef",
		chroma.LiteralNumber: "#d19a66",
		chroma.Operator:      "#56b6c2",
	}))
}

func availableEditorThemes(includeCustom bool) []themeOption {
	items := []themeOption{
		{name: "monokai"},
		{name: "dracula"},
		{name: "github-dark"},
		{name: "nord"},
		{name: "solarized-dark"},
		{name: "one-dark"},
	}
	if includeCustom {
		items = append(items, themeOption{name: "custom", detail: "(from ~/.config/termide/theme.json)"})
	}
	return items
}
