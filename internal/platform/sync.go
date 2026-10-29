package platform

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Pointer fields distinguish an omitted field from an explicit empty value.
type ItemChange struct {
	ExternalKey     string  `json:"external_key"`
	ExpectedVersion int     `json:"expected_version"`
	ID              string  `json:"id,omitempty"`
	Kind            *string `json:"kind,omitempty"`
	Title           *string `json:"title,omitempty"`
	Description     *string `json:"description,omitempty"`
	Status          *string `json:"status,omitempty"`
	Priority        *string `json:"priority,omitempty"`
	ParentID        *string `json:"parent_id,omitempty"`
	ParentKey       string  `json:"parent_key,omitempty"`
	Acceptance      *string `json:"acceptance,omitempty"`
	Evidence        *string `json:"evidence,omitempty"`
	NextStep        *string `json:"next_step,omitempty"`
}
type BatchInput struct {
	RequestID string       `json:"request_id"`
	ProjectID string       `json:"project_id"`
	Changes   []ItemChange `json:"changes"`
}
type ItemAck struct {
	ID          string    `json:"id"`
	ExternalKey string    `json:"external_key"`
	Kind        string    `json:"kind"`
	Status      string    `json:"status"`
	Version     int       `json:"version"`
	Changed     bool      `json:"changed"`
	UpdatedAt   time.Time `json:"updated_at"`
}
type BatchResult struct {
	RequestID string    `json:"request_id"`
	ProjectID string    `json:"project_id"`
	Results   []ItemAck `json:"results"`
}

func itemAck(i Item, changed bool) ItemAck {
	return ItemAck{i.ID, i.ExternalKey, i.Kind, i.Status, i.Version, changed, i.UpdatedAt}
}
func sameItem(i Item, in ItemInput) bool {
	return i.Title == in.Title && i.Description == in.Description && i.Status == in.Status && i.Priority == in.Priority && i.Acceptance == in.Acceptance && i.Evidence == in.Evidence && i.NextStep == in.NextStep && i.Kind == in.Kind && i.ParentID == in.ParentID
}
func findItem(ctx context.Context, tx *sql.Tx, project, key string) (*Item, error) {
	var i Item
	err := scanItem(tx.QueryRowContext(ctx, "SELECT id,project_id,external_key,kind,title,description,status,priority,COALESCE(parent_id,''),version,acceptance,evidence,next_step,actor,created_at,updated_at FROM items WHERE project_id=? AND external_key=? FOR UPDATE", project, key), &i)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &i, nil
}
func applyChange(project, request string, c ItemChange, old *Item) ItemInput {
	in := ItemInput{RequestID: request, ProjectID: project, ExternalKey: c.ExternalKey, ExpectedVersion: c.ExpectedVersion, ID: c.ID, Status: "todo", Priority: "normal"}
	if old != nil {
		in.ID = old.ID
		in.Kind = old.Kind
		in.Title = old.Title
		in.Description = old.Description
		in.Status = old.Status
		in.Priority = old.Priority
		in.ParentID = old.ParentID
		in.Acceptance = old.Acceptance
		in.Evidence = old.Evidence
		in.NextStep = old.NextStep
		if c.ID != "" {
			in.ID = c.ID
		}
	}
	fields := []struct {
		src *string
		dst *string
	}{{c.Kind, &in.Kind}, {c.Title, &in.Title}, {c.Description, &in.Description}, {c.Status, &in.Status}, {c.Priority, &in.Priority}, {c.ParentID, &in.ParentID}, {c.Acceptance, &in.Acceptance}, {c.Evidence, &in.Evidence}, {c.NextStep, &in.NextStep}}
	for _, f := range fields {
		if f.src != nil {
			*f.dst = *f.src
		}
	}
	return in
}
func (s *Store) SyncBatch(ctx context.Context, u User, in BatchInput) (any, error) {
	if len(in.Changes) < 1 || len(in.Changes) > 20 {
		return nil, fail(400, "changes 必须包含 1–20 个开发记录")
	}
	if strings.TrimSpace(in.ProjectID) == "" || len(in.ProjectID) > 36 {
		return nil, fail(400, "project_id 必填且不能超过 36 字节")
	}
	seen := map[string]bool{}
	for _, c := range in.Changes {
		if strings.TrimSpace(c.ExternalKey) == "" || len(c.ExternalKey) > 160 || seen[c.ExternalKey] {
			return nil, fail(400, "external_key 必填、长度不超过 160，且同批次不能重复")
		}
		seen[c.ExternalKey] = true
		if c.ParentKey != "" && (c.ParentID != nil || len(c.ParentKey) > 160) {
			return nil, fail(400, "parent_key 与 parent_id 不能同时指定")
		}
	}
	// One atomic transaction and idempotency record for the whole bounded batch.
	return s.mutate(ctx, u, in.RequestID, struct {
		Operation string
		Input     BatchInput
	}{"sync_work_batch", in}, func(tx *sql.Tx) (any, error) {
		var id string
		if err := tx.QueryRowContext(ctx, "SELECT id FROM projects WHERE id=? FOR UPDATE", in.ProjectID).Scan(&id); errors.Is(err, sql.ErrNoRows) {
			return nil, fail(404, "项目不存在")
		} else if err != nil {
			return nil, err
		}
		result := BatchResult{in.RequestID, in.ProjectID, []ItemAck{}}
		for _, change := range in.Changes {
			old, err := findItem(ctx, tx, in.ProjectID, change.ExternalKey)
			if err != nil {
				return nil, err
			}
			input := applyChange(in.ProjectID, in.RequestID, change, old)
			if change.ParentKey != "" {
				parent, err := findItem(ctx, tx, in.ProjectID, change.ParentKey)
				if err != nil {
					return nil, err
				}
				if parent == nil || parent.Kind != "requirement" {
					return nil, fail(400, "parent_key 必须指向已有或同批次前面创建的需求")
				}
				input.ParentID = parent.ID
			}
			if err = input.Validate(u); err != nil {
				return nil, err
			}
			out, err := s.putItemTx(ctx, tx, u, input, true)
			if err != nil {
				return nil, err
			}
			item := out.(Item)
			result.Results = append(result.Results, itemAck(item, old == nil || old.Version != item.Version))
		}
		return result, nil
	})
}

func legacyWouldClear(old Item, in ItemInput) bool {
	return old.Description != "" && in.Description == "" || old.Acceptance != "" && in.Acceptance == "" || old.Evidence != "" && in.Evidence == "" || old.NextStep != "" && in.NextStep == ""
}
