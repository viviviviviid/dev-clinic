package supabase

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coding-tutor/internal/config"
)

const maxResponseBytes = 8 << 20 // 8 MiB

var httpClient = &http.Client{Timeout: 20 * time.Second}

// FilterValue escapes a value before it is interpolated into a PostgREST
// filter expression such as "column=eq.<value>".
func FilterValue(value string) string {
	return url.QueryEscape(value)
}

func Get(path string, result interface{}) error {
	body, err := request(http.MethodGet, path, nil, "")
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, result); err != nil {
		return fmt.Errorf("supabase decode response: %w", err)
	}
	return nil
}

func Insert(table string, data interface{}) error {
	_, err := request(http.MethodPost, table, data, "return=minimal")
	return err
}

func Patch(path string, data interface{}) error {
	_, err := request(http.MethodPatch, path, data, "return=minimal")
	return err
}

func Delete(path string) error {
	_, err := request(http.MethodDelete, path, nil, "")
	return err
}

func Upsert(table string, data interface{}) error {
	_, err := request(http.MethodPost, table, data, "resolution=merge-duplicates,return=minimal")
	return err
}

func request(method, path string, data interface{}, prefer string) ([]byte, error) {
	endpoint, err := endpoint(path)
	if err != nil {
		return nil, err
	}
	key := strings.TrimSpace(config.Global.Supabase.ServiceRoleKey)
	if key == "" {
		return nil, fmt.Errorf("supabase service role key is not configured")
	}

	var body io.Reader
	if data != nil {
		encoded, err := json.Marshal(data)
		if err != nil {
			return nil, fmt.Errorf("supabase encode request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}

	req, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("supabase create request: %w", err)
	}
	req.Header.Set("apikey", key)
	req.Header.Set("Authorization", "Bearer "+key)
	if data != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if prefer != "" {
		req.Header.Set("Prefer", prefer)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("supabase request: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("supabase read response: %w", err)
	}
	if len(responseBody) > maxResponseBytes {
		return nil, fmt.Errorf("supabase response exceeds %d bytes", maxResponseBytes)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("supabase %s error %d: %s", strings.ToLower(method), resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	return responseBody, nil
}

func endpoint(path string) (string, error) {
	base := strings.TrimSpace(config.Global.Supabase.URL)
	if base == "" {
		return "", fmt.Errorf("supabase URL is not configured")
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("invalid supabase URL")
	}
	if strings.HasPrefix(path, "/") || strings.Contains(path, "#") {
		return "", fmt.Errorf("invalid supabase REST path")
	}
	return strings.TrimRight(base, "/") + "/rest/v1/" + path, nil
}
