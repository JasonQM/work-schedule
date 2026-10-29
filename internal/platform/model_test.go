package platform

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func validInput() ItemInput {
	return ItemInput{RequestID: "request-1", ProjectID: "project-1", ExternalKey: "TASK-1", Kind: "task", Title: "交付登录", ParentID: "requirement-1", Status: "in_progress", Priority: "normal"}
}
func TestValidation(t *testing.T) {
	tests := []struct {
		name    string
		edit    func(*ItemInput)
		role    string
		wantErr bool
	}{
		{"normal task", func(i *ItemInput) {}, "member", false},
		{"AI cannot accept", func(i *ItemInput) { i.Status = "accepted"; i.Evidence = "test passed" }, "member", true},
		{"human acceptance", func(i *ItemInput) { i.Status = "accepted"; i.Evidence = "reviewed" }, "admin", false},
		{"acceptance needs evidence", func(i *ItemInput) { i.Status = "pending_acceptance" }, "member", true},
		{"blocked requires next step", func(i *ItemInput) { i.Status = "blocked" }, "member", true},
		{"task must belong to requirement", func(i *ItemInput) { i.ParentID = "" }, "member", true},
		{"session cannot be accepted", func(i *ItemInput) { i.Kind = "session"; i.ParentID = ""; i.Status = "accepted"; i.Evidence = "x" }, "admin", true},
		{"requirement requires criteria", func(i *ItemInput) { i.Kind = "requirement"; i.ParentID = "" }, "member", true},
		{"empty whitespace title", func(i *ItemInput) { i.Title = "  " }, "member", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := validInput()
			tt.edit(&in)
			err := in.Validate(User{Role: tt.role})
			if (err != nil) != tt.wantErr {
				t.Fatalf("unexpected validation result: %v", err)
			}
		})
	}
}
func TestOriginAndAuthBoundary(t *testing.T) {
	a := NewApp(nil, Config{PublicURL: "https://work.example.com", Secure: true})
	tests := []struct {
		name, method, path, origin, body string
		status                           int
	}{
		{"cross origin", "POST", "/api/login", "https://evil.example", "{}", 403},
		{"unauthenticated MCP", "POST", "/mcp", "", "{}", 401},
		{"unauthenticated dashboard", "GET", "/api/me", "", "", 401},
		{"reject unknown fields", "POST", "/api/login", "https://work.example.com", `{"unexpected":true}`, 400},
		{"reject multiple objects", "POST", "/api/login", "", `{} {}`, 400},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			r.Header.Set("Origin", tt.origin)
			w := httptest.NewRecorder()
			a.Handler().ServeHTTP(w, r)
			if w.Code != tt.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}
func TestRequestFingerprint(t *testing.T) {
	a := validInput()
	b := a
	b.Status = "blocked"
	if fingerprint(a) == fingerprint(b) {
		t.Fatal("different payloads must not share fingerprints")
	}
}
func TestCredentialEntropy(t *testing.T) {
	a, b := secret(), secret()
	if a == b || len(a) != 64 || len(digest(a)) != 64 {
		t.Fatal("invalid generated credential")
	}
}
func TestConfig(t *testing.T) {
	if ValidateConfig(Config{PublicURL: "https://work.example.com", Secure: true}) != nil {
		t.Fatal("valid config rejected")
	}
	if ValidateConfig(Config{PublicURL: "https://work.example.com"}) == nil {
		t.Fatal("insecure HTTPS cookie accepted")
	}
}
func TestRateLimiter(t *testing.T) {
	l := &limiter{entries: map[string]bucket{}}
	for i := 0; i < 10; i++ {
		if !l.Allow("login:client", 10) {
			t.Fatal("limited too early")
		}
	}
	if l.Allow("login:client", 10) {
		t.Fatal("missing limit")
	}
}
