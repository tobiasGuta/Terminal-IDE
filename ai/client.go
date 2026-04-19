package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	openAIChatCompletionsURL  = "https://api.openai.com/v1/chat/completions"
	anthropicMessagesURL      = "https://api.anthropic.com/v1/messages"
	defaultOpenAIModel        = "gpt-4o-mini"
	defaultGeminiModel        = "gemini-2.5-flash"
	defaultAnthropicModel     = "claude-3-5-sonnet-latest"
	requestTimeout            = 15 * time.Second
	maxGeminiAttempts         = 3
	defaultGlobalRateCooldown = 10 * time.Second
)

var geminiRetryBackoffs = [...]time.Duration{2 * time.Second, 4 * time.Second}

const (
	defaultExplainSystemPrompt = "You are a friendly programming tutor helping a beginner student. When given a Python error traceback and the student's code, explain what went wrong in plain English in 2-3 sentences maximum. Be warm and encouraging. Point to the specific line number if possible. Suggest one concrete fix. Never show corrected code - only describe what to change."
	defaultHintSystemPrompt    = "You are a Socratic computer science professor. You must NEVER write code or give the exact solution. Based on the hint level (1 = very vague, 2 = somewhat specific, 3 = point directly at the problem), give one short hint of 1-2 sentences maximum that guides the student toward the answer. Hint level: %d."
)

var ErrParseResponse = errors.New("ai response could not be parsed")

type ErrHTTPStatus struct {
	Provider   string
	StatusCode int
}

func (e ErrHTTPStatus) Error() string {
	return fmt.Sprintf("%s returned HTTP %d", e.Provider, e.StatusCode)
}

type ErrRateLimited struct {
	RetryAfter time.Duration
}

func (e ErrRateLimited) Error() string {
	return fmt.Sprintf("AI cooldown active for %s", e.RetryAfter.Round(time.Second))
}

type PromptTemplates struct {
	Explain string `json:"explain"`
	Hint    string `json:"hint"`
}

type Client struct {
	openaiKey    string
	geminiKey    string
	anthropicKey string
	provider     string
	model        string
	httpClient   *http.Client
	prompts      PromptTemplates

	rateMu      sync.Mutex
	nextAllowed time.Time
	cooldown    time.Duration
}

func NewClient(openaiKey, geminiKey, anthropicKey, preferredModel string) *Client {
	c := &Client{
		openaiKey:    strings.TrimSpace(openaiKey),
		geminiKey:    strings.TrimSpace(geminiKey),
		anthropicKey: strings.TrimSpace(anthropicKey),
		httpClient:   &http.Client{},
		prompts:      loadPromptTemplates(),
		cooldown:     globalRateCooldown(),
	}

	provider, model := c.selectProviderAndModel(strings.TrimSpace(preferredModel))
	c.provider = provider
	c.model = model
	return c
}

func (c *Client) Disabled() bool {
	return c == nil || c.provider == "" || c.model == ""
}

func (c *Client) Provider() string {
	if c == nil {
		return ""
	}
	return c.provider
}

func (c *Client) Model() string {
	if c == nil {
		return ""
	}
	return c.model
}

func (c *Client) SetModel(provider, model string) {
	if c == nil {
		return
	}

	provider = strings.TrimSpace(strings.ToLower(provider))
	model = strings.TrimSpace(model)

	switch provider {
	case "gemini":
		if c.geminiKey == "" {
			return
		}
		if model == "" {
			model = defaultGeminiModel
		}
	case "openai":
		if c.openaiKey == "" {
			return
		}
		if model == "" {
			model = defaultOpenAIModel
		}
	case "anthropic":
		if c.anthropicKey == "" {
			return
		}
		if model == "" {
			model = defaultAnthropicModel
		}
	default:
		return
	}

	c.provider = provider
	c.model = model
}

