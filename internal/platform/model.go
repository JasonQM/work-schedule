package platform

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type User struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
}
type Project struct {
	ID          string    `json:"id"`
	IdentityKey string    `json:"identity_key"`
	Name        string    `json:"name"`
	Summary     string    `json:"summary"`
	Repository  string    `json:"repository"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
type Item struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	ExternalKey string    `json:"external_key"`
	Kind        string    `json:"kind"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	Priority    string    `json:"priority"`
	ParentID    string    `json:"parent_id"`
	Version     int       `json:"version"`
	Acceptance  string    `json:"acceptance"`
	Evidence    string    `json:"evidence"`
	NextStep    string    `json:"next_step"`
	Actor       string    `json:"actor"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
type Event struct {
	Sequence  int64     `json:"sequence"`
	ProjectID string    `json:"project_id"`
	ItemID    string    `json:"item_id"`
	Actor     string    `json:"actor"`
	Action    string    `json:"action"`
	Summary   string    `json:"summary"`
	CreatedAt time.Time `json:"created_at"`
}
type Snapshot struct {
	Projects []Project `json:"projects"`
	Items    []Item    `json:"items"`
	Events   []Event   `json:"events"`
	Cursor   int64     `json:"cursor"`
	User     User      `json:"user"`
}
type ProjectInput struct {
	RequestID   string `json:"request_id"`
	IdentityKey string `json:"identity_key"`
	Name        string `json:"name"`
	Summary     string `json:"summary"`
	Repository  string `json:"repository"`
}
type ItemInput struct {
	AllowClear      bool   `json:"allow_clear,omitempty"`
	RequestID       string `json:"request_id"`
	ProjectID       string `json:"project_id"`
	ID              string `json:"id,omitempty"`
	ExternalKey     string `json:"external_key"`
	ExpectedVersion int    `json:"expected_version"`
	Kind            string `json:"kind"`
	Title           string `json:"title"`
	Description     string `json:"description"`
	Status          string `json:"status"`
	Priority        string `json:"priority"`
	ParentID        string `json:"parent_id"`
	Acceptance      string `json:"acceptance"`
	Evidence        string `json:"evidence"`
	NextStep        string `json:"next_step"`
}
type AppError struct {
	Code    int
	Message string
}

func (e *AppError) Error() string     { return e.Message }
func fail(code int, msg string) error { return &AppError{code, msg} }
func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func secret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func digest(s string) string   { b := sha256.Sum256([]byte(s)); return hex.EncodeToString(b[:]) }
func fingerprint(v any) string { b, _ := json.Marshal(v); return digest(string(b)) }
func (in *ItemInput) Validate(u User) error {
	for _, v := range []struct {
		s   string
		max int
	}{{in.RequestID, 100}, {in.ExternalKey, 160}, {in.Title, 200}, {in.ProjectID, 36}, {in.ParentID, 36}, {in.ID, 36}} {
		if len(v.s) > v.max {
			return fail(400, "字段过长")
		}
	}
	if strings.TrimSpace(in.RequestID) == "" || strings.TrimSpace(in.ProjectID) == "" || strings.TrimSpace(in.Title) == "" || strings.TrimSpace(in.ExternalKey) == "" {
		return fail(400, "request_id、project_id、external_key、title 必填")
	}
	if len(in.Description) > 32000 || len(in.Acceptance) > 16000 || len(in.Evidence) > 16000 || len(in.NextStep) > 8000 {
		return fail(400, "内容超出长度限制")
	}
	if in.Kind != "requirement" && in.Kind != "task" && in.Kind != "session" {
		return fail(400, "kind 必须是 requirement、task 或 session")
	}
	if in.Priority == "" {
		in.Priority = "normal"
	}
	if in.Priority != "high" && in.Priority != "normal" && in.Priority != "low" {
		return fail(400, "无效优先级")
	}
	valid := map[string]bool{"todo": true, "in_progress": true, "blocked": true, "pending_verification": true, "pending_acceptance": true, "accepted": true, "rejected": true, "needs_review": true, "cancelled": true, "ended": true}
	if !valid[in.Status] {
		return fail(400, "无效状态")
	}
	if in.Kind == "session" {
		if in.Status != "in_progress" && in.Status != "blocked" && in.Status != "ended" {
			return fail(400, "开发会话仅支持 in_progress、blocked、ended")
		}
		if in.ParentID != "" {
			return fail(400, "开发会话不能设置父项")
		}
	} else if in.Status == "ended" {
		return fail(400, "ended 仅用于开发会话")
	}
	if in.Kind == "requirement" && in.ParentID != "" {
		return fail(400, "需求不能设置父项")
	}
	if in.Kind == "task" && in.ParentID == "" {
		return fail(400, "开发任务必须关联需求")
	}
	if in.Kind == "requirement" && strings.TrimSpace(in.Acceptance) == "" {
		return fail(400, "需求必须包含验收标准；不确定时明确记录待确认内容")
	}
	if in.Status == "accepted" && u.Role != "admin" {
		return fail(403, "验收通过必须由管理员确认，AI 开发凭据不能自行验收")
	}
	if (in.Status == "accepted" || in.Status == "pending_acceptance") && strings.TrimSpace(in.Evidence) == "" {
		return fail(400, "提交验收必须提供验证依据")
	}
	if in.Status == "rejected" && strings.TrimSpace(in.NextStep) == "" {
		return fail(400, "验收未通过必须说明返工事项")
	}
	if in.Status == "blocked" && strings.TrimSpace(in.NextStep) == "" {
		return fail(400, "阻塞时必须说明原因及下一步")
	}
	return nil
}
