package openaiadmin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestProvision(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-admin-test" {
			t.Errorf("missing authorization header")
		}
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/organization/projects":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["external_key_id"] != "pty_test" {
				t.Errorf("external key = %v", body["external_key_id"])
			}
			_, _ = w.Write([]byte(`{"id":"proj_test"}`))
		case "/organization/projects/proj_test/spend_limit":
			_, _ = w.Write([]byte(`{"object":"project.spend_limit"}`))
		case "/organization/projects/proj_test/service_accounts":
			_, _ = w.Write([]byte(`{"id":"svc_test","api_key":{"id":"key_initial","value":"sk-party-test"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := New("sk-admin-test", 1000, server.Client())
	client.baseURL = server.URL
	got, err := client.Provision(context.Background(), "pty_test", "The Test Party")
	if err != nil {
		t.Fatal(err)
	}
	want := ProvisionedProject{"proj_test", "svc_test", "key_initial", "sk-party-test"}
	if got != want {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	wantPaths := []string{
		"/organization/projects",
		"/organization/projects/proj_test/spend_limit",
		"/organization/projects/proj_test/service_accounts",
	}
	if !reflect.DeepEqual(paths, wantPaths) {
		t.Fatalf("paths = %#v", paths)
	}
}

func TestServiceAccountAPIKeyLifecycle(t *testing.T) {
	var paths []string
	deleted := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-admin-test" {
			t.Error("missing authorization header")
		}
		paths = append(paths, r.Method+" "+r.URL.RequestURI())
		switch r.Method + " " + r.URL.Path {
		case "POST /organization/projects/proj_test/service_accounts/svc_test/api_keys":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["name"] != "ringring-runtime" {
				t.Errorf("key name = %v", body["name"])
			}
			_, _ = w.Write([]byte(`{"id":"key_fresh","value":"sk-fresh-secret"}`))
		case "GET /organization/projects/proj_test/api_keys":
			if r.URL.Query().Get("limit") != "100" || r.URL.Query().Get("owner_project_access") != "active" {
				t.Errorf("unexpected list query: %s", r.URL.RawQuery)
			}
			if r.URL.Query().Get("after") == "" {
				_, _ = w.Write([]byte(`{"data":[{"id":"key_old","owner":{"type":"service_account","service_account":{"id":"svc_test"}}},{"id":"key_other","owner":{"type":"service_account","service_account":{"id":"svc_other"}}},{"id":"key_user","owner":{"type":"user","user":{"id":"usr_test"}}}],"last_id":"key_user","has_more":true}`))
				return
			}
			if r.URL.Query().Get("after") != "key_user" {
				t.Errorf("unexpected cursor: %q", r.URL.Query().Get("after"))
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"key_fresh","owner":{"type":"service_account","service_account":{"id":"svc_test"}}}],"last_id":"key_fresh","has_more":false}`))
		case "DELETE /organization/projects/proj_test/api_keys/key_old":
			deleted["key_old"]++
			_, _ = w.Write([]byte(`{"id":"key_old","deleted":true}`))
		case "DELETE /organization/projects/proj_test/api_keys/key_missing":
			deleted["key_missing"]++
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := New("sk-admin-test", 1000, server.Client())
	client.baseURL = server.URL
	created, err := client.CreateServiceAccountAPIKey(t.Context(), "proj_test", "svc_test")
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != "key_fresh" || created.Value != "sk-fresh-secret" {
		t.Fatalf("unexpected created key: %#v", created)
	}
	ids, err := client.ServiceAccountAPIKeyIDs(t.Context(), "proj_test", "svc_test")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids, []string{"key_old", "key_fresh"}) {
		t.Fatalf("service account key IDs = %#v", ids)
	}
	if err := client.DeleteProjectAPIKey(t.Context(), "proj_test", "key_old"); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteProjectAPIKey(t.Context(), "proj_test", "key_missing"); err != nil {
		t.Fatalf("missing key deletion was not retry-safe: %v", err)
	}
	if deleted["key_old"] != 1 || deleted["key_missing"] != 1 {
		t.Fatalf("unexpected deletions: %#v", deleted)
	}
	if len(paths) != 5 {
		t.Fatalf("request count = %d, paths=%#v", len(paths), paths)
	}
}

func TestServiceAccountAPIKeyResponsesFailClosed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			_, _ = w.Write([]byte(`{"id":"key_without_value"}`))
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"data":[],"last_id":"","has_more":true}`))
		case http.MethodDelete:
			_, _ = w.Write([]byte(`{"id":"wrong_key","deleted":true}`))
		}
	}))
	defer server.Close()
	client := New("sk-admin-test", 1000, server.Client())
	client.baseURL = server.URL
	if _, err := client.CreateServiceAccountAPIKey(t.Context(), "project", "service"); err == nil {
		t.Fatal("accepted a key response without its one-time value")
	}
	if _, err := client.ServiceAccountAPIKeyIDs(t.Context(), "project", "service"); err == nil {
		t.Fatal("accepted invalid pagination")
	}
	if err := client.DeleteProjectAPIKey(t.Context(), "project", "key"); err == nil {
		t.Fatal("accepted deletion without a matching confirmation")
	}
}

func TestArchiveProjectIsRetrySafe(t *testing.T) {
	status := "active"
	archives := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-admin-test" {
			t.Error("missing authorization header")
		}
		if r.URL.Path != "/organization/projects/proj_test" && r.URL.Path != "/organization/projects/proj_test/archive" {
			http.NotFound(w, r)
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /organization/projects/proj_test":
			_, _ = w.Write([]byte(`{"id":"proj_test","status":"` + status + `"}`))
		case "POST /organization/projects/proj_test/archive":
			archives++
			status = "archived"
			_, _ = w.Write([]byte(`{"id":"proj_test","status":"archived"}`))
		default:
			http.Error(w, "wrong method", http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	client := New("sk-admin-test", 1000, server.Client())
	client.baseURL = server.URL
	if err := client.ArchiveProject(context.Background(), "proj_test"); err != nil {
		t.Fatal(err)
	}
	if err := client.ArchiveProject(context.Background(), "proj_test"); err != nil {
		t.Fatal(err)
	}
	if archives != 1 {
		t.Fatalf("archive requests = %d, want 1", archives)
	}
}
