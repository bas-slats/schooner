package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

const (
	cloudflareAPIBase = "https://api.cloudflare.com/client/v4"
)

// DNSClient handles Cloudflare DNS API operations
type DNSClient struct {
	apiToken   string
	httpClient *http.Client
}

// NewDNSClient creates a new DNS client
func NewDNSClient(apiToken string) *DNSClient {
	return &DNSClient{
		apiToken:   apiToken,
		httpClient: &http.Client{},
	}
}

// Zone represents a Cloudflare zone
type Zone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// DNSRecord represents a Cloudflare DNS record
type DNSRecord struct {
	ID      string `json:"id,omitempty"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
	TTL     int    `json:"ttl,omitempty"`
}

type apiResponse struct {
	Success bool            `json:"success"`
	Errors  []apiError      `json:"errors"`
	Result  json.RawMessage `json:"result"`
}

type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// doRequest performs an authenticated API request
func (c *DNSClient) doRequest(ctx context.Context, method, path string, body interface{}) (*apiResponse, error) {
	var reqBody io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, cloudflareAPIBase+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var apiResp apiResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if !apiResp.Success {
		if len(apiResp.Errors) > 0 {
			return nil, fmt.Errorf("API error: %s (code %d)", apiResp.Errors[0].Message, apiResp.Errors[0].Code)
		}
		return nil, fmt.Errorf("API request failed")
	}

	return &apiResp, nil
}

// GetZoneByName finds a zone by its domain name
func (c *DNSClient) GetZoneByName(ctx context.Context, name string) (*Zone, error) {
	resp, err := c.doRequest(ctx, "GET", "/zones?name="+name, nil)
	if err != nil {
		return nil, err
	}

	var zones []Zone
	if err := json.Unmarshal(resp.Result, &zones); err != nil {
		return nil, fmt.Errorf("failed to parse zones: %w", err)
	}

	if len(zones) == 0 {
		return nil, fmt.Errorf("zone not found: %s", name)
	}

	return &zones[0], nil
}

// GetDNSRecord finds a DNS record by name and type
func (c *DNSClient) GetDNSRecord(ctx context.Context, zoneID, recordType, name string) (*DNSRecord, error) {
	path := fmt.Sprintf("/zones/%s/dns_records?type=%s&name=%s", zoneID, recordType, name)
	resp, err := c.doRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}

	var records []DNSRecord
	if err := json.Unmarshal(resp.Result, &records); err != nil {
		return nil, fmt.Errorf("failed to parse records: %w", err)
	}

	if len(records) == 0 {
		return nil, nil // Not found
	}

	return &records[0], nil
}

// CreateDNSRecord creates a new DNS record
func (c *DNSClient) CreateDNSRecord(ctx context.Context, zoneID string, record DNSRecord) (*DNSRecord, error) {
	path := fmt.Sprintf("/zones/%s/dns_records", zoneID)
	resp, err := c.doRequest(ctx, "POST", path, record)
	if err != nil {
		return nil, err
	}

	var created DNSRecord
	if err := json.Unmarshal(resp.Result, &created); err != nil {
		return nil, fmt.Errorf("failed to parse created record: %w", err)
	}

	return &created, nil
}

// UpdateDNSRecord updates an existing DNS record
func (c *DNSClient) UpdateDNSRecord(ctx context.Context, zoneID, recordID string, record DNSRecord) (*DNSRecord, error) {
	path := fmt.Sprintf("/zones/%s/dns_records/%s", zoneID, recordID)
	resp, err := c.doRequest(ctx, "PUT", path, record)
	if err != nil {
		return nil, err
	}

	var updated DNSRecord
	if err := json.Unmarshal(resp.Result, &updated); err != nil {
		return nil, fmt.Errorf("failed to parse updated record: %w", err)
	}

	return &updated, nil
}

// DeleteDNSRecord deletes a DNS record
func (c *DNSClient) DeleteDNSRecord(ctx context.Context, zoneID, recordID string) error {
	path := fmt.Sprintf("/zones/%s/dns_records/%s", zoneID, recordID)
	_, err := c.doRequest(ctx, "DELETE", path, nil)
	return err
}

// EnsureTunnelCNAME creates or updates a CNAME record pointing to the tunnel
func (c *DNSClient) EnsureTunnelCNAME(ctx context.Context, hostname, tunnelID string) error {
	// Extract the zone name (e.g., "slats.dev" from "schooner.slats.dev")
	parts := strings.Split(hostname, ".")
	if len(parts) < 2 {
		return fmt.Errorf("invalid hostname: %s", hostname)
	}
	zoneName := strings.Join(parts[len(parts)-2:], ".")

	// Get the zone ID
	zone, err := c.GetZoneByName(ctx, zoneName)
	if err != nil {
		return fmt.Errorf("failed to get zone: %w", err)
	}

	tunnelTarget := fmt.Sprintf("%s.cfargotunnel.com", tunnelID)

	// Check if record exists
	existing, err := c.GetDNSRecord(ctx, zone.ID, "CNAME", hostname)
	if err != nil {
		return fmt.Errorf("failed to check existing record: %w", err)
	}

	record := DNSRecord{
		Type:    "CNAME",
		Name:    hostname,
		Content: tunnelTarget,
		Proxied: true,
		TTL:     1, // Auto
	}

	if existing != nil {
		// Update if content is different
		if existing.Content != tunnelTarget {
			slog.Info("updating DNS record", "hostname", hostname, "tunnel", tunnelTarget)
			_, err = c.UpdateDNSRecord(ctx, zone.ID, existing.ID, record)
			if err != nil {
				return fmt.Errorf("failed to update record: %w", err)
			}
		} else {
			slog.Debug("DNS record already correct", "hostname", hostname)
		}
	} else {
		// Create new record
		slog.Info("creating DNS record", "hostname", hostname, "tunnel", tunnelTarget)
		_, err = c.CreateDNSRecord(ctx, zone.ID, record)
		if err != nil {
			return fmt.Errorf("failed to create record: %w", err)
		}
	}

	return nil
}
