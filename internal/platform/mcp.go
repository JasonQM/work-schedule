package platform

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	mysql "github.com/go-sql-driver/mysql"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type SyncError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
	Action    string `json:"action"`
}

func syncError(err error) SyncError {
	out := SyncError{"unavailable", "同步暂不可用，保存待同步记录后继续开发", true, "save_pending_continue"}
	var e *AppError
	if errors.As(err, &e) {
		out.Message = e.Message
		out.Retryable = e.Code >= 500
		switch e.Code {
		case 400, 422:
			out.Code = "invalid_input"
			out.Action = "fix_input"
		case 401, 403:
			out.Code = "unauthorized"
			out.Action = "check_credentials"
		case 404:
			out.Code = "not_found"
			out.Action = "refresh_target"
		case 409:
			out.Code = "conflict"
			out.Action = "refresh_target_and_merge"
		default:
			out.Action = "save_pending_continue"
		}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		out.Code = "timeout"
		out.Message = "同步预算已耗尽，暂存后继续开发；稍后使用相同 request_id 补传"
	}
	var db *mysql.MySQLError
	if errors.As(err, &db) && (db.Number == 1213 || db.Number == 1205) {
		out.Code = "busy"
		out.Message = "记录正在被其他会话更新，暂存后稍后补传"
	}
	return out
}
func compactResult(out any) any {
	switch v := out.(type) {
	case Project:
		return ProjectBrief{v.ID, v.IdentityKey, v.Name, v.Repository}
	case Item:
		return map[string]any{"id": v.ID, "external_key": v.ExternalKey, "kind": v.Kind, "status": v.Status, "version": v.Version, "updated_at": v.UpdatedAt}
	case map[string]any:
		if _, ok := v["external_key"]; ok {
			r := map[string]any{}
			for _, k := range []string{"id", "external_key", "kind", "status", "version", "updated_at"} {
				r[k] = v[k]
			}
			return r
		}
		if _, ok := v["identity_key"]; ok {
			r := map[string]any{}
			for _, k := range []string{"id", "identity_key", "name", "repository"} {
				r[k] = v[k]
			}
			return r
		}
	}
	return out
}
func toolResult(out any, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		e := syncError(err)
		out = map[string]any{"error": e}
		b, _ := json.Marshal(out)
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}, StructuredContent: out}, nil, nil
	}
	out = compactResult(out)
	b, _ := json.Marshal(out)
	// Text fallback is retained for older clients; business data itself is now bounded and compact.
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}, StructuredContent: out}, nil, nil
}
func (a *App) mcpHandler() http.Handler {
	server := mcp.NewServer(&mcp.Implementation{Name: "work-schedule", Version: "0.2.0"}, &mcp.ServerOptions{Instructions: "低干扰同步：复用项目标识，按项目读取分页概要，仅在需要时取单条详情；在有效开发节点合并成批次，不为每条命令上报。同步失败停止当前轮重试，暂存后继续开发。写入需 request_id 和 expected_version；冲突只读目标并合并，不能盲目覆盖。AI 不能自行验收。"})
	mcp.AddTool(server, &mcp.Tool{Name: "ensure_project", Description: "首次按稳定 identity_key 登记或查找项目，返回简短项目标识。已知 project_id 后复用，不要在每个步骤重复登记。"}, func(ctx context.Context, _ *mcp.CallToolRequest, in ProjectInput) (*mcp.CallToolResult, any, error) {
		out, err := a.Store.EnsureProject(ctx, actor(ctx), in)
		return toolResult(out, err)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "read_workspace", Description: "轻量读取。未指定项目仅列项目，不读团队工作记录。project_id 或 identity_key 限定项目；默认 summary 返回计数与最多20条概要，不含长文或历史。limit上限50，next_cursor用作after_id分页；可按kind/status/parent_id筛选，默认排除已结束/取消/验收及session。view=details配item_ids按需取最多5条完整详情。if_revision和if_scope均匹配上次revision/scope时只返回unchanged。禁止为每次写入反复读取整个项目。"}, func(ctx context.Context, _ *mcp.CallToolRequest, in ReadInput) (*mcp.CallToolResult, any, error) {
		out, err := a.Store.ReadWorkspace(ctx, in)
		return toolResult(out, err)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "sync_work_item", Description: "兼容旧单条完整更新；优先使用sync_work_batch局部字段批量同步。所有写入需request_id，更新需id和expected_version；AI不可设置accepted。成功只返回id/状态/版本，不回传长文。清空已有长文需显式allow_clear=true，避免概要被误当作完整记录覆盖。"}, func(ctx context.Context, _ *mcp.CallToolRequest, in ItemInput) (*mcp.CallToolResult, any, error) {
		out, err := a.Store.PutItem(ctx, actor(ctx), in)
		return toolResult(out, err)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "sync_work_batch", Description: "推荐：同一项目1–20条原子批量创建/局部更新。批次request_id重试需完全相同参数；更新必须expected_version，未传字段保留，空字符串显式清空。新建expected_version=0且提供kind/title等必要字段。task关联parent_id或parent_key；parent_key可指向本批前面创建的需求。相同external_key不能重复。无变化不产生新版本。成功仅返回简短逐项确认，任一项失败整批回滚。权限与验收规则与单条相同；失败后不要循环重试阻塞开发。"}, func(ctx context.Context, _ *mcp.CallToolRequest, in BatchInput) (*mcp.CallToolResult, any, error) {
		out, err := a.Store.SyncBatch(ctx, actor(ctx), in)
		return toolResult(out, err)
	})
	return mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
}
