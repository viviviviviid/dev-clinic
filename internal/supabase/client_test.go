package supabase

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
		if r.Header.Get("apikey") != "sb_publishable_test" || r.Header.Get("Authorization") != "Bearer user-token" {
			t.Fatal("missing Supabase authentication headers")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`[{"id":"one"}]`)),
		}, nil
	})}

	config.Global.Supabase.URL = "https://example.supabase.co"
	config.Global.Supabase.AnonKey = "sb_publishable_test"
	var rows []struct {
		ID string `json:"id"`
	}
	if err := Get(WithAccessToken(context.Background(), "user-token"), "items?select=*", &rows); err != nil {
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
	config.Global.Supabase.AnonKey = ""

	if err := Insert(context.Background(), "items", map[string]string{"id": "one"}); err == nil {
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

func TestRESTKeepsConcurrentUsersSeparateAndRequiresToken(t *testing.T) {
	previous := *config.Global
	t.Cleanup(func() { *config.Global = previous })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := r.URL.Query().Get("user")
		if r.Header.Get("apikey") != "sb_publishable_test" || r.Header.Get("Authorization") != "Bearer "+user {
			t.Error("request used another user's credentials")
			http.Error(w, "wrong user", http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()
	config.Global.Supabase = config.SupabaseConfig{URL: server.URL, AnonKey: "sb_publishable_test"}
	var wg sync.WaitGroup
	for _, user := range []string{"alice", "bob", "carol", "dan"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := WithAccessToken(context.Background(), user)
			var rows []interface{}
			if err := Get(ctx, "items?user="+user, &rows); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if err := Insert(context.Background(), "items", map[string]string{"id": "no-token"}); err == nil {
		t.Fatal("unauthenticated write accepted")
	}
	ctx, cancel := context.WithCancel(WithAccessToken(context.Background(), "alice"))
	cancel()
	if err := Insert(ctx, "items?user=alice", nil); err == nil {
		t.Fatal("cancelled request accepted")
	}
}
