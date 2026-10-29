# Work Schedule

AI 开发过程自动同步的团队工作台。开发 AI 通过 MCP 登记项目、维护需求和开发子任务、同步阻塞与验证结果；团队在网页查看进度，由管理员确认验收。

## 面板使用层级

一级导航只保留工作总览、项目空间、AI 接入、团队成员。点击项目，弹出项目介绍、大需求列表和项目内动态；点击大需求，进入该需求的拆解进度，按顺序查看每个开发单元的工作说明、开发阶段、验证依据与验收反馈。开发详情在同一项目窗口内展开，面包屑可逐层返回。

开发提交和验收通过分别统计，取消的开发单元不计入进度；没有拆解项的需求不显示为已完成。总览的待处理事项会直接定位到所属项目、需求和开发单元。原有记录及 MCP 数据结构保持兼容。

## 技术结构

- Go 单体服务：业务 API、远程 MCP（官方 Go SDK / Streamable HTTP）、浏览器 SSE。
- React + TypeScript + Vite：静态管理面板。
- 现有 MySQL 8.0+ / InnoDB：当前状态、需求变更前后记录、幂等请求与活动历史。
- Skill：`skills/work-schedule-sync/SKILL.md`，规范 AI 开发时的同步行为。
- 服务器不运行模型、不执行成员项目的构建和测试。所有写入在确认数据库提交后才返回成功。

## 本地开发，连接线上 MySQL

需要 Go 1.26+、Node.js 24、pnpm 11.25.0，以及可从本机访问的专用 MySQL 数据库。无需本地安装 MySQL。

1. 复制 `.env.example` 为 `.env`，填写 `MYSQL_DSN`、首次管理员邮箱及随机密码。`.env` 已被 Git 忽略，勿提交凭据。
2. 数据库由管理员预先创建，使用专用账号；首次启动需要建表权限。服务会创建本工具的数据表，不创建或清空数据库。
3. 在 `web` 执行 `pnpm install --frozen-lockfile` 和 `pnpm build`。
4. 根目录执行 `go run ./cmd/server`，访问 `http://localhost:8080`。程序自动读取根目录 `.env`，已有环境变量优先。
5. 登录后在右上角修改初始密码。`BOOTSTRAP_*` 仅在用户表为空时创建首个管理员，修改环境配置不会重置已有密码。

如需前端热更新，在 `.env` 设置 `PUBLIC_URL=http://localhost:5173`，启动 Go 服务后在 `web` 执行 `pnpm dev`，浏览器访问 5173。MCP 客户端可直连后端 8080；连接配置中填写实际可访问的地址。

数据库连接建议启用证书校验的 TLS，例如 DSN 的 `tls=true`。`tls=preferred` 仅尝试加密且不验证服务端证书，可能回退明文；仅在已了解连接环境限制时使用。自签名证书需配置可信 CA 或通过受控隧道连接，不应将数据库端口向全部公网开放。

## 团队与 AI 接入

1. 管理员在“团队成员”创建账号。第一版是单团队共享空间，全部成员可查看所有项目：管理员管理成员并确认验收，开发成员可同步记录，只读成员不能写入。
2. 每位成员在“AI 接入”创建个人凭据。凭据只显示一次，有效期 90 天，可独立撤销。数据库仅保存摘要。
3. 将远程 MCP 地址与 Bearer 凭据配置到支持相应传输的开发工具。不是所有客户端都支持相同认证配置；第一版提供手动 Bearer 接入，不提供 OAuth 登录发现。
4. 安装 `skills/work-schedule-sync`，或把其中工作规范配置到开发工具支持的规则入口。安装位置、全局自动触发能力取决于宿主。

常见配置示意（字段以客户端为准）：

```json
{
  "mcpServers": {
    "work-schedule": {
      "url": "https://your-domain.example/mcp",
      "headers": { "Authorization": "Bearer <个人凭据>" }
    }
  }
}
```

### 低干扰 MCP 同步（0.2）

| 工具 | 用途 |
|---|---|
| `ensure_project` | 首次登记或查找项目，之后复用返回的 ID |
| `read_workspace` | 默认只读当前项目的分页概要；无项目范围仅列项目。`view=details` 配 `item_ids` 按需取最多 5 条详情 |
| `sync_work_batch` | 推荐：同项目 1–20 条原子批量创建或局部更新，仅返回 ID、状态和新版本 |
| `sync_work_item` | 保留旧单条完整更新入口；响应改为精简确认 |

批次使用唯一 `request_id`；结果未知时保持完全相同 ID 和参数。更新需 `expected_version`，未传字段保留，空字符串显式清空；同批可先创建需求，再用 `parent_key` 关联子任务。重复请求返回同一结果，无变化不增加版本或事件；任一项失败整批回滚。需求变更复核和管理员验收规则继续生效。项目级锁保留以维护需求与子项一致性，不再为所有写入锁住整个用户行。

概要默认 20 条、上限 50，返回 `next_cursor` 后用 `after_id` 翻页；按 `kind/status/parent_id` 缩小范围，默认排除已完成、已结束会话。相同查询可携带上一结果的 `revision/scope` 作为 `if_revision/if_scope`，未变化时只返回确认，不能跨查询范围复用。错误返回 `code/retryable/action`，权限、输入和版本冲突不能盲重试。

