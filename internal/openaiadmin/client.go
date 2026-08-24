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
	"time"
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
	APIKeyID         string
	APIKey           string
	SpendLimitCents  int
}

type SpendLimit struct {
	ThresholdAmount   int
	Currency          string
	Interval          string
	EnforcementStatus string
}

type ServiceAccountAPIKey struct {
	ID    string
	Value string
}

// OrganizationDataRetention is the narrow, non-secret provider evidence used
// by the operator safety gate. RingRing accepts only the two current API values
// that explicitly identify Zero Data Retention.
type OrganizationDataRetention struct {
	Type string `json:"type"`
}

type ProjectDataRetention struct {
	Type string `json:"type"`
}

type projectResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
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

// VerifyOrganizationZeroDataRetention reads the organization control without
// changing it. A missing key, provider denial, malformed response, unknown
// value, or any non-ZDR retention mode fails closed.
func (c *Client) VerifyOrganizationZeroDataRetention(ctx context.Context) (OrganizationDataRetention, error) {
	if c.adminKey == "" {
		return OrganizationDataRetention{}, errors.New("OpenAI admin key is not configured")
	}
	var response struct {
		Object string `json:"object"`
		Type   string `json:"type"`
	}
	if err := c.get(ctx, "/organization/data_retention", &response); err != nil {
		return OrganizationDataRetention{}, fmt.Errorf("retrieve OpenAI organization data retention: %w", err)
	}
	if response.Object != "organization.data_retention" {
		return OrganizationDataRetention{}, errors.New("retrieve OpenAI organization data retention: response had an unexpected object")
	}
	switch response.Type {
	case "zero_data_retention", "enhanced_zero_data_retention":
		return OrganizationDataRetention{Type: response.Type}, nil
	case "modified_abuse_monitoring", "enhanced_modified_abuse_monitoring":
		return OrganizationDataRetention{}, errors.New("OpenAI organization has not enabled Zero Data Retention")
	default:
		return OrganizationDataRetention{}, errors.New("OpenAI organization returned an unknown data retention control")
	}
}

// VerifyProjectZeroDataRetention checks a single party-owned project after the
// organization itself has already passed the ZDR check. Organization-default
// is therefore safe; explicit non-ZDR overrides are not.
func (c *Client) VerifyProjectZeroDataRetention(ctx context.Context, projectID string) (ProjectDataRetention, error) {
	if c.adminKey == "" {
		return ProjectDataRetention{}, errors.New("OpenAI admin key is not configured")
	}
	if projectID == "" {
		return ProjectDataRetention{}, errors.New("OpenAI project ID is required")
	}
	var response struct {
		Object string `json:"object"`
		Type   string `json:"type"`
	}
	path := "/organization/projects/" + url.PathEscape(projectID) + "/data_retention"
	if err := c.get(ctx, path, &response); err != nil {
		return ProjectDataRetention{}, fmt.Errorf("retrieve OpenAI project data retention: %w", err)
	}
	if response.Object != "project.data_retention" {
		return ProjectDataRetention{}, errors.New("retrieve OpenAI project data retention: response had an unexpected object")
	}
	switch response.Type {
	case "organization_default", "zero_data_retention", "enhanced_zero_data_retention":
		return ProjectDataRetention{Type: response.Type}, nil
	case "none", "modified_abuse_monitoring", "enhanced_modified_abuse_monitoring":
		return ProjectDataRetention{}, errors.New("OpenAI project has not enabled Zero Data Retention")
	default:
		return ProjectDataRetention{}, errors.New("OpenAI project returned an unknown data retention control")
	}
}

func (c *Client) Provision(ctx context.Context, partyID, partyName string) (provisioned ProvisionedProject, err error) {
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
	complete := false
	defer func() {
		if complete {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
		defer cancel()
		if cleanupErr := c.ArchiveProject(cleanupCtx, project.ID); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("archive incomplete OpenAI project: %w", cleanupErr))
		}
	}()

	limit, err := c.UpdateProjectSpendLimit(ctx, project.ID, c.spendLimit)
	if err != nil {
		return ProvisionedProject{}, fmt.Errorf("set OpenAI project spend limit: %w", err)
	}

	var serviceAccount struct {
		ID     string `json:"id"`
		APIKey *struct {
			ID    string `json:"id"`
			Value string `json:"value"`
		} `json:"api_key"`
	}
	path := "/organization/projects/" + url.PathEscape(project.ID) + "/service_accounts"
	if err := c.post(ctx, path, map[string]any{"name": "ringring-runtime"}, &serviceAccount); err != nil {
		return ProvisionedProject{}, fmt.Errorf("create OpenAI service account: %w", err)
	}
	if serviceAccount.ID == "" || serviceAccount.APIKey == nil || serviceAccount.APIKey.ID == "" || serviceAccount.APIKey.Value == "" {
		return ProvisionedProject{}, errors.New("create OpenAI service account: response omitted credentials")
	}

	provisioned = ProvisionedProject{
		ProjectID:        project.ID,
		ServiceAccountID: serviceAccount.ID,
		APIKeyID:         serviceAccount.APIKey.ID,
		APIKey:           serviceAccount.APIKey.Value,
		SpendLimitCents:  limit.ThresholdAmount,
	}
	complete = true
	return provisioned, nil
}

