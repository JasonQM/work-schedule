package platform

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func text(v string) *string { return &v }
func TestPatchPreservesOmittedFields(t *testing.T) {
	old := Item{ID: "item", Kind: "task", Title: "keep", Description: "long context", Acceptance: "criteria", Evidence: "proof", NextStep: "next", Status: "todo", Priority: "normal", ParentID: "parent"}
	in := applyChange("project", "request", ItemChange{ExternalKey: "TASK", ExpectedVersion: 4, Status: text("in_progress"), Evidence: text("")}, &old)
	if in.Title != "keep" || in.Description != "long context" || in.Acceptance != "criteria" || in.Evidence != "" || in.Status != "in_progress" {
		t.Fatalf("patch lost fields: %+v", in)
	}
}
func TestSyncErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		err   error
		code  string
		retry bool
	}{{fail(409, "conflict"), "conflict", false}, {fail(403, "denied"), "unauthorized", false}, {fail(400, "invalid"), "invalid_input", false}, {context.DeadlineExceeded, "timeout", true}, {errors.New("down"), "unavailable", true}} {
		e := syncError(tc.err)
		if e.Code != tc.code || e.Retryable != tc.retry {
			t.Fatalf("wrong classification: %+v", e)
		}
	}
}
func TestCompactLegacyReplies(t *testing.T) {
	i := Item{ID: "id", ExternalKey: "TASK", Description: strings.Repeat("secret-context", 1000), Version: 1}
	out := compactResult(i)
	b, _ := json.Marshal(out)
	if len(b) > 500 || strings.Contains(string(b), "secret-context") {
		t.Fatal("write acknowledgement includes long context")
	}
	var replay map[string]any
	raw, _ := json.Marshal(i)
	json.Unmarshal(raw, &replay)
	b, _ = json.Marshal(compactResult(replay))
	if len(b) > 500 {
		t.Fatal("idempotency replay was not compact")
	}
}
func TestMCPProcessingBudget(t *testing.T) {
	a := NewApp(nil, Config{PublicURL: "http://localhost"})
	slow := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done(); time.Sleep(30 * time.Millisecond) })
	r := httptest.NewRequest("POST", "/mcp", strings.NewReader("{}"))
	w := httptest.NewRecorder()
	start := time.Now()
	a.security(slow).ServeHTTP(w, r)
	if w.Code != 503 || time.Since(start) > 4*time.Second || !strings.Contains(w.Body.String(), "save_pending_continue") {
		t.Fatalf("unbounded or unclassified timeout: %d %s", w.Code, w.Body.String())
	}
}
func TestSharedRevisionPoller(t *testing.T) {
	var calls atomic.Int64
	ready := make(chan struct{})
	release := make(chan struct{})
	hub := newRevisionHub(func(ctx context.Context) (int64, error) {
		if calls.Add(1) == 1 {
			close(ready)
			select {
			case <-release:
			case <-ctx.Done():
				return 0, ctx.Err()
			}
		}
		return 42, nil
	}, time.Hour)
	first, closeFirst := hub.subscribe()
	second, closeSecond := hub.subscribe()
	defer closeFirst()
	defer closeSecond()
	<-ready
	close(release)
	for _, ch := range []<-chan int64{first, second} {
		select {
		case v := <-ch:
			if v != 42 {
				t.Fatal(v)
			}
		case <-time.After(time.Second):
			t.Fatal("missing shared notification")
		}
	}
	if calls.Load() != 1 {
		t.Fatal("each subscriber started its own DB poller")
	}
	closeFirst()
	hub.mu.Lock()
	remaining := len(hub.watchers)
	hub.mu.Unlock()
	if remaining != 1 {
		t.Fatal("subscriber lifecycle incorrect")
	}
	closeSecond()
	hub.mu.Lock()
	running := hub.cancel != nil
	hub.mu.Unlock()
	if running {
		t.Fatal("poller remains active without subscribers")
	}
}