MCP 及队列写入口服务端处理预算 3 秒；客户端丢包、连接和模型调度仍由宿主控制，不能承诺所有客户端都在 3 秒内返回。直接 MCP 同步失败后暂存并继续开发，不连续循环重试。

### 可选本地后台队列

AI 接入页面下载完整 Skill ZIP，包含 `scripts/sync_queue.py`。Python 3 即可使用，无第三方依赖。它不调用模型、不扫描项目，写入先落本机私有目录，后台再通过 `/api/agent/sync` 提交；只在明确启用时启动。

在开发环境安全配置中设置 `WORK_SCHEDULE_URL`（平台根地址）和 `WORK_SCHEDULE_TOKEN`（个人凭据），然后：

```sh
python scripts/sync_queue.py start
# 把与 sync_work_batch 相同的 JSON 从标准输入传入；此步不等待网络。
python scripts/sync_queue.py enqueue < batch.json
python scripts/sync_queue.py status
python scripts/sync_queue.py stop
```

`queued` 是已暂存，`synced` 才是远端确认。首次项目登记、需要最新内容的读取仍需联网。每次后台请求超时 2 秒，退避重试；同项目按序，权限或冲突错误会保留并阻塞该项目，其他项目继续。重新读取、合并并用新 ID 入队后，显式 `resolve --request-id 旧ID --replacement 新ID` 解除冲突。不能盲目改写旧请求或伪造新版本。

默认状态目录 `~/.local/state/work-schedule/outbox`，可用 `WORK_SCHEDULE_QUEUE_DIR` 修改。采用文件锁、原子替换和私有权限；队列有 10MiB/1000 条上限，满时明确失败且不丢已有事件。已同步后删除本地业务正文，仅保留最近确认记录。凭据不写入队列；后台进程从启动环境读取凭据，轮换后需 stop 再 start。电脑重启后需重新 start，不自动安装系统服务。

## 验证

```sh
python3 -m unittest discover -s tests -v
go vet ./...
go test -race ./...
cd web && pnpm test && pnpm build
```

默认测试不访问线上数据库。`TestMySQLWorkflow` 需要显式设置 `TEST_MYSQL_DSN`，推荐使用独立测试库。它创建随机测试用户与项目，完成后仅清理对应数据，不清空库。不要对有真实成员正在开发的数据库运行集成测试。

GitHub Actions 配置了隔离 MySQL 8.4 服务，运行实际数据库流程和 MCP 协议测试。工作流只有仓库内容读取权限。

## 部署到 2 核 2GB 服务器

在本地或 CI 构建，线上只运行成品：

```sh
make linux
# ARM 服务器使用 GOARCH=arm64 make linux
```

将 `bin/work-schedule` 和 `web/dist` 上传至 `/opt/work-schedule` 的对应目录。复用现有 MySQL、Nginx，不需要新建数据库进程、Redis、消息队列或常驻模型服务。

1. 创建无登录的 `work-schedule` 系统用户。
2. 将配置放 `/etc/work-schedule.env`，权限设为 `root:root 600`。设置 `PUBLIC_URL=https://实际域名`、`COOKIE_SECURE=true`、`LISTEN_ADDR=127.0.0.1:8080`、`WEB_DIR=/opt/work-schedule/web/dist`、数据库 DSN。
3. 使用 `deploy/work-schedule.service` 注册 systemd 服务，使用 `deploy/nginx.conf` 接入现有 HTTPS 配置。
4. 检查 `/healthz`、网页登录、MCP 连接和事件更新。确认成功后从运行配置移除 `BOOTSTRAP_PASSWORD`。
5. 记录资源基线，监控内存、磁盘、数据库连接、错误率和写入等待；实际容量通过负载测试确认，不按 CPU 核数承诺人数。

应用连接池上限 8、空闲连接 3。面板共享一个按需启动的修订检测器，每 5 秒查询一次修订号并向连接广播；每个连接继续独立验证会话，断线自动重连；另有每分钟补读。修订号在业务事务内更新，不依赖事件自增 ID 的提交先后顺序。没有面板连接时修订检测器自动停止。

Nginx 必须关闭事件流缓冲并允许长连接。仅信任本机反向代理提供的 `X-Real-IP`，否则用户提交的来源头不能影响限流身份。

## 数据维护与第一版边界

- 单团队共享项目，没有项目级隔离、独立租户、成员停用/删除、密码找回、任务依赖调度或部署平台自动接入。
- 面板全局快照项目上限 200、工作项上限 2000，活动最近 100 条；MCP 使用独立分页概要和按需详情。面板超限明确报错，不静默截断。规模扩大前补分页和历史归档。
- `read_workspace` 读取状态摘要和版本；完整变更前后数据保存在事件表，面板暂展示活动摘要。
- 幂等请求和活动历史不自动删除。日志、数据库容量和备份需要部署方监控，制定保留策略。
- 定期使用现有 MySQL 备份体系备份到其他设备，升级前备份并验证恢复；本仓库不自动改动现有备份计划。
- `.env` 只在本地保存，API 凭据不存前端本地存储。浏览器登录使用 HttpOnly Cookie；修改密码撤销其他会话及开发凭据。