func (c *Client) ExplainError(ctx context.Context, code, traceback string, currentLine int, onChunk func(string) error) (string, error) {
	if c.Disabled() {
		return "", nil
	}

	userPrompt := fmt.Sprintf(
		"Python traceback:\n%s\n\nRelevant code window:\n%s",
		strings.TrimSpace(traceback),
		buildCodeWindow(code, firstNonZero(extractLineNumber(traceback), currentLine), 20),
	)
	return c.sendPrompt(ctx, c.prompts.Explain, userPrompt, onChunk)
}

func (c *Client) GetHint(ctx context.Context, code, currentError string, currentLine int, hintLevel int, onChunk func(string) error) (string, error) {
	if c.Disabled() {
		return "", nil
	}

	systemPrompt := fmt.Sprintf(c.prompts.Hint, hintLevel)
	userPrompt := fmt.Sprintf(
		"Current execution line: %d\n\nCurrent error:\n%s\n\nRelevant code window:\n%s",
		currentLine,
		strings.TrimSpace(currentError),
		buildCodeWindow(code, currentLine, 20),
	)
	return c.sendPrompt(ctx, systemPrompt, userPrompt, onChunk)
}

func (c *Client) sendPrompt(ctx context.Context, systemPrompt, userPrompt string, onChunk func(string) error) (string, error) {
	if c.Disabled() {
		return "", nil
	}
	if err := c.claimRateLimit(); err != nil {
		return "", err
	}

	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	switch c.provider {
	case "gemini":
		return callGemini(reqCtx, c.httpClient, c.geminiKey, c.model, systemPrompt, userPrompt, onChunk)
	case "openai":
		return callOpenAI(reqCtx, c.httpClient, c.openaiKey, c.model, systemPrompt, userPrompt, onChunk)
	case "anthropic":
		return callAnthropic(reqCtx, c.httpClient, c.anthropicKey, c.model, systemPrompt, userPrompt, onChunk)
	default:
		return "", nil
	}
}

func (c *Client) claimRateLimit() error {
	if c == nil || c.cooldown <= 0 {
		return nil
	}
	c.rateMu.Lock()
	defer c.rateMu.Unlock()
	now := time.Now()
	if now.Before(c.nextAllowed) {
		return ErrRateLimited{RetryAfter: c.nextAllowed.Sub(now)}
	}
	c.nextAllowed = now.Add(c.cooldown)
	return nil
}

func (c *Client) selectProviderAndModel(preferredModel string) (string, string) {
	model := strings.TrimSpace(preferredModel)
	lowerModel := strings.ToLower(model)

	switch {
	case strings.Contains(lowerModel, "claude") && c.anthropicKey != "":
		if model == "" {
			model = defaultAnthropicModel
		}
		return "anthropic", model
	case strings.Contains(lowerModel, "gemini") && c.geminiKey != "":
		if model == "" {
			model = defaultGeminiModel
		}
		return "gemini", model
	case strings.Contains(lowerModel, "gpt") && c.openaiKey != "":
		if model == "" {
			model = defaultOpenAIModel
		}
		return "openai", model
	case model == "" && c.geminiKey != "":
		return "gemini", defaultGeminiModel
	case model == "" && c.openaiKey != "":
		return "openai", defaultOpenAIModel
	case model == "" && c.anthropicKey != "":
		return "anthropic", defaultAnthropicModel
	default:
		return "", ""
	}
}

func callOpenAI(ctx context.Context, httpClient *http.Client, apiKey, model, systemPrompt, userPrompt string, onChunk func(string) error) (string, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	payload := openAIChatCompletionRequest{
		Model:  model,
		Stream: true,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openAIChatCompletionsURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", ErrHTTPStatus{Provider: "openai", StatusCode: resp.StatusCode}
	}

	var out strings.Builder
	err = consumeSSE(resp.Body, func(data string) error {
		if data == "[DONE]" {
			return nil
		}
		var parsed openAIChatCompletionStreamResponse
		if err := json.Unmarshal([]byte(data), &parsed); err != nil {
			return nil
		}
		if len(parsed.Choices) == 0 {
			return nil
		}
		chunk := parsed.Choices[0].Delta.Content
		if chunk == "" {
			return nil
		}
		out.WriteString(chunk)
		if onChunk != nil {
			return onChunk(chunk)
		}
		return nil
	})
	if err != nil {
		return "", err
	}

	content := strings.TrimSpace(out.String())
	if content == "" {
		return "", ErrParseResponse
	}
	return content, nil
}

