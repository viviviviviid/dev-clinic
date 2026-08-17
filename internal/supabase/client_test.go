package supabase

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/coding-tutor/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestGetUsesAuthenticatedRESTRequest(t *testing.T) {
	original := *config.Global
	t.Cleanup(func() { *config.Global = original })
	originalClient := httpClient
	t.Cleanup(func() { httpClient = originalClient })
	httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/rest/v1/items" || r.URL.Query().Get("select") != "*" {
			t.Fatalf("unexpected URL: %s", r.URL.String())
		}
		if r.Header.Get("apikey") != "secret" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatal("missing Supabase authentication headers")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`[{"id":"one"}]`)),
		}, nil
	})}

	config.Global.Supabase.URL = "https://example.supabase.co"
	config.Global.Supabase.ServiceRoleKey = "secret"
	var rows []struct {
		ID string `json:"id"`
	}
	if err := Get("items?select=*", &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != "one" {
		t.Fatalf("unexpected rows: %#v", rows)
	}
}

func TestRequestRejectsMissingConfiguration(t *testing.T) {
	original := *config.Global
	t.Cleanup(func() { *config.Global = original })
	config.Global.Supabase.URL = ""
	config.Global.Supabase.ServiceRoleKey = ""

	if err := Insert("items", map[string]string{"id": "one"}); err == nil {
		t.Fatal("Insert succeeded without Supabase configuration")
	}
}

func TestEndpointRejectsInvalidPath(t *testing.T) {
	original := *config.Global
	t.Cleanup(func() { *config.Global = original })
	config.Global.Supabase.URL = "https://example.supabase.co"

	if _, err := endpoint("/outside"); err == nil {
		t.Fatal("endpoint accepted an absolute path")
	}
}

func TestFilterValueEscapesQuerySeparators(t *testing.T) {
	got := FilterValue("project&user_id=neq.owner")
	if got == "project&user_id=neq.owner" {
		t.Fatal("FilterValue left query separators unescaped")
	}
}
