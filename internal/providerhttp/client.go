package providerhttp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
)

type Response struct {
	StatusCode int
	Body       []byte
}

type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func (c *Client) Do(ctx context.Context, key string, body []byte) (Response, error) {
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return Response{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", key)
	resp, err := client.Do(req)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return Response{}, fmt.Errorf("read provider response: %w", err)
	}
	return Response{StatusCode: resp.StatusCode, Body: b}, nil
}
