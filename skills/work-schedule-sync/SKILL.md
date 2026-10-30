---
name: work-schedule-sync
description: 在已连接 Work Schedule 的 AI 开发中，以概要读取、批量局部更新和可选本地队列同步项目、需求拆解、进度与验证结果，控制等待和上下文成本。
---

# 低干扰开发同步

开发是主任务，同步失败不能阻塞它。使用可用工具中的 `ensure_project`、`read_workspace`、`sync_work_batch`（宿主可能增加前缀）；旧 `sync_work_item` 仅用于兼容。没有工具就说明尚未连接并继续开发，不虚构同步成功。

## 只读取需要的信息

- 已知 project_id 后复用；首次按统一仓库 URL 调用 ensure_project。GitHub SSH URL 统一为 HTTPS，去掉末尾 `.git` 和 `/`。没有仓库时用 UUID 身份，写入项目 `.work-schedule.json`，不保存凭据。
- 开工只读取当前项目概要。read_workspace 可用 identity_key 查项目；未限定项目仅列项目。默认最多 20 条概要，next_cursor 用作 after_id，limit 最大 50；按 kind/status/parent_id 缩小范围。不要循环拉完团队记录或已结束会话。
- 只有需要业务说明、验收标准或冲突合并时，才用 view=details 和 item_ids 读取最多 5 条完整详情。概要不是完整对象，禁止用概要重建旧完整更新。
- 缓存 id/version；只在目标过期或冲突时重读。相同查询可携带上一结果的 if_revision 和 if_scope；查询范围改变时不复用条件。
- 接入已有项目优先使用当前对话、README、需求与任务文档和当前修改，不为整理历史执行全仓库、全提交历史扫描或额外构建测试。

## 在交付节点合并写入

- 大需求为 requirement，拆解交付单元为 task，本轮执行为 session。task 关联本项目的父需求。会话按本轮创建一次、结束一次，不为每条命令更新心跳。
- 用 sync_work_batch 一次提交同一项目的 1–20 条变化。新建需 external_key、kind、title 等必要内容与 expected_version=0；需求必须有 acceptance。先创建需求，再让任务用 parent_key 引用它，可在同一批完成。
- 更新只传变化字段及已知 expected_version；未传字段保留，空字符串表示清空。每批 external_key 不重复。没有变化不发送，刚返回的版本可继续复用，不强制再次读取。
- 开始一个有意义单元、需求范围变化、阻塞、验证结束和交付时同步。相邻变化合并，不上报读文件、每次编辑、每条命令。短任务合并到一个完成节点，优先使用批量接口。
- 需求变化后关联单元会进入 needs_review，旧验证依据失效。不要恢复旧验收结果。AI 不能设置 accepted；正式验收由管理员确认。
- 日常进展更新子任务的 status、evidence、next_step；仅改说明文字不会推进阶段。大需求的 title/description/acceptance 是范围与验收约定，修改会触发全部关联单元复核，不要为了汇总进度反复改写它们，模块进度由面板根据子任务计算。
- 需求变更后，在继续相关单元时核对当前范围：未开始设 todo，已有局部实现且仍在开发设 in_progress，完整提交后按实际验证阶段设 pending_verification 或 pending_acceptance；有阻塞设 blocked 并写 next_step。重新记录适用于新需求的验证依据，无法确认的单元保留 needs_review。不能只追加说明而让已核对的单元一直停留在 needs_review，也不能批量恢复旧验收状态。
- pending_verification 表示尚未完成验证；pending_acceptance 必须有实际 evidence。失败、未执行或无法验证要明确记录。blocked 必须 next_step，说明原因和下一步。不上传思维过程、源码、完整对话、密钥或未脱敏日志。

## 失败即降级

每个逻辑批次使用唯一 request_id。结果未知时保留完全相同 ID 和参数，不改 payload 盲重试。同步服务处理预算为 3 秒，但客户端网络等待由宿主控制；能配置时将客户端超时设为 3–5 秒。一次失败后停止本轮即时重试，继续开发，避免把多个超时串起来。

错误会提供 code/action：conflict 需要只读目标、合并并生成新 request_id；unauthorized 检查凭据；invalid_input 修正参数。它们不是可盲重试的网络故障。恢复时不能用旧版本覆盖别人修改。

## 可选本地队列：真实后台补传

完整 Skill 包包含 `scripts/sync_queue.py`（Python 3，无额外依赖）。仅在用户明确选择队列模式并配置环境时使用，不默默启动常驻程序。

- 环境：WORK_SCHEDULE_URL 为平台根地址，WORK_SCHEDULE_TOKEN 为个人凭据。放在开发环境安全配置，不能写入项目文件或队列。
- 首次执行 `python scripts/sync_queue.py start` 启动本机后台工作进程。写入改为将 sync_work_batch 的 JSON 经标准输入传给 `python scripts/sync_queue.py enqueue`；enqueue 仅持久化本地文件，不等待网络。不要同时再通过 MCP 写同一批数据。
- queued 只表示本地已暂存；synced 才表示服务器已确认。使用 status 查看最近确认及新版本号，不能将已暂存说成已同步。不知道远端版本时保留待确认状态，不能猜测版本。
- 工作进程每次网络调用超时 2 秒，后台退避重试；同项目按序处理。冲突、权限或输入错误会保留并阻塞该项目后续批次，其他项目可继续。
- 出现 conflict/attention 时，先读取目标最新详情并合并，以新 request_id 入队，再执行 `resolve --request-id 旧ID --replacement 新ID`；不能直接丢弃错误批次。批次内字段与 expected_version 必须正确。
- 队列容量有上限，满时保留已有记录并明确报告未保存；不要阻塞开发。退出或重启电脑后需重新 start；stop 只停止本工具后台进程。默认队列在用户私有状态目录，可用 WORK_SCHEDULE_QUEUE_DIR 指定。状态不包含凭据。
- 本地队列只用于已登记且有 project_id 的项目。首次登记与按需读取仍需连接服务器。不能承诺完全无网络依赖。

未启用队列时，失败内容写到项目内带唯一请求 ID 的独立待同步文件（原子写入，加入忽略规则），避免多个 AI 共写一个 JSON；恢复后在下一有效节点补传。不把这种文件暂存宣称为自动后台补传。

## 完成工作

合并本轮已完成内容、真实验证结果、阻塞和下一步，结束本次会话。最终只简述同步成功、已暂存或待处理冲突，不反复输出整份项目记录。