func callGemini(ctx context.Context, httpClient *http.Client, apiKey, model, systemPrompt, userMessage string, onChunk func(string) error) (string, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	endpoint := fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/models/%s:streamGenerateContent?alt=sse&key=%s",
		url.PathEscape(model),
		url.QueryEscape(apiKey),
	)

	payload := geminiGenerateContentRequest{
		SystemInstruction: geminiInstruction{
			Parts: []geminiPart{{Text: systemPrompt}},
		},
		Contents: []geminiContent{
			{
				Role:  "user",
				Parts: []geminiPart{{Text: userMessage}},
			},
		},
		GenerationConfig: geminiGenerationConfig{
			MaxOutputTokens: 300,
			Temperature:     0.4,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	var lastErr error
	for attempt := 0; attempt < maxGeminiAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		if err != nil {
			return "", err
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
			lastErr = ErrHTTPStatus{Provider: "gemini", StatusCode: resp.StatusCode}
			resp.Body.Close()
			if attempt < len(geminiRetryBackoffs) {
				timer := time.NewTimer(geminiRetryBackoffs[attempt])
				select {
				case <-ctx.Done():
					timer.Stop()
					return "", ctx.Err()
				case <-timer.C:
				}
				continue
			}
			return "", lastErr
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return "", ErrHTTPStatus{Provider: "gemini", StatusCode: resp.StatusCode}
		}

		var out strings.Builder
		err = consumeSSE(resp.Body, func(data string) error {
			var parsed geminiGenerateContentResponse
			if err := json.Unmarshal([]byte(data), &parsed); err != nil {
				return nil
			}
			chunk := parsed.Text()
			if chunk == "" {
				return nil
			}
			out.WriteString(chunk)
			if onChunk != nil {
				return onChunk(chunk)
			}
			return nil
		})
		resp.Body.Close()
		if err != nil {
			return "", err
		}

		content := strings.TrimSpace(out.String())
		if content == "" {
			return "", ErrParseResponse
		}
		return content, nil
	}

	if lastErr != nil {
		return "", lastErr
	}
	return "", ErrHTTPStatus{Provider: "gemini", StatusCode: http.StatusServiceUnavailable}
}