// UpdateProjectSpendLimit creates or replaces a project's hard monthly limit.
// It succeeds only when OpenAI echoes the exact requested USD amount and says
// enforcement is active; callers may safely retry the same amount after an
// ambiguous response or transport failure.
func (c *Client) UpdateProjectSpendLimit(ctx context.Context, projectID string, thresholdAmount int) (SpendLimit, error) {
	if c.adminKey == "" {
		return SpendLimit{}, errors.New("OpenAI admin key is not configured")
	}
	if projectID == "" {
		return SpendLimit{}, errors.New("OpenAI project ID is required")
	}
	if thresholdAmount < 1 {
		return SpendLimit{}, errors.New("OpenAI spend limit must be at least one cent")
	}
	var response struct {
		Object          string `json:"object"`
		ThresholdAmount int    `json:"threshold_amount"`
		Currency        string `json:"currency"`
		Interval        string `json:"interval"`
		Enforcement     struct {
			Status string `json:"status"`
		} `json:"enforcement"`
	}
	path := "/organization/projects/" + url.PathEscape(projectID) + "/spend_limit"
	if err := c.post(ctx, path, map[string]any{
		"threshold_amount": thresholdAmount,
		"currency":         "USD",
		"interval":         "month",
	}, &response); err != nil {
		return SpendLimit{}, fmt.Errorf("update OpenAI project spend limit: %w", err)
	}
	if response.Object != "project.spend_limit" || response.ThresholdAmount != thresholdAmount ||
		response.Currency != "USD" || response.Interval != "month" || response.Enforcement.Status != "enforcing" {
		return SpendLimit{}, errors.New("update OpenAI project spend limit: response did not confirm the requested enforced monthly USD limit")
	}
	return SpendLimit{
		ThresholdAmount: response.ThresholdAmount, Currency: response.Currency,
		Interval: response.Interval, EnforcementStatus: response.Enforcement.Status,
	}, nil
}

// CreateServiceAccountAPIKey creates a replacement runtime key. The returned
// value is available only in this response and must be encrypted immediately.
func (c *Client) CreateServiceAccountAPIKey(ctx context.Context, projectID, serviceAccountID string) (ServiceAccountAPIKey, error) {
	if err := c.validateKeyManagement(projectID, serviceAccountID); err != nil {
		return ServiceAccountAPIKey{}, err
	}
	var created struct {
		ID    string `json:"id"`
		Value string `json:"value"`
	}
	path := "/organization/projects/" + url.PathEscape(projectID) + "/service_accounts/" + url.PathEscape(serviceAccountID) + "/api_keys"
	if err := c.post(ctx, path, map[string]any{"name": "ringring-runtime"}, &created); err != nil {
		return ServiceAccountAPIKey{}, fmt.Errorf("create OpenAI service account key: %w", err)
	}
	if created.ID == "" || created.Value == "" {
		return ServiceAccountAPIKey{}, errors.New("create OpenAI service account key: response omitted credentials")
	}
	return ServiceAccountAPIKey{ID: created.ID, Value: created.Value}, nil
}