func checkOptimizedStore(t *testing.T, ctx context.Context, s *Store, u User, projectID string) {
	t.Helper()
	batch := BatchInput{RequestID: newID(), ProjectID: projectID, Changes: []ItemChange{{ExternalKey: "REQ-BATCH", Kind: text("requirement"), Title: text("批量需求"), Acceptance: text("必须保留说明"), Description: text(strings.Repeat("长说明", 1000))}, {ExternalKey: "TASK-BATCH", Kind: text("task"), Title: text("子单元"), ParentKey: "REQ-BATCH", Description: text("保留我的说明")}}}
	out, err := s.SyncBatch(ctx, u, batch)
	if err != nil {
		t.Fatal(err)
	}
	created := convert[BatchResult](t, out)
	if len(created.Results) != 2 {
		t.Fatal(created)
	}
	raw, _ := json.Marshal(created)
	if len(raw) > 1500 {
		t.Fatal("batch response too large")
	}
	replay, err := s.SyncBatch(ctx, u, batch)
	if err != nil || convert[BatchResult](t, replay).Results[1].ID != created.Results[1].ID {
		t.Fatal("batch idempotency failed", err)
	}
	patch := BatchInput{RequestID: newID(), ProjectID: projectID, Changes: []ItemChange{{ExternalKey: "TASK-BATCH", ExpectedVersion: 1, Status: text("in_progress")}}}
	out, err = s.SyncBatch(ctx, u, patch)
	if err != nil {
		t.Fatal(err)
	}
	if convert[BatchResult](t, out).Results[0].Version != 2 {
		t.Fatal("version not updated")
	}
	details, err := s.ReadWorkspace(ctx, ReadInput{ProjectID: projectID, View: "details", ItemIDs: []string{created.Results[1].ID}})
	if err != nil {
		t.Fatal(err)
	}
	if details.Items.([]Item)[0].Description != "保留我的说明" {
		t.Fatal("patch wiped description")
	}
	before, err := s.ReadWorkspace(ctx, ReadInput{ProjectID: projectID})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(before)
	if strings.Contains(string(raw), "description") || strings.Contains(string(raw), "长说明") {
		t.Fatal("summary includes full business text")
	}
	patch.RequestID = newID()
	patch.Changes[0].ExpectedVersion = 2
	out, err = s.SyncBatch(ctx, u, patch)
	if err != nil {
		t.Fatal(err)
	}
	noop := convert[BatchResult](t, out).Results[0]
	if noop.Changed || noop.Version != 2 {
		t.Fatal("unchanged patch caused a new version")
	}
	after, err := s.ReadWorkspace(ctx, ReadInput{ProjectID: projectID})
	if err != nil || after.Revision != before.Revision {
		t.Fatal("no-op emitted a workspace event", err)
	}
	conditional, err := s.ReadWorkspace(ctx, ReadInput{ProjectID: projectID, IfRevision: &after.Revision, IfScope: after.Scope})
	if err != nil || !conditional.Unchanged {
		t.Fatal("conditional read failed", err)
	}
	anotherScope, err := s.ReadWorkspace(ctx, ReadInput{ProjectID: projectID, Kind: "task", IfRevision: &after.Revision, IfScope: after.Scope})
	if err != nil || anotherScope.Unchanged {
		t.Fatal("cache was incorrectly reused across query scopes", err)
	}
	page, err := s.ReadWorkspace(ctx, ReadInput{ProjectID: projectID, Limit: 1})
	if err != nil || page.NextCursor == "" {
		t.Fatal("missing pagination", err)
	}
	page2, err := s.ReadWorkspace(ctx, ReadInput{ProjectID: projectID, Limit: 1, AfterID: page.NextCursor})
	if err != nil || page.Items.([]ItemBrief)[0].ID == page2.Items.([]ItemBrief)[0].ID {
		t.Fatal("pagination repeated a record", err)
	}
	index, err := s.ReadWorkspace(ctx, ReadInput{})
	if err != nil || index.Items != nil {
		t.Fatal("unscoped query read team work items", err)
	}
	rollback := BatchInput{RequestID: newID(), ProjectID: projectID, Changes: []ItemChange{{ExternalKey: "TASK-BATCH", ExpectedVersion: 2, Title: text("不得提交")}, {ExternalKey: "REQ-BATCH", ExpectedVersion: 999, Status: text("in_progress")}}}
	if _, err = s.SyncBatch(ctx, u, rollback); err == nil {
		t.Fatal("conflicting batch was accepted")
	}
	details, err = s.ReadWorkspace(ctx, ReadInput{ProjectID: projectID, View: "details", ItemIDs: []string{created.Results[1].ID}})
	if err != nil || details.Items.([]Item)[0].Title != "子单元" || details.Items.([]Item)[0].Version != 2 {
		t.Fatal("batch was partially committed", err)
	}
	forbidden := BatchInput{RequestID: newID(), ProjectID: projectID, Changes: []ItemChange{{ExternalKey: "TASK-BATCH", ExpectedVersion: 2, Status: text("accepted"), Evidence: text("test")}}}
	if _, err = s.SyncBatch(ctx, u, forbidden); err == nil {
		t.Fatal("AI batch bypassed acceptance permission")
	}
	// Two simultaneous retries share only one idempotency record and one logical mutation.
	concurrent := BatchInput{RequestID: newID(), ProjectID: projectID, Changes: []ItemChange{{ExternalKey: "TASK-BATCH", ExpectedVersion: 2, Status: text("pending_verification")}}}
	results := make(chan error, 2)
	for n := 0; n < 2; n++ {
		go func() {
			v, err := s.SyncBatch(ctx, u, concurrent)
			if err == nil && convert[BatchResult](t, v).Results[0].Version != 3 {
				err = errors.New("duplicate request advanced version twice")
			}
			results <- err
		}()
	}
	for n := 0; n < 2; n++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	apiToken, err := s.Credential(ctx, u.ID, "api", "batch-http-test", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	apiBatch := BatchInput{RequestID: newID(), ProjectID: projectID, Changes: []ItemChange{{ExternalKey: "TASK-BATCH", ExpectedVersion: 3, Status: text("pending_acceptance"), Evidence: text("HTTP batch verified")}}}
	send := func(in BatchInput) *httptest.ResponseRecorder {
		body, _ := json.Marshal(in)
		request := httptest.NewRequest("POST", "/api/agent/sync", bytes.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+apiToken)
		response := httptest.NewRecorder()
		NewApp(s, Config{PublicURL: "http://localhost"}).Handler().ServeHTTP(response, request)
		return response
	}
	response := send(apiBatch)
	if response.Code != 200 {
		t.Fatalf("REST batch failed: %d %s", response.Code, response.Body.String())
	}
	var ack BatchResult
	if err = json.Unmarshal(response.Body.Bytes(), &ack); err != nil || ack.Results[0].Version != 4 {
		t.Fatal("invalid REST receipt", err)
	}
	apiBatch.RequestID = newID()
	apiBatch.Changes[0].ExpectedVersion = 4
	apiBatch.Changes[0].Status = text("accepted")
	if response = send(apiBatch); response.Code != 403 {
		t.Fatal("API admin credential bypassed batch acceptance rules")
	}
	browserToken, err := s.Credential(ctx, u.ID, "session", "archive-test", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/api/skill-package", nil)
	request.AddCookie(&http.Cookie{Name: "ws_session", Value: browserToken})
	response = httptest.NewRecorder()
	NewApp(s, Config{PublicURL: "http://localhost"}).Handler().ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatal("skill package failed")
	}
	archive, err := zip.NewReader(bytes.NewReader(response.Body.Bytes()), int64(response.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, file := range archive.File {
		if file.Name == "work-schedule-sync/scripts/sync_queue.py" {
			stream, _ := file.Open()
			content, _ := io.ReadAll(stream)
			stream.Close()
			found = bytes.Contains(content, []byte("process_once"))
		}
	}
	if !found {
		t.Fatal("skill archive missing actual queue helper")
	}

}

func TestLegacySummaryCannotWipeFullContext(t *testing.T) {
	old := Item{Description: "important", Evidence: "test results"}
	if !legacyWouldClear(old, ItemInput{}) {
		t.Fatal("legacy clear guard missing")
	}
	if legacyWouldClear(old, ItemInput{Description: old.Description, Evidence: old.Evidence}) {
		t.Fatal("complete legacy payload incorrectly blocked")
	}
}
