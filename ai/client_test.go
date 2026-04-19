package ai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCallOpenAIReturnsHTTPStatusError(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return newResponse(req, http.StatusTooManyRequests, ""), nil
	})}
	_, err := callOpenAI(context.Background(), client, "test-key", "gpt-4o-mini", "sys", "user", nil)
	if err == nil {
		t.Fatalf("expected error")
	}

	var statusErr ErrHTTPStatus
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected ErrHTTPStatus, got %T", err)
	}
	if statusErr.Provider != "openai" || statusErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("unexpected status error: %+v", statusErr)
	}
}

func TestCallOpenAIStreamsContent(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := "data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n" +
			"data: {\"choices\":[{\"delta\":{\"content\":\" world\"}}]}\n\n" +
			"data: [DONE]\n\n"
		return newResponse(req, http.StatusOK, body), nil
	})}
	var chunks []string
	content, err := callOpenAI(context.Background(), client, "test-key", "gpt-4o-mini", "sys", "user", func(chunk string) error {
		chunks = append(chunks, chunk)
		return nil
	})
	if err != nil {
		t.Fatalf("callOpenAI() error = %v", err)
	}
	if content != "Hello world" {
		t.Fatalf("callOpenAI() content = %q, want %q", content, "Hello world")
	}
	if strings.Join(chunks, "") != "Hello world" {
		t.Fatalf("streamed chunks = %q", strings.Join(chunks, ""))
	}
}

func TestCallAnthropicReturnsHTTPStatusError(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return newResponse(req, http.StatusUnauthorized, ""), nil
	})}
	_, err := callAnthropic(context.Background(), client, "test-key", "claude-3-5-sonnet-latest", "sys", "user", nil)
	if err == nil {
		t.Fatalf("expected error")
	}

	var statusErr ErrHTTPStatus
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected ErrHTTPStatus, got %T", err)
	}
	if statusErr.Provider != "anthropic" || statusErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unexpected status error: %+v", statusErr)
	}
}

func TestClientGlobalRateLimit(t *testing.T) {
	client := &Client{cooldown: 50 * time.Millisecond}

	if err := client.claimRateLimit(); err != nil {
		t.Fatalf("first claimRateLimit() error = %v", err)
	}
	if err := client.claimRateLimit(); err == nil {
		t.Fatalf("expected rate limit error")
	} else {
		var rateErr ErrRateLimited
		if !errors.As(err, &rateErr) {
			t.Fatalf("expected ErrRateLimited, got %T", err)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func newResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}