// ServiceAccountAPIKeyIDs lists only active project keys owned by the supplied
// dedicated service account. It never returns redacted or unredacted key values.
func (c *Client) ServiceAccountAPIKeyIDs(ctx context.Context, projectID, serviceAccountID string) ([]string, error) {
	if err := c.validateKeyManagement(projectID, serviceAccountID); err != nil {
		return nil, err
	}
	var ids []string
	after := ""
	for page := 0; page < 100; page++ {
		query := url.Values{"limit": {"100"}, "owner_project_access": {"active"}}
		if after != "" {
			query.Set("after", after)
		}
		var result struct {
			Data []struct {
				ID    string `json:"id"`
				Owner struct {
					Type           string `json:"type"`
					ServiceAccount *struct {
						ID string `json:"id"`
					} `json:"service_account"`
				} `json:"owner"`
			} `json:"data"`
			LastID  string `json:"last_id"`
			HasMore bool   `json:"has_more"`
		}
		path := "/organization/projects/" + url.PathEscape(projectID) + "/api_keys?" + query.Encode()
		if err := c.get(ctx, path, &result); err != nil {
			return nil, fmt.Errorf("list OpenAI service account keys: %w", err)
		}
		for _, key := range result.Data {
			if key.ID != "" && key.Owner.Type == "service_account" && key.Owner.ServiceAccount != nil && key.Owner.ServiceAccount.ID == serviceAccountID {
				ids = append(ids, key.ID)
			}
		}
		if !result.HasMore {
			return ids, nil
		}
		if result.LastID == "" || result.LastID == after {
			return nil, errors.New("list OpenAI service account keys: invalid pagination response")
		}
		after = result.LastID
	}
	return nil, errors.New("list OpenAI service account keys: pagination limit exceeded")
}

// DeleteProjectAPIKey is retry-safe: a missing key is already retired.
func (c *Client) DeleteProjectAPIKey(ctx context.Context, projectID, keyID string) error {
	if c.adminKey == "" {
		return errors.New("OpenAI admin key is not configured")
	}
	if projectID == "" || keyID == "" {
		return errors.New("OpenAI project and key IDs are required")
	}
	path := "/organization/projects/" + url.PathEscape(projectID) + "/api_keys/" + url.PathEscape(keyID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, strings.TrimRight(c.baseURL, "/")+path, nil)
	if err != nil {
		return fmt.Errorf("delete OpenAI project key: create request: %w", err)
	}
	c.setHeaders(req)
	response, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete OpenAI project key: send request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		limited, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("delete OpenAI project key: OpenAI API returned %s: %s", response.Status, safeAPIError(limited))
	}
	var deleted struct {
		ID      string `json:"id"`
		Deleted bool   `json:"deleted"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&deleted); err != nil {
		return fmt.Errorf("delete OpenAI project key: decode response: %w", err)
	}
	if !deleted.Deleted || deleted.ID != keyID {
		return errors.New("delete OpenAI project key: response did not confirm deletion")
	}
	return nil
}

func (c *Client) validateKeyManagement(projectID, serviceAccountID string) error {
	if c.adminKey == "" {
		return errors.New("OpenAI admin key is not configured")
	}
	if projectID == "" || serviceAccountID == "" {
		return errors.New("OpenAI project and service account IDs are required")
	}
	return nil
}

// ArchiveProject disables a party's external project before RingRing removes
// its local encryption key and ownership record. Retrieving first makes retries
// safe when a previous archive succeeded but the local delete did not.
func (c *Client) ArchiveProject(ctx context.Context, projectID string) error {
	if c.adminKey == "" {
		return errors.New("OpenAI admin key is not configured")
	}
	if projectID == "" {
		return errors.New("OpenAI project ID is required")
	}
	path := "/organization/projects/" + url.PathEscape(projectID)
	var current projectResponse
	if err := c.get(ctx, path, &current); err != nil {
		return fmt.Errorf("retrieve OpenAI project: %w", err)
	}
	if current.Status == "archived" {
		return nil
	}
	var archived projectResponse
	if err := c.post(ctx, path+"/archive", map[string]any{}, &archived); err != nil {
		return fmt.Errorf("archive OpenAI project: %w", err)
	}
	if archived.Status == "archived" {
		return nil
	}
	var confirmed projectResponse
	if err := c.get(ctx, path, &confirmed); err != nil {
		return fmt.Errorf("confirm archived OpenAI project: %w", err)
	}
	if confirmed.Status != "archived" {
		return errors.New("archive OpenAI project: follow-up did not confirm archived status")
	}
	return nil
}

func (c *Client) get(ctx context.Context, path string, destination any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.baseURL, "/")+path, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	c.setHeaders(req)
	return c.do(req, destination)
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
	req.Header.Set("Content-Type", "application/json")
	c.setHeaders(req)
	return c.do(req, destination)
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.adminKey)
	req.Header.Set("User-Agent", "ringring/0.1")
}

func (c *Client) do(req *http.Request, destination any) error {
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
