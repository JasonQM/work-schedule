package platform

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type ReadInput struct {
	ProjectID       string   `json:"project_id,omitempty"`
	IdentityKey     string   `json:"identity_key,omitempty"`
	View            string   `json:"view,omitempty"`
	Kind            string   `json:"kind,omitempty"`
	Status          string   `json:"status,omitempty"`
	ParentID        string   `json:"parent_id,omitempty"`
	ItemIDs         []string `json:"item_ids,omitempty"`
	AfterID         string   `json:"after_id,omitempty"`
	Limit           int      `json:"limit,omitempty"`
	IncludeDone     bool     `json:"include_done,omitempty"`
	IncludeSessions bool     `json:"include_sessions,omitempty"`
	IfRevision      *int64   `json:"if_revision,omitempty"`
	IfScope         string   `json:"if_scope,omitempty"`
}
type ProjectBrief struct {
	ID          string `json:"id"`
	IdentityKey string `json:"identity_key"`
	Name        string `json:"name"`
	Repository  string `json:"repository"`
}
type ItemBrief struct {
	ID          string    `json:"id"`
	ExternalKey string    `json:"external_key"`
	Kind        string    `json:"kind"`
	Title       string    `json:"title"`
	Status      string    `json:"status"`
	ParentID    string    `json:"parent_id,omitempty"`
	Version     int       `json:"version"`
	UpdatedAt   time.Time `json:"updated_at"`
}
type WorkspaceRead struct {
	Scope      string         `json:"scope"`
	Revision   int64          `json:"revision"`
	Unchanged  bool           `json:"unchanged,omitempty"`
	Project    *ProjectBrief  `json:"project,omitempty"`
	Projects   []ProjectBrief `json:"projects,omitempty"`
	Counts     map[string]int `json:"counts,omitempty"`
	Items      any            `json:"items,omitempty"`
	NextCursor string         `json:"next_cursor,omitempty"`
}

