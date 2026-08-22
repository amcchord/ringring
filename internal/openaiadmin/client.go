package openaiadmin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const defaultBaseURL = "https://api.openai.com/v1"

type Client struct {
	adminKey   string
	baseURL    string
	spendLimit int
	httpClient *http.Client
}

type ProvisionedProject struct {
	ProjectID        string
	ServiceAccountID string
	APIKey           string
}

func New(adminKey string, spendLimitCents int, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		adminKey:   adminKey,
		baseURL:    defaultBaseURL,
		spendLimit: spendLimitCents,
		httpClient: httpClient,
	}
}

func (c *Client) Provision(ctx context.Context, partyID, partyName string) (ProvisionedProject, error) {
	if c.adminKey == "" {
		return ProvisionedProject{}, errors.New("OpenAI admin key is not configured")
	}

	var project struct {
		ID string `json:"id"`
	}
	if err := c.post(ctx, "/organization/projects", map[string]any{
		"name":            "RingRing: " + partyName,
		"external_key_id": partyID,
	}, &project); err != nil {
		return ProvisionedProject{}, fmt.Errorf("create OpenAI project: %w", err)
	}
	if project.ID == "" {
		return ProvisionedProject{}, errors.New("create OpenAI project: response had no project ID")
	}

	if err := c.post(ctx, "/organization/projects/"+url.PathEscape(project.ID)+"/spend_limit", map[string]any{
		"threshold_amount": c.spendLimit,
		"currency":         "USD",
		"interval":         "month",
	}, nil); err != nil {
		return ProvisionedProject{}, fmt.Errorf("set OpenAI project spend limit: %w", err)
	}

	var serviceAccount struct {
		ID     string `json:"id"`
		APIKey *struct {
			Value string `json:"value"`
		} `json:"api_key"`
	}
	path := "/organization/projects/" + url.PathEscape(project.ID) + "/service_accounts"
	if err := c.post(ctx, path, map[string]any{"name": "ringring-runtime"}, &serviceAccount); err != nil {
		return ProvisionedProject{}, fmt.Errorf("create OpenAI service account: %w", err)
	}
	if serviceAccount.ID == "" || serviceAccount.APIKey == nil || serviceAccount.APIKey.Value == "" {
		return ProvisionedProject{}, errors.New("create OpenAI service account: response omitted credentials")
	}

	return ProvisionedProject{
		ProjectID:        project.ID,
		ServiceAccountID: serviceAccount.ID,
		APIKey:           serviceAccount.APIKey.Value,
	}, nil
}

func (c *Client) post(ctx context.Context, path string, body any, destination any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.baseURL, "/")+path, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.adminKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "ringring/0.1")

	response, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		limited, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("OpenAI API returned %s: %s", response.Status, safeAPIError(limited))
	}
	if destination == nil {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(destination); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func safeAPIError(body []byte) string {
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &payload) == nil && payload.Error.Message != "" {
		if payload.Error.Type != "" {
			return payload.Error.Type + ": " + payload.Error.Message
		}
		return payload.Error.Message
	}
	return "request failed"
}
