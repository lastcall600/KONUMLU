package eidsadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"backend/internal/eids/trdecision"
)

var ErrDelivery = errors.New("tr decision delivery failed")

type DeliveryClient struct {
	baseURL string
	token   string
	http    *http.Client
}

func NewDeliveryClient(baseURL, token string, timeout time.Duration) (*DeliveryClient, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || strings.TrimSpace(token) == "" {
		return nil, ErrDelivery
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &DeliveryClient{
		baseURL: baseURL,
		token:   token,
		http:    &http.Client{Timeout: timeout},
	}, nil
}

func (c *DeliveryClient) PostDecision(ctx context.Context, env trdecision.Envelope) error {
	if c == nil || c.http == nil {
		return ErrDelivery
	}
	body, err := json.Marshal(env)
	if err != nil {
		return ErrDelivery
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/internal/tr-compliance/v1/verification-decisions", bytes.NewReader(body))
	if err != nil {
		return ErrDelivery
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return ErrDelivery
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	return fmt.Errorf("%w: http %d", ErrDelivery, resp.StatusCode)
}
