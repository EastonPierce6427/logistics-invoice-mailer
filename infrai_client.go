package invoicemailer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type InfraiClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func NewInfraiClient(baseURL, apiKey string, client *http.Client) *InfraiClient {
	return &InfraiClient{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, http: client}
}

type PDFGenerateRequest struct {
	HTML           string `json:"html"`
	PageSize       string `json:"page_size"`
	Orientation    string `json:"orientation"`
	Store          bool   `json:"store"`
	IdempotencyKey string `json:"idempotency_key"`
}

type GeneratedPDF struct {
	URL string `json:"url"`
}

type EmailSendRequest struct {
	To             string `json:"to"`
	Subject        string `json:"subject"`
	HTML           string `json:"html"`
	IdempotencyKey string `json:"idempotency_key"`
}

type SentEmail struct {
	MessageID string `json:"message_id"`
}

type envelope[T any] struct {
	OK       bool            `json:"ok"`
	Data     T               `json:"data"`
	Error    *envelopeError  `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type InfraiError struct {
	Code    string
	Message string
	Status  int
}

func (e *InfraiError) Error() string {
	return fmt.Sprintf("infrai request rejected: %s: %s", e.Code, e.Message)
}

func (c *InfraiClient) GeneratePDF(ctx context.Context, input PDFGenerateRequest, idempotencyKey string) (GeneratedPDF, error) {
	input.IdempotencyKey = idempotencyKey
	return post[PDFGenerateRequest, GeneratedPDF](ctx, c, "/v1/pdf/generate", input, idempotencyKey)
}

func (c *InfraiClient) SendEmail(ctx context.Context, input EmailSendRequest, idempotencyKey string) (SentEmail, error) {
	input.IdempotencyKey = idempotencyKey
	return post[EmailSendRequest, SentEmail](ctx, c, "/v1/email/send", input, idempotencyKey)
}

func post[In, Out any](ctx context.Context, client *InfraiClient, path string, input In, idempotencyKey string) (Out, error) {
	var zero Out
	payload, err := json.Marshal(input)
	if err != nil {
		return zero, err
	}

	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, client.baseURL+path, bytes.NewReader(payload))
		if err != nil {
			return zero, err
		}
		req.Header.Set("Authorization", "Bearer "+client.apiKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", idempotencyKey)

		res, err := client.http.Do(req)
		if err != nil {
			return zero, err
		}
		body, readErr := io.ReadAll(io.LimitReader(res.Body, 2<<20))
		res.Body.Close()
		if readErr != nil {
			return zero, readErr
		}

		var env envelope[Out]
		decodeErr := json.Unmarshal(body, &env)
		if res.StatusCode == http.StatusTooManyRequests {
			time.Sleep(retryDelay(res.Header.Get("Retry-After"), attempt))
			continue
		}
		if decodeErr != nil {
			return zero, fmt.Errorf("decode Infrai envelope: %w", decodeErr)
		}
		if !env.OK && env.Error != nil {
			return zero, &InfraiError{Code: env.Error.Code, Message: env.Error.Message, Status: res.StatusCode}
		}
		if res.StatusCode >= 500 {
			return zero, fmt.Errorf("Infrai transport status %d", res.StatusCode)
		}
		if !env.OK {
			return zero, &InfraiError{Code: "REQUEST_REJECTED", Message: "request was rejected", Status: res.StatusCode}
		}
		return env.Data, nil
	}
	return zero, fmt.Errorf("Infrai rate limit retry budget exhausted")
}

func retryDelay(retryAfter string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(retryAfter); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(1<<attempt) * 250 * time.Millisecond
}
