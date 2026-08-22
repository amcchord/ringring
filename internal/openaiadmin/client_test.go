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
			_, _ = w.Write([]byte(`{"id":"svc_test","api_key":{"value":"sk-party-test"}}`))
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
	want := ProvisionedProject{"proj_test", "svc_test", "sk-party-test"}
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
