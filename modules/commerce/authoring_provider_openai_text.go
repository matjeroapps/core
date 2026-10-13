package commerce

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type openAITextProvider struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

func NewOpenAITextProvider(baseURL, apiKey, model string, timeout time.Duration) TextGenerator {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &openAITextProvider{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, model: model, client: &http.Client{Timeout: timeout}}
}

func (p *openAITextProvider) GenerateProductText(ctx context.Context, req TextRequest) (TextResult, error) {
	payload := map[string]any{
		"model": p.model,
		"messages": []map[string]string{
			{"role": "system", "content": "Return a JSON object with a fields array containing product name and description only. For each field, provide both Arabic (locale ar) and English (locale en). Each item must contain field_key, locale, and value. Use only field_key name or description. Do not include keywords, categories, attributes, price, inventory, sku, origin country, or specifications."},
			{"role": "user", "content": req.Brief},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return TextResult{}, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		result, err := p.doGenerate(ctx, body)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if failureCode(err) != ProviderFailureUnavailable {
			break
		}
	}
	return TextResult{}, lastErr
}

func (p *openAITextProvider) doGenerate(ctx context.Context, body []byte) (TextResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return TextResult{}, ProviderFailureError{Code: ProviderFailureUnavailable}
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return TextResult{}, ProviderFailureError{Code: ProviderFailureUnavailable}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return TextResult{}, ProviderFailureError{Code: ProviderFailureUnavailable}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return TextResult{}, ProviderFailureError{Code: ProviderFailureRefused}
	}
	var decoded struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil || len(decoded.Choices) == 0 {
		return TextResult{}, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	return parseTextProviderContent(decoded.Choices[0].Message.Content)
}

func parseTextProviderContent(content string) (TextResult, error) {
	var raw json.RawMessage
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return TextResult{}, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	var fields []TextFieldResult
	switch {
	case len(raw) > 0 && raw[0] == '[':
		// Some compatible chat models return the proposal array directly even
		// when prompted for a JSON object. Accept that equivalent envelope.
		if err := json.Unmarshal(raw, &fields); err != nil {
			return TextResult{}, ProviderFailureError{Code: ProviderFailureInvalidOutput}
		}
	default:
		var payload struct {
			Fields []TextFieldResult `json:"fields"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return TextResult{}, ProviderFailureError{Code: ProviderFailureInvalidOutput}
		}
		fields = payload.Fields
	}
	if len(fields) == 0 {
		return TextResult{}, ProviderFailureError{Code: ProviderFailureInvalidOutput}
	}
	for _, field := range fields {
		if !IsValidFieldKey(field.FieldKey) || !IsValidLocale(field.Locale) {
			return TextResult{}, fmt.Errorf("%w", ProviderFailureError{Code: AuthoringUnsupportedField})
		}
	}
	return TextResult{Fields: fields}, nil
}

func failureCode(err error) string {
	if e, ok := err.(ProviderFailureError); ok {
		return e.Code
	}
	return ProviderFailureUnavailable
}
