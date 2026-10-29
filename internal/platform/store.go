package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/JasonQM/work-schedule/migrations"
	mysql "github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/bcrypt"
)

type Store struct{ DB *sql.DB }

func Open(ctx context.Context, dsn string) (*Store, error) {
	c, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, err
	}
	c.ParseTime = true
	c.Loc = time.UTC
	c.MultiStatements = false
	c.Timeout = 5 * time.Second
	c.ReadTimeout = 10 * time.Second
	c.WriteTimeout = 10 * time.Second
	db, err := sql.Open("mysql", c.FormatDSN())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(3)
	db.SetConnMaxLifetime(3 * time.Minute)
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db}, nil
}
func (s *Store) Migrate(ctx context.Context) error {
	// Serialize schema setup across accidental simultaneous starts. DDL is restartable.
	conn, err := s.DB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var locked int
	if err = conn.QueryRowContext(ctx, "SELECT GET_LOCK('work_schedule_migration',10)").Scan(&locked); err != nil {
		return err
	}
	if locked != 1 {
		return errors.New("migration lock timeout")
	}
	defer conn.ExecContext(context.Background(), "SELECT RELEASE_LOCK('work_schedule_migration')")
	for _, q := range strings.Split(migrations.Initial, ";") {
		if strings.TrimSpace(q) != "" {
			if _, err = conn.ExecContext(ctx, q); err != nil {
				return err
			}
		}
	}
	_, err = conn.ExecContext(ctx, "INSERT IGNORE INTO schema_migrations(version,applied_at) VALUES(1,UTC_TIMESTAMP(6))")
	return err
}
func (s *Store) Bootstrap(ctx context.Context, email, password string) error {
	var n int
	if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	if email == "" || len(password) < 12 || len(password) > 72 {
		return errors.New("empty database: configure BOOTSTRAP_EMAIL and BOOTSTRAP_PASSWORD (12–72 bytes)")
	}
	_, err := s.CreateUser(ctx, email, "管理员", password, "admin")
	return err
}
func (s *Store) CreateUser(ctx context.Context, email, name, password, role string) (User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if len(password) < 12 || len(password) > 72 || !strings.Contains(email, "@") || len(email) > 190 || len(name) > 120 || strings.TrimSpace(name) == "" {
		return User{}, fail(400, "姓名、邮箱及 12–72 字节密码必填")
	}
	if role != "admin" && role != "member" && role != "viewer" {
		return User{}, fail(400, "无效角色")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}
	u := User{newID(), email, name, role}
	_, err = s.DB.ExecContext(ctx, "INSERT INTO users(id,email,name,password_hash,role,created_at) VALUES(?,?,?,?,?,UTC_TIMESTAMP(6))", u.ID, u.Email, u.Name, string(hash), u.Role)
	if isDuplicate(err) {
		return User{}, fail(409, "邮箱已存在")
	}
	return u, err
}
func (s *Store) Login(ctx context.Context, email, password string) (string, error) {
	var id, hash string
	err := s.DB.QueryRowContext(ctx, "SELECT id,password_hash FROM users WHERE email=?", strings.ToLower(strings.TrimSpace(email))).Scan(&id, &hash)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return "", fail(401, "邮箱或密码错误")
	}
	return s.Credential(ctx, id, "session", "浏览器会话", 24*time.Hour)
}
func (s *Store) Credential(ctx context.Context, userID, kind, name string, ttl time.Duration) (string, error) {
	if len(name) > 120 || strings.TrimSpace(name) == "" {
		return "", fail(400, "凭据名称必填且不能超过 120 字节")
	}
	token := secret()
	_, err := s.DB.ExecContext(ctx, "INSERT INTO credentials(hash,user_id,kind,name,expires_at,created_at) VALUES(?,?,?,?,?,UTC_TIMESTAMP(6))", digest(token), userID, kind, name, time.Now().UTC().Add(ttl))
	return token, err
}
func (s *Store) Authenticate(ctx context.Context, token, kind string) (User, error) {
	var u User
	err := s.DB.QueryRowContext(ctx, "SELECT u.id,u.email,u.name,u.role FROM users u JOIN credentials c ON c.user_id=u.id WHERE c.hash=? AND c.kind=? AND c.expires_at>UTC_TIMESTAMP(6)", digest(token), kind).Scan(&u.ID, &u.Email, &u.Name, &u.Role)
	if errors.Is(err, sql.ErrNoRows) {
		return u, fail(401, "请登录或检查访问凭据")
	}
	return u, err
}
func (s *Store) Snapshot(ctx context.Context, u User, projectID string) (Snapshot, error) {
	out := Snapshot{User: u, Projects: []Project{}, Items: []Item{}, Events: []Event{}}
	// One consistent snapshot prevents a newer event cursor from skipping unseen changes.
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = tx.QueryRowContext(ctx, "SELECT revision FROM workspace_revision WHERE id=1").Scan(&out.Cursor); err != nil {
		return out, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT id,identity_key,name,summary,repository,created_at,updated_at FROM projects ORDER BY updated_at DESC LIMIT 201")
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var p Project
		if err = rows.Scan(&p.ID, &p.IdentityKey, &p.Name, &p.Summary, &p.Repository, &p.CreatedAt, &p.UpdatedAt); err != nil {
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
	if len(out.Projects) > 200 {
		return out, fail(422, "项目数量超过第一版单页限制，需要启用分页")
	}
	q := "SELECT id,project_id,external_key,kind,title,description,status,priority,COALESCE(parent_id,''),version,acceptance,evidence,next_step,actor,created_at,updated_at FROM items"
	args := []any{}
	if projectID != "" {
		q += " WHERE project_id=?"
		args = append(args, projectID)
	}
	q += " ORDER BY updated_at DESC LIMIT 2001"
	rows, err = tx.QueryContext(ctx, q, args...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var i Item
		if err = scanItem(rows, &i); err != nil {
			rows.Close()
			return out, err
		}
		out.Items = append(out.Items, i)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if len(out.Items) > 2000 {
		return out, fail(422, "记录超过第一版读取限制，请指定项目或升级分页能力")
	}
	q = "SELECT sequence,project_id,COALESCE(item_id,''),actor,action,summary,created_at FROM events"
	if projectID != "" {
		q += " WHERE project_id=?"
	}
	q += " ORDER BY sequence DESC LIMIT 100"
	rows, err = tx.QueryContext(ctx, q, args...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var e Event
		if err = rows.Scan(&e.Sequence, &e.ProjectID, &e.ItemID, &e.Actor, &e.Action, &e.Summary, &e.CreatedAt); err != nil {
			rows.Close()
			return out, err
		}
		out.Events = append(out.Events, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}

type scanner interface{ Scan(...any) error }

func scanItem(r scanner, i *Item) error {
	return r.Scan(&i.ID, &i.ProjectID, &i.ExternalKey, &i.Kind, &i.Title, &i.Description, &i.Status, &i.Priority, &i.ParentID, &i.Version, &i.Acceptance, &i.Evidence, &i.NextStep, &i.Actor, &i.CreatedAt, &i.UpdatedAt)
}
func isDuplicate(err error) bool {
	var e *mysql.MySQLError
	return errors.As(err, &e) && e.Number == 1062
}
func (s *Store) mutate(ctx context.Context, u User, requestID string, input any, fn func(*sql.Tx) (any, error)) (any, error) {
	if u.Role == "viewer" {
		return nil, fail(403, "只读账号不能修改项目")
	}
	if requestID == "" || len(requestID) > 100 {
		return nil, fail(400, "request_id 必填且不能超过 100 字节")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Reserve only this idempotency key; independent projects/users no longer queue on a user row.
	fp := fingerprint(input)
	_, err = tx.ExecContext(ctx, "INSERT INTO requests(user_id,request_id,fingerprint,response,created_at) VALUES(?,?,?,'null',UTC_TIMESTAMP(6)) ON DUPLICATE KEY UPDATE request_id=VALUES(request_id)", u.ID, requestID, fp)
	if err != nil {
		return nil, err
	}
	var oldFP string
	var result []byte
	if err = tx.QueryRowContext(ctx, "SELECT fingerprint,response FROM requests WHERE user_id=? AND request_id=? FOR UPDATE", u.ID, requestID).Scan(&oldFP, &result); err != nil {
		return nil, err
	}
	if oldFP != fp {
		return nil, fail(409, "request_id 已用于不同操作，请使用新的请求 ID")
	}
	if string(result) != "null" {
		var out any
		err = json.Unmarshal(result, &out)
		return out, err
	}
	out, err := fn(tx)
	if err != nil {
		return nil, err
	}
	result, err = json.Marshal(out)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, "UPDATE requests SET response=? WHERE user_id=? AND request_id=?", string(result), u.ID, requestID)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}
func event(ctx context.Context, tx *sql.Tx, u User, projectID, itemID, action, summary string, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO events(id,project_id,item_id,actor,action,summary,payload,created_at) VALUES(?,?,NULLIF(?,''),?,?,?,?,UTC_TIMESTAMP(6))", newID(), projectID, itemID, u.Name, action, summary, string(b))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE workspace_revision SET revision=revision+1 WHERE id=1")
	return err
}
func (s *Store) EnsureProject(ctx context.Context, u User, in ProjectInput) (any, error) {
	in.IdentityKey = strings.TrimSpace(in.IdentityKey)
	in.Name = strings.TrimSpace(in.Name)
	if in.IdentityKey == "" || in.Name == "" || len(in.IdentityKey) > 255 || len(in.Name) > 160 || len(in.Summary) > 32000 || len(in.Repository) > 500 {
		return nil, fail(400, "项目标识和名称必填，且字段长度必须符合限制")
	}
	return s.mutate(ctx, u, in.RequestID, in, func(tx *sql.Tx) (any, error) {
		p := Project{newID(), in.IdentityKey, in.Name, in.Summary, in.Repository, time.Now().UTC(), time.Now().UTC()}
		// Unique identity handles simultaneous registration by different users.
		_, err := tx.ExecContext(ctx, "INSERT INTO projects(id,identity_key,name,summary,repository,created_at,updated_at) VALUES(?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE identity_key=VALUES(identity_key)", p.ID, p.IdentityKey, p.Name, p.Summary, p.Repository, p.CreatedAt, p.UpdatedAt)
		if err != nil {
			return nil, err
		}
		var found Project
		err = tx.QueryRowContext(ctx, "SELECT id,identity_key,name,summary,repository,created_at,updated_at FROM projects WHERE identity_key=? FOR UPDATE", in.IdentityKey).Scan(&found.ID, &found.IdentityKey, &found.Name, &found.Summary, &found.Repository, &found.CreatedAt, &found.UpdatedAt)
		if err != nil {
			return nil, err
		}
		if found.ID == p.ID {
			err = event(ctx, tx, u, p.ID, "", "project.created", "自动接入项目："+p.Name, p)
		}
		return found, err
	})
}
func (s *Store) PutItem(ctx context.Context, u User, in ItemInput) (any, error) {
	if err := in.Validate(u); err != nil {
		return nil, err
	}
	return s.mutate(ctx, u, in.RequestID, in, func(tx *sql.Tx) (any, error) { return s.putItemTx(ctx, tx, u, in, false) })
}
func (s *Store) putItemTx(ctx context.Context, tx *sql.Tx, u User, in ItemInput, projectLocked bool) (any, error) {
	if !projectLocked {
		var project string
		if err := tx.QueryRowContext(ctx, "SELECT id FROM projects WHERE id=? FOR UPDATE", in.ProjectID).Scan(&project); errors.Is(err, sql.ErrNoRows) {
			return nil, fail(404, "项目不存在")
		} else if err != nil {
			return nil, err
		}
	}

	if in.ParentID != "" {
		var kind string
		err := tx.QueryRowContext(ctx, "SELECT kind FROM items WHERE id=? AND project_id=?", in.ParentID, in.ProjectID).Scan(&kind)
		if errors.Is(err, sql.ErrNoRows) || err == nil && kind != "requirement" {
			return nil, fail(400, "父项必须是当前项目的需求")
		}
		if err != nil {
			return nil, err
		}
	}
	var old Item
	err := scanItem(tx.QueryRowContext(ctx, "SELECT id,project_id,external_key,kind,title,description,status,priority,COALESCE(parent_id,''),version,acceptance,evidence,next_step,actor,created_at,updated_at FROM items WHERE project_id=? AND external_key=? FOR UPDATE", in.ProjectID, in.ExternalKey), &old)
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if !exists && (in.ID != "" || in.ExpectedVersion != 0) {
		return nil, fail(409, "记录不存在，请重新读取项目")
	}
	if (in.Status == "accepted" || in.Status == "rejected") && (!exists || old.Status != "pending_acceptance") {
		return nil, fail(409, "只能对待验收记录作出验收决定")
	}
	now := time.Now().UTC()
	i := Item{newID(), in.ProjectID, in.ExternalKey, in.Kind, in.Title, in.Description, in.Status, in.Priority, in.ParentID, 1, in.Acceptance, in.Evidence, in.NextStep, u.Name, now, now}
	action := "item.created"
	if exists {
		if in.ID != old.ID || in.ExpectedVersion != old.Version {
			return nil, fail(409, "版本冲突，请读取最新记录，合并后使用新的 request_id 重试")
		}
		if old.Kind != in.Kind || old.ParentID != in.ParentID {
			return nil, fail(400, "已有记录不能更改类型或父需求")
		}
		if !projectLocked && !in.AllowClear && legacyWouldClear(old, in) {
			return nil, fail(400, "旧完整更新将清空已有说明，请读取详情；有意清空请用批量局部更新或 allow_clear=true")
		}
		if sameItem(old, in) {
			return old, nil
		}

		i.ID = old.ID
		i.Version = old.Version + 1
		i.CreatedAt = old.CreatedAt
		action = "item.updated"
		// Semantic requirement changes invalidate prior acceptance and all dependent task evidence.
		if i.Kind == "requirement" && (i.Title != old.Title || i.Description != old.Description || i.Acceptance != old.Acceptance) {
			i.Status = "needs_review"
			i.Evidence = ""
			action = "requirement.changed"
			_, err = tx.ExecContext(ctx, "UPDATE items SET status='needs_review',evidence='',version=version+1,updated_at=?,actor=? WHERE parent_id=? AND status<>'cancelled'", now, u.Name, i.ID)
			if err != nil {
				return nil, err
			}
		}
		if i.Status == "accepted" && i.Kind == "requirement" {
			var remaining int
			err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM items WHERE parent_id=? AND status NOT IN ('accepted','cancelled')", i.ID).Scan(&remaining)
			if err != nil {
				return nil, err
			}
			if remaining > 0 {
				return nil, fail(409, "仍有未验收的开发子任务，不能通过需求验收")
			}
		}
		_, err = tx.ExecContext(ctx, "UPDATE items SET title=?,description=?,status=?,priority=?,version=?,acceptance=?,evidence=?,next_step=?,actor=?,updated_at=? WHERE id=?", i.Title, i.Description, i.Status, i.Priority, i.Version, i.Acceptance, i.Evidence, i.NextStep, i.Actor, now, i.ID)
	} else {
		_, err = tx.ExecContext(ctx, "INSERT INTO items(id,project_id,external_key,kind,title,description,status,priority,parent_id,version,acceptance,evidence,next_step,actor,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,NULLIF(?,''),?,?,?,?,?,?,?)", i.ID, i.ProjectID, i.ExternalKey, i.Kind, i.Title, i.Description, i.Status, i.Priority, i.ParentID, i.Version, i.Acceptance, i.Evidence, i.NextStep, i.Actor, i.CreatedAt, i.UpdatedAt)
	}
	if err != nil {
		return nil, err
	}
	if i.Kind == "task" && i.Status != "accepted" && i.Status != "cancelled" {
		_, err = tx.ExecContext(ctx, "UPDATE items SET status='needs_review',evidence='',version=version+1,updated_at=?,actor=? WHERE id=? AND status='accepted'", now, u.Name, i.ParentID)
		if err != nil {
			return nil, err
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE projects SET updated_at=? WHERE id=?", now, i.ProjectID); err != nil {
		return nil, err
	}
	err = event(ctx, tx, u, i.ProjectID, i.ID, action, i.Title, map[string]any{"before": old, "after": i})
	return i, err
}

func (s *Store) ChangePassword(ctx context.Context, u User, current, next, currentSession string) error {
	if len(next) < 12 || len(next) > 72 || len(current) > 72 {
		return fail(400, "新密码必须为 12–72 字节")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(next), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var old string
	if err = tx.QueryRowContext(ctx, "SELECT password_hash FROM users WHERE id=? FOR UPDATE", u.ID).Scan(&old); err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(old), []byte(current)) != nil {
		return fail(400, "当前密码不正确")
	}
	if _, err = tx.ExecContext(ctx, "UPDATE users SET password_hash=? WHERE id=?", string(hash), u.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM credentials WHERE user_id=? AND hash<>?", u.ID, digest(currentSession)); err != nil {
		return err
	}
	return tx.Commit()
}