func (s *Store) ReadWorkspace(ctx context.Context, in ReadInput) (WorkspaceRead, error) {
	out := WorkspaceRead{}
	if in.View == "" {
		in.View = "summary"
	}
	if in.View != "summary" && in.View != "details" {
		return out, fail(400, "view 必须为 summary 或 details")
	}
	if in.Limit == 0 {
		in.Limit = 20
	}
	if in.Limit < 1 || in.Limit > 50 {
		return out, fail(400, "limit 必须为 1–50")
	}
	if len(in.ProjectID) > 36 || len(in.IdentityKey) > 255 || len(in.AfterID) > 36 || len(in.ParentID) > 36 || len(in.ItemIDs) > 5 {
		return out, fail(400, "查询范围过大；详情每次最多 5 条")
	}
	if in.Kind != "" && in.Kind != "task" && in.Kind != "requirement" && in.Kind != "session" {
		return out, fail(400, "无效 kind")
	}
	if len(in.Status) > 30 {
		return out, fail(400, "无效 status")
	}
	scope := in
	scope.IfRevision = nil
	scope.IfScope = ""
	out.Scope = fingerprint(scope)[:16]
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = tx.QueryRowContext(ctx, "SELECT revision FROM workspace_revision WHERE id=1").Scan(&out.Revision); err != nil {
		return out, err
	}
	if in.IdentityKey != "" {
		var id string
		err = tx.QueryRowContext(ctx, "SELECT id FROM projects WHERE identity_key=?", in.IdentityKey).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return out, fail(404, "项目不存在")
		}
		if err != nil {
			return out, err
		}
		if in.ProjectID != "" && in.ProjectID != id {
			return out, fail(400, "项目标识不匹配")
		}
		in.ProjectID = id
	}
	if in.ProjectID == "" {
		if in.View == "details" || len(in.ItemIDs) > 0 || in.ParentID != "" || in.Kind != "" || in.Status != "" {
			return out, fail(400, "读取开发记录必须指定 project_id 或 identity_key")
		}
		rows, err := tx.QueryContext(ctx, "SELECT id,identity_key,name,repository FROM projects WHERE id>? ORDER BY id LIMIT ?", in.AfterID, in.Limit+1)
		if err != nil {
			return out, err
		}
		out.Projects = []ProjectBrief{}
		for rows.Next() {
			var p ProjectBrief
			if err = rows.Scan(&p.ID, &p.IdentityKey, &p.Name, &p.Repository); err != nil {
				rows.Close()
				return out, err
			}
			out.Projects = append(out.Projects, p)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return out, err
		}
		if len(out.Projects) > in.Limit {
			out.Projects = out.Projects[:in.Limit]
			out.NextCursor = out.Projects[len(out.Projects)-1].ID
		}
		return out, tx.Commit()
	}
	var p ProjectBrief
	if err = tx.QueryRowContext(ctx, "SELECT id,identity_key,name,repository FROM projects WHERE id=?", in.ProjectID).Scan(&p.ID, &p.IdentityKey, &p.Name, &p.Repository); errors.Is(err, sql.ErrNoRows) {
		return out, fail(404, "项目不存在")
	} else if err != nil {
		return out, err
	}
	out.Project = &p
	if in.IfRevision != nil && in.IfScope == out.Scope && *in.IfRevision == out.Revision {
		out.Unchanged = true
		return out, tx.Commit()
	}
	if in.View == "details" {
		if len(in.ItemIDs) == 0 {
			return out, fail(400, "details 必须指定 item_ids，最多 5 条")
		}
		args := []any{in.ProjectID}
		placeholders := []string{}
		for _, id := range in.ItemIDs {
			if len(id) > 36 {
				return out, fail(400, "无效工作项 ID")
			}
			args = append(args, id)
			placeholders = append(placeholders, "?")
		}
		rows, err := tx.QueryContext(ctx, "SELECT id,project_id,external_key,kind,title,description,status,priority,COALESCE(parent_id,''),version,acceptance,evidence,next_step,actor,created_at,updated_at FROM items WHERE project_id=? AND id IN ("+strings.Join(placeholders, ",")+") ORDER BY id", args...)
		if err != nil {
			return out, err
		}
		items := []Item{}
		for rows.Next() {
			var i Item
			if err = scanItem(rows, &i); err != nil {
				rows.Close()
				return out, err
			}
			items = append(items, i)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return out, err
		}
		out.Items = items
		return out, tx.Commit()
	}
	if len(in.ItemIDs) > 0 {
		return out, fail(400, "item_ids 仅用于 details")
	}
	out.Counts = map[string]int{}
	rows, err := tx.QueryContext(ctx, "SELECT kind,status,COUNT(*) FROM items WHERE project_id=? GROUP BY kind,status", in.ProjectID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var kind, status string
		var count int
		if err = rows.Scan(&kind, &status, &count); err != nil {
			rows.Close()
			return out, err
		}
		out.Counts[kind+"."+status] = count
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	q := "SELECT id,external_key,kind,title,status,COALESCE(parent_id,''),version,updated_at FROM items WHERE project_id=? AND id>?"
	args := []any{in.ProjectID, in.AfterID}
	if !in.IncludeDone && in.Status == "" {
		q += " AND status NOT IN ('accepted','cancelled','ended')"
	}
	if !in.IncludeSessions && in.Kind != "session" {
		q += " AND kind<>'session'"
	}
	for _, f := range []struct{ column, value string }{{"kind", in.Kind}, {"status", in.Status}, {"parent_id", in.ParentID}} {
		if f.value != "" {
			q += " AND " + f.column + "=?"
			args = append(args, f.value)
		}
	}
	q += " ORDER BY id LIMIT ?"
	args = append(args, in.Limit+1)
	rows, err = tx.QueryContext(ctx, q, args...)
	if err != nil {
		return out, err
	}
	items := []ItemBrief{}
	for rows.Next() {
		var i ItemBrief
		if err = rows.Scan(&i.ID, &i.ExternalKey, &i.Kind, &i.Title, &i.Status, &i.ParentID, &i.Version, &i.UpdatedAt); err != nil {
			rows.Close()
			return out, err
		}
		items = append(items, i)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if len(items) > in.Limit {
		items = items[:in.Limit]
		out.NextCursor = items[len(items)-1].ID
	}
	out.Items = items
	return out, tx.Commit()
}
