package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	openAIChatCompletionsURL = "https://api.openai.com/v1/chat/completions"
	defaultOpenAIModel       = "gpt-4o-mini"
	defaultGeminiModel       = "gemini-2.5-flash"
	requestTimeout           = 15 * time.Second
	maxGeminiRetries         = 3
)

const explainSystemPrompt = "You are a friendly programming tutor helping a beginner student. When given a Python error traceback and the student's code, explain what went wrong in plain English in 2-3 sentences maximum. Be warm and encouraging. Point to the specific line number if possible. Suggest one concrete fix. Never show corrected code - only describe what to change."

var ErrParseResponse = errors.New("ai response could not be parsed")

type ErrHTTPStatus struct {
	Provider   string
	StatusCode int
}

func (e ErrHTTPStatus) Error() string {
	return fmt.Sprintf("%s returned HTTP %d", e.Provider, e.StatusCode)
}

type Client struct {
	openaiKey  string
	geminiKey  string
	provider   string
	model      string
	httpClient *http.Client
}

func NewClient(openaiKey, geminiKey, preferredModel string) *Client {
	c := &Client{
		openaiKey:  strings.TrimSpace(openaiKey),
		geminiKey:  strings.TrimSpace(geminiKey),
		httpClient: &http.Client{},
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
	default:
		return
	}

	c.provider = provider
	c.model = model
}

func (c *Client) ExplainError(ctx context.Context, code, traceback string) (string, error) {
	if c.Disabled() {
		return "", nil
	}

	userPrompt := fmt.Sprintf("Python traceback:\n%s\n\nStudent code:\n%s", strings.TrimSpace(traceback), strings.TrimSpace(code))
	return c.sendPrompt(ctx, explainSystemPrompt, userPrompt)
}

func (c *Client) GetHint(ctx context.Context, code, currentError string, currentLine int, hintLevel int) (string, error) {
	if c.Disabled() {
		return "", nil
	}

	systemPrompt := fmt.Sprintf("You are a Socratic computer science professor. You must NEVER write code or give the exact solution. Based on the hint level (1 = very vague, 2 = somewhat specific, 3 = point directly at the problem), give one short hint of 1-2 sentences maximum that guides the student toward the answer. Hint level: %d.", hintLevel)
	userPrompt := fmt.Sprintf("Current execution line: %d\n\nCurrent error:\n%s\n\nStudent code:\n%s", currentLine, strings.TrimSpace(currentError), strings.TrimSpace(code))
	return c.sendPrompt(ctx, systemPrompt, userPrompt)
}

func (c *Client) sendPrompt(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	if c.Disabled() {
		return "", nil
	}

	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	switch c.provider {
	case "gemini":
		return callGemini(reqCtx, c.geminiKey, c.model, systemPrompt, userPrompt)
	case "openai":
		return callOpenAI(reqCtx, c.openaiKey, c.model, systemPrompt, userPrompt)
	default:
		return "", nil
	}
}

func (c *Client) selectProviderAndModel(preferredModel string) (string, string) {
	model := strings.TrimSpace(preferredModel)
	lowerModel := strings.ToLower(model)

	switch {
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
	default:
		return "", ""
	}
}

func callOpenAI(ctx context.Context, apiKey, model, systemPrompt, userPrompt string) (string, error) {
	payload := openAIChatCompletionRequest{
		Model: model,
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

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", ErrHTTPStatus{Provider: "openai", StatusCode: resp.StatusCode}
	}

	var parsed openAIChatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", ErrParseResponse
	}
	if len(parsed.Choices) == 0 {
		return "", ErrParseResponse
	}

	content := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if content == "" {
		return "", ErrParseResponse
	}
	return content, nil
}

func callGemini(ctx context.Context, apiKey, model, systemPrompt, userMessage string) (string, error) {
	endpoint := fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
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
	for attempt := 0; attempt < maxGeminiRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", err
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
			lastErr = ErrHTTPStatus{Provider: "gemini", StatusCode: resp.StatusCode}
			resp.Body.Close()
			if attempt < maxGeminiRetries-1 {
				backoff := time.Duration(2<<attempt) * time.Second
				timer := time.NewTimer(backoff)
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

		var parsed geminiGenerateContentResponse
		err = json.NewDecoder(resp.Body).Decode(&parsed)
		resp.Body.Close()
		if err != nil {
			return "", ErrParseResponse
		}
		if len(parsed.Candidates) == 0 || len(parsed.Candidates[0].Content.Parts) == 0 {
			return "", ErrParseResponse
		}

		content := strings.TrimSpace(parsed.Candidates[0].Content.Parts[0].Text)
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

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatCompletionRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type openAIChatCompletionResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
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
