package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPDiscovery(t *testing.T) {
	a := NewApp(nil, Config{})
	srv := httptest.NewServer(a.mcpHandler())
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: srv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(tools.Tools))
	}
	// Validate a malformed request at the actual protocol boundary without touching storage.
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "sync_work_item", Arguments: map[string]any{"project_id": "missing"}})
	if err == nil && !result.IsError {
		t.Fatal("malformed tool input must fail")
	}
}
func TestMySQLWorkflow(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set TEST_MYSQL_DSN to a dedicated MySQL test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	s, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.DB.Close() })
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	suffix := secret()[:10]
	u, err := s.CreateUser(ctx, "test-"+suffix+"@example.com", "测试成员", "test-password-12345", "admin")
	if err != nil {
		t.Fatal(err)
	}
	u.Role = "member" // API credentials are restricted even for an administrator.
	var projectID string

	t.Cleanup(func() {
		tx, err := s.DB.Begin()
		if err != nil {
			t.Error(err)
			return
		}
		defer tx.Rollback()
		exec := func(q string, arg any) bool {
			if _, err := tx.Exec(q, arg); err != nil {
				t.Errorf("test cleanup failed: %v", err)
				return false
			}
			return true
		}
		if projectID != "" {
			for _, q := range []string{"DELETE FROM events WHERE project_id=?", "DELETE FROM items WHERE project_id=? AND parent_id IS NOT NULL", "DELETE FROM items WHERE project_id=?", "DELETE FROM projects WHERE id=?"} {
				if !exec(q, projectID) {
					return
				}
			}
		}
		for _, q := range []string{"DELETE FROM credentials WHERE user_id=?", "DELETE FROM requests WHERE user_id=?", "DELETE FROM users WHERE id=?"} {
			if !exec(q, u.ID) {
				return
			}
		}
		if _, err = tx.Exec("UPDATE workspace_revision SET revision=revision+1 WHERE id=1"); err != nil {
			t.Error(err)
			return
		}
		if err = tx.Commit(); err != nil {
			t.Error(err)
		}
	})
	projectIn := ProjectInput{RequestID: "project-" + suffix, IdentityKey: "test:" + suffix, Name: "自动接入测试"}
	out, err := s.EnsureProject(ctx, u, projectIn)
	if err != nil {
		t.Fatal(err)
	}
	p := convert[Project](t, out)
	projectID = p.ID
	repeated, err := s.EnsureProject(ctx, u, projectIn)
	if err != nil || convert[Project](t, repeated).ID != p.ID {
		t.Fatal("idempotent registration failed", err)
	}
	different := projectIn
	different.Name = "changed"
	if _, err = s.EnsureProject(ctx, u, different); err == nil {
		t.Fatal("request reuse with changed payload accepted")
	}
	req := ItemInput{RequestID: "req-" + suffix, ProjectID: p.ID, ExternalKey: "REQ-1", Kind: "requirement", Title: "用户登录", Acceptance: "正确密码可以登录", Status: "todo"}
	out, err = s.PutItem(ctx, u, req)
	if err != nil {
		t.Fatal(err)
	}
	r := convert[Item](t, out)
	task := ItemInput{RequestID: "task-" + suffix, ProjectID: p.ID, ExternalKey: "TASK-1", Kind: "task", ParentID: r.ID, Title: "登录接口", Status: "pending_acceptance", Evidence: "接口测试通过"}
	out, err = s.PutItem(ctx, u, task)
	if err != nil {
		t.Fatal(err)
	}
	child := convert[Item](t, out)
	task.ID = child.ID
	task.ExpectedVersion = child.Version
	task.Status = "accepted"
	task.RequestID = "accept-ai-" + suffix
	if _, err = s.PutItem(ctx, u, task); err == nil {
		t.Fatal("AI accepted own delivery")
	}
	task.RequestID = "accept-human-" + suffix
	sessionToken, err := s.Credential(ctx, u.ID, "session", "browser-test", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(task)
	request := httptest.NewRequest("PUT", "/api/items", bytes.NewReader(payload))
	request.AddCookie(&http.Cookie{Name: "ws_session", Value: sessionToken})
	response := httptest.NewRecorder()
	NewApp(s, Config{PublicURL: "http://localhost"}).Handler().ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("browser acceptance failed: %s", response.Body.String())
	}
	req.ID = r.ID
	req.ExpectedVersion = r.Version
	req.Description = "增加登录失败限流"
	req.RequestID = "change-" + suffix
	out, err = s.PutItem(ctx, u, req)
	if err != nil {
		t.Fatal(err)
	}
	changed := convert[Item](t, out)
	if changed.Status != "needs_review" || changed.Version != 2 {
		t.Fatal("requirement revision not invalidated")
	}
	snap, err := s.Snapshot(ctx, u, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range snap.Items {
		if i.ID == child.ID && (i.Status != "needs_review" || i.Evidence != "" || i.Version != 3) {
			t.Fatalf("stale task acceptance survives requirement change: %+v", i)
		}
	}
	req.RequestID = "stale-" + suffix
	if _, err = s.PutItem(ctx, u, req); err == nil {
		t.Fatal("stale version update accepted")
	}
	// Authenticate browser, create MCP token, then verify transport auth and tool writes.
	raw, err := s.Credential(ctx, u.ID, "api", "integration", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp(s, Config{PublicURL: "http://localhost", WebDir: "../../web/dist"})
	server := httptest.NewServer(app.Handler())
	defer server.Close()
	transport := &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: authTransport{base: http.DefaultTransport, token: raw}}}
	client := mcp.NewClient(&mcp.Implementation{Name: "integration", Version: "1.0"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "read_workspace", Arguments: map[string]any{"project_id": p.ID}})
	if err != nil || res.IsError {
		t.Fatal("authenticated MCP read failed", err)
	}
	res, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "ensure_project", Arguments: projectIn})
	if err != nil || res.IsError {
		t.Fatal("authenticated idempotent MCP write failed", err)
	}
	mcpTask := ItemInput{RequestID: "mcp-task-" + suffix, ProjectID: p.ID, ExternalKey: "TASK-MCP", Kind: "task", ParentID: r.ID, Title: "MCP真实写入", Status: "pending_acceptance", Evidence: "集成测试"}
	res, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "sync_work_item", Arguments: mcpTask})
	if err != nil || res.IsError {
		t.Fatal("authenticated MCP create failed", err)
	}
	mcpTask.ExternalKey = "TASK-MCP-ACCEPT"
	mcpTask.RequestID = "mcp-accept-" + suffix
	mcpTask.Status = "accepted"
	res, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "sync_work_item", Arguments: mcpTask})
	if err != nil || !res.IsError {
		t.Fatal("admin API credential bypassed acceptance boundary", err)
	}
	if err = s.ChangePassword(ctx, User{ID: u.ID}, "test-password-12345", "changed-test-password-123", sessionToken); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(ctx, raw, "api"); err == nil {
		t.Fatal("API credential survived password change")
	}
	if _, err = s.Authenticate(ctx, sessionToken, "session"); err != nil {
		t.Fatal("current browser session should remain valid")
	}

}

type authTransport struct {
	base  http.RoundTripper
	token string
}

func (a authTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+a.token)
	return a.base.RoundTrip(r)
}
func convert[T any](t *testing.T, v any) T {
	t.Helper()
	var out T
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
