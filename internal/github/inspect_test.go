package github

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
)

func testInspectRSAKey(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating RSA key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
}

func TestInspectRepo_WIZ3(t *testing.T) {
	pemBytes := testInspectRSAKey(t)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /app/installations/42/access_tokens", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Repositories []string `json:"repositories"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode access_tokens body: %v", err)
		}
		if len(body.Repositories) != 1 || body.Repositories[0] != "my-app" {
			t.Fatalf("expected repo-scoped token for [my-app], got %v", body.Repositories)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":      "ghs_scoped_wiz3_token",
			"expires_at": "2026-11-25T15:00:00Z",
		})
	})
	mux.HandleFunc("GET /repos/octo/my-app/branches", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token ghs_scoped_wiz3_token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode([]Branch{
			{Name: "main", Protected: true},
			{Name: "staging", Protected: false},
		})
	})
	mux.HandleFunc("GET /repos/octo/my-app/git/trees/main", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"truncated": false,
			"tree": []map[string]string{
				{"path": "README.md", "type": "blob"},
				{"path": ".gitmodules", "type": "blob"},
				{"path": ".gitattributes", "type": "blob"},
				{"path": "web/Dockerfile", "type": "blob"},
				{"path": "node_modules/pkg/Dockerfile", "type": "blob"},
			},
		})
	})
	mux.HandleFunc("GET /repos/octo/my-app/contents/.gitattributes", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"encoding": "base64",
			"content":  base64.StdEncoding.EncodeToString([]byte("*.bin filter=lfs diff=lfs merge=lfs -text\n")),
		})
	})
	mux.HandleFunc("GET /repos/octo/my-app/contents/web/Dockerfile", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"encoding": "base64",
			"content":  base64.StdEncoding.EncodeToString([]byte("FROM node:22-alpine\nEXPOSE 3000/tcp\nCMD [\"node\", \"server.js\"]\n")),
		})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := NewClient()
	client.APIBaseURL = srv.URL

	branches, err := client.ListBranches(context.Background(), 10, pemBytes, 42, "octo", "my-app")
	if err != nil {
		t.Fatalf("ListBranches: %v", err)
	}
	if len(branches) != 2 || branches[0].Name != "main" {
		t.Fatalf("unexpected branches: %+v", branches)
	}

	insp, err := client.InspectRepo(context.Background(), 10, pemBytes, 42, "octo", "my-app", "main")
	if err != nil {
		t.Fatalf("InspectRepo: %v", err)
	}
	if !insp.HasContainerfile || insp.Containerfile != "web/Dockerfile" || insp.BuildContext != "web" {
		t.Fatalf("unexpected containerfile detection: %+v", insp)
	}
	if insp.SuggestedPort != 3000 {
		t.Fatalf("expected suggested port 3000, got %d", insp.SuggestedPort)
	}
	if !insp.HasSubmodules || !insp.HasLFS {
		t.Fatalf("expected HasSubmodules=true and HasLFS=true, got %+v", insp)
	}
	if len(insp.Candidates) != 1 {
		t.Fatalf("expected node_modules Dockerfile to be ignored, got %+v", insp.Candidates)
	}
}

func TestParseExposedPort_VariableAndEmpty_WIZ3(t *testing.T) {
	if got := parseExposedPort("FROM alpine\nEXPOSE $PORT\n"); got != 0 {
		t.Fatalf("expected 0 for EXPOSE $PORT, got %d", got)
	}
	if got := parseExposedPort("FROM alpine\nEXPOSE ${PORT}\n"); got != 0 {
		t.Fatalf("expected 0 for EXPOSE ${PORT}, got %d", got)
	}
	if got := parseExposedPort("FROM alpine\nCMD [\"echo\", \"hi\"]\n"); got != 0 {
		t.Fatalf("expected 0 when no EXPOSE, got %d", got)
	}
	if got := parseExposedPort("FROM golang:1.27\n# EXPOSE 9999\nEXPOSE 8080\n"); got != 8080 {
		t.Fatalf("expected 8080, got %d", got)
	}
}

func TestInspectRepo_EmptyRepo409_WIZ3(t *testing.T) {
	pemBytes := testInspectRSAKey(t)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /app/installations/42/access_tokens", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "ghs_tok", "expires_at": "2026-11-25T15:00:00Z"})
	})
	mux.HandleFunc("GET /repos/octo/empty/git/trees/main", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Git Repository is empty."})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := NewClient()
	client.APIBaseURL = srv.URL
	insp, err := client.InspectRepo(context.Background(), 10, pemBytes, 42, "octo", "empty", "main")
	if err != nil {
		t.Fatalf("expected nil error on empty 409 repo, got %v", err)
	}
	if insp.HasContainerfile || len(insp.Candidates) != 0 {
		t.Fatalf("expected HasContainerfile=false, got %+v", insp)
	}
}