func callAnthropic(ctx context.Context, httpClient *http.Client, apiKey, model, systemPrompt, userPrompt string, onChunk func(string) error) (string, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	payload := anthropicMessagesRequest{
		Model:     model,
		System:    systemPrompt,
		MaxTokens: 300,
		Stream:    true,
		Messages: []chatMessage{
			{Role: "user", Content: userPrompt},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicMessagesURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", ErrHTTPStatus{Provider: "anthropic", StatusCode: resp.StatusCode}
	}

	var out strings.Builder
	err = consumeSSE(resp.Body, func(data string) error {
		var parsed anthropicStreamEvent
		if err := json.Unmarshal([]byte(data), &parsed); err != nil {
			return nil
		}
		if parsed.Type != "content_block_delta" {
			return nil
		}
		chunk := parsed.Delta.Text
		if chunk == "" {
			return nil
		}
		out.WriteString(chunk)
		if onChunk != nil {
			return onChunk(chunk)
		}
		return nil
	})
	if err != nil {
		return "", err
	}

	content := strings.TrimSpace(out.String())
	if content == "" {
		return "", ErrParseResponse
	}
	return content, nil
}

func consumeSSE(body io.Reader, onData func(string) error) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 4096), 1024*1024)

	var eventLines []string
	flush := func() error {
		if len(eventLines) == 0 {
			return nil
		}
		data := strings.Join(eventLines, "\n")
		eventLines = nil
		if onData != nil {
			return onData(data)
		}
		return nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			eventLines = append(eventLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return flush()
}

func buildCodeWindow(code string, centerLine, radius int) string {
	lines := strings.Split(strings.ReplaceAll(code, "\r\n", "\n"), "\n")
	if len(lines) == 0 {
		return ""
	}
	if centerLine <= 0 {
		centerLine = 1
	}
	if radius < 0 {
		radius = 0
	}
	start := max(1, centerLine-radius)
	end := min(len(lines), centerLine+radius)
	var b strings.Builder
	for i := start; i <= end; i++ {
		fmt.Fprintf(&b, "%4d | %s\n", i, lines[i-1])
	}
	return strings.TrimRight(b.String(), "\n")
}

func extractLineNumber(traceback string) int {
	for _, line := range strings.Split(traceback, "\n") {
		idx := strings.Index(line, "line ")
		if idx == -1 {
			continue
		}
		rest := line[idx+5:]
		var digits strings.Builder
		for _, r := range rest {
			if r < '0' || r > '9' {
				break
			}
			digits.WriteRune(r)
		}
		if digits.Len() == 0 {
			continue
		}
		value, err := strconv.Atoi(digits.String())
		if err == nil && value > 0 {
			return value
		}
	}
	return 0
}

func firstNonZero(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func loadPromptTemplates() PromptTemplates {
	templates := PromptTemplates{
		Explain: defaultExplainSystemPrompt,
		Hint:    defaultHintSystemPrompt,
	}

	if path := strings.TrimSpace(os.Getenv("TERMINAL_IDE_AI_PROMPTS_FILE")); path != "" {
		data, err := os.ReadFile(path)
		if err == nil {
			var fileTemplates PromptTemplates
			if json.Unmarshal(data, &fileTemplates) == nil {
				if strings.TrimSpace(fileTemplates.Explain) != "" {
					templates.Explain = strings.TrimSpace(fileTemplates.Explain)
				}
				if strings.TrimSpace(fileTemplates.Hint) != "" {
					templates.Hint = strings.TrimSpace(fileTemplates.Hint)
				}
			}
		}
	}

	if value := strings.TrimSpace(os.Getenv("TERMINAL_IDE_EXPLAIN_SYSTEM_PROMPT")); value != "" {
		templates.Explain = value
	}
	if value := strings.TrimSpace(os.Getenv("TERMINAL_IDE_HINT_SYSTEM_PROMPT")); value != "" {
		templates.Hint = value
	}
	return templates
}

func globalRateCooldown() time.Duration {
	raw := strings.TrimSpace(os.Getenv("TERMINAL_IDE_AI_RATE_LIMIT"))
	if raw == "" {
		return defaultGlobalRateCooldown
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value < 0 {
		return defaultGlobalRateCooldown
	}
	return value
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatCompletionRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

type openAIChatCompletionStreamResponse struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

type geminiGenerateContentRequest struct {
	SystemInstruction geminiInstruction      `json:"system_instruction"`
	Contents          []geminiContent        `json:"contents"`
	GenerationConfig  geminiGenerationConfig `json:"generationConfig"`
}

type geminiInstruction struct {
	Parts []geminiPart `json:"parts"`
}

type geminiContent struct {
	Role  string       `json:"role"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGenerationConfig struct {
	MaxOutputTokens int     `json:"maxOutputTokens"`
	Temperature     float64 `json:"temperature"`
}

type geminiGenerateContentResponse struct {
	Candidates []struct {
		Content struct {
			Parts []geminiPart `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

func (r geminiGenerateContentResponse) Text() string {
	var b strings.Builder
	for _, candidate := range r.Candidates {
		for _, part := range candidate.Content.Parts {
			b.WriteString(part.Text)
		}
	}
	return b.String()
}

type anthropicMessagesRequest struct {
	Model     string        `json:"model"`
	System    string        `json:"system,omitempty"`
	MaxTokens int           `json:"max_tokens"`
	Stream    bool          `json:"stream"`
	Messages  []chatMessage `json:"messages"`
}

type anthropicStreamEvent struct {
	Type  string `json:"type"`
	Delta struct {
		Text string `json:"text"`
	} `json:"delta"`
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
