import {
  ArrowLeft,
  ArrowRight,
  Blocks,
  Check,
  ChevronDown,
  ChevronRight,
  Clock3,
  ExternalLink,
  FileText,
  FolderGit2,
  GitBranch,
  ShieldCheck,
  X,
} from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { elapsed } from "../lib/api";
import { deliveryStage, deliverySteps, summarizeTasks } from "../lib/progress";
import type { Event, Item, Project, User } from "../types";
import { Detail } from "./Management";
import { ProgressSummary } from "./ProgressSummary";
import { Empty, Status, Timeline } from "./shared";

function repositoryLink(value: string) {
  try {
    const u = new URL(value);
    return ["https:", "http:"].includes(u.protocol) ? u.href : undefined;
  } catch {
    return undefined;
  }
}
export function ProjectExplorer({
  project,
  items,
  events,
  initialItemId,
  user,
  close,
  saved,
  connected,
  syncError,
}: {
  project: Project;
  items: Item[];
  events: Event[];
  initialItemId?: string;
  user: User;
  close: () => void;
  saved: () => Promise<void>;
  connected: boolean;
  syncError?: string;
}) {
  const initial = items.find((i) => i.id === initialItemId);
  const [requirementId, setRequirementId] = useState(
    initial?.kind === "requirement"
      ? initial.id
      : initial?.kind === "task"
        ? initial.parent_id
        : "",
  );
  const [taskId, setTaskId] = useState(
    initial?.kind === "task" ? initial.id : "",
  );
  const [view, setView] = useState("requirements"),
    [reviewing, setReviewing] = useState(false),
    [filter, setFilter] = useState("all");
  const restoreFocus = useRef(document.activeElement as HTMLElement | null);
  const root = useRef<HTMLElement>(null),
    scroll = useRef<HTMLDivElement>(null),
    navigation = useRef(() => {});
  useEffect(() => {
    if (scroll.current) scroll.current.scrollTop = 0;
    root.current?.focus({ preventScroll: true });
  }, [requirementId, view]);
  navigation.current = () => {
    if (reviewing) setReviewing(false);
    else if (taskId) setTaskId("");
    else if (requirementId) {
      setRequirementId("");
      setFilter("all");
    } else close();
  };
  useEffect(() => {
    const previous = restoreFocus.current,
      oldOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    root.current?.focus();
    const keydown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        navigation.current();
      } else if (e.key === "Tab") {
        const nodes = Array.from(
          root.current?.querySelectorAll<HTMLElement>(
            'button:not(:disabled),a[href],input,textarea,select,[tabindex="0"]',
          ) || [],
        ).filter((n) => n.getClientRects().length > 0);
        const first = nodes[0],
          last = nodes[nodes.length - 1];
        if (!first || !last) return;
        if (!root.current?.contains(document.activeElement)) {
          e.preventDefault();
          (e.shiftKey ? last : first).focus();
          return;
        }
        if (
          e.shiftKey &&
          (document.activeElement === first ||
            document.activeElement === root.current)
        ) {
          e.preventDefault();
          last.focus();
        } else if (
          !e.shiftKey &&
          (document.activeElement === last ||
            document.activeElement === root.current)
        ) {
          e.preventDefault();
          first.focus();
        }
      }
    };
    document.addEventListener("keydown", keydown);
    return () => {
      document.body.style.overflow = oldOverflow;
      document.removeEventListener("keydown", keydown);
      previous?.focus();
    };
  }, []);
  const requirements = items
    .filter((i) => i.kind === "requirement")
    .sort((a, b) => a.created_at.localeCompare(b.created_at));
  const requirement = items.find(
    (i) => i.id === requirementId && i.kind === "requirement",
  );
  const children = requirement
    ? items
        .filter((i) => i.kind === "task" && i.parent_id === requirement.id)
        .sort(
          (a, b) =>
            a.created_at.localeCompare(b.created_at) ||
            a.external_key.localeCompare(b.external_key),
        )
    : [];
  const summary = summarizeTasks(requirement ? children : items);
  const visibleChildren = children.filter(
    (i) =>
      filter === "all" ||
      (filter === "attention" &&
        ["blocked", "needs_review", "rejected"].includes(i.status)) ||
      (filter === "verification" &&
        ["pending_verification", "pending_acceptance"].includes(i.status)),
  );
  const unassigned = items.filter(
    (i) => i.kind === "task" && !requirements.some((r) => r.id === i.parent_id),
  );
  const href = repositoryLink(project.repository);
  const backToProject = () => {
    setRequirementId("");
    setTaskId("");
    setReviewing(false);
    setFilter("all");
  };
  const openRequirement = (id: string) => {
    setRequirementId(id);
    setTaskId("");
    setFilter("all");
    setReviewing(false);
  };
  return (
    <div className="project-backdrop" onClick={close}>
      <section
        ref={root}
        tabIndex={-1}
        className="project-explorer"
        role="dialog"
        aria-modal="true"
        aria-label={"项目详情：" + project.name}
        onClick={(e) => e.stopPropagation()}
      >
        <header className="explorer-header">
          <div className="explorer-breadcrumb">
            <button onClick={requirement ? backToProject : close}>
              <ArrowLeft size={16} />
              {requirement ? "项目概览" : "返回项目"}
            </button>
            <ChevronRight size={13} />
            <button onClick={backToProject}>{project.name}</button>
            {requirement && (
              <>
                <ChevronRight size={13} />
                <span>{requirement.external_key}</span>
              </>
            )}
          </div>
          <button
            className="icon-button"
            aria-label="关闭项目详情"
            onClick={close}
          >
            <X size={21} />
          </button>
        </header>
        <div ref={scroll} className="explorer-scroll">
          {syncError && (
            <div className="error" role="alert">
              同步暂未成功，以下为最近一次读取的记录。{syncError}
              <button onClick={() => void saved()}>重试</button>
            </div>
          )}
          {!requirement ? (
            <>
              <div className="project-intro">
                <span className="project-intro-icon">
                  <FolderGit2 size={26} />
                </span>
                <div>
                  <span className="eyebrow">PROJECT WORKSPACE</span>
                  <h2>{project.name}</h2>
                  <p>{project.summary || "项目介绍尚未由开发 AI 补充。"}</p>
                  <div className="project-intro-meta">
                    {href ? (
                      <a href={href} target="_blank" rel="noreferrer">
                        <GitBranch size={13} />
                        查看代码仓库
                        <ExternalLink size={12} />
                      </a>
                    ) : (
                      <span>
                        <GitBranch size={13} />
                        尚未关联仓库
                      </span>
                    )}
                    <span>
                      <Clock3 size={13} />
                      {elapsed(project.updated_at)}更新
                    </span>
                    <span
                      className={"connection " + (connected ? "online" : "")}
                    >
                      <span />
                      {connected ? "进度动态同步" : "连接恢复后更新"}
                    </span>
                  </div>
                </div>
              </div>
              <div className="project-rollup">
                <div>
                  <span>大需求</span>
                  <strong>
                    {requirements.length}
                    <small>项</small>
                  </strong>
                </div>
                <div>
                  <span>开发单元</span>
                  <strong>
                    {summary.total}
                    <small>项</small>
                  </strong>
                </div>
                <div>
                  <span>开发已提交</span>
                  <strong>
                    {summary.submitted}
                    <small> / {summary.total}</small>
                  </strong>
                </div>
                <div>
                  <span>验收已通过</span>
                  <strong>
                    {summary.accepted}
                    <small> / {summary.total}</small>
                  </strong>
                </div>
              </div>
              <nav className="project-local-tabs" aria-label="项目内导航">
                <button
                  className={view === "requirements" ? "selected" : ""}
                  onClick={() => setView("requirements")}
                >
                  <FileText size={15} />
                  需求与拆解<span>{requirements.length}</span>
                </button>
                <button
                  className={view === "activity" ? "selected" : ""}
                  onClick={() => setView("activity")}
                >
                  <GitBranch size={15} />
                  项目动态
                </button>
              </nav>
              {view === "requirements" ? (
                <section className="requirements-section">
                  <div className="explorer-section-heading">
                    <div>
                      <h3>从大需求，逐步推进到交付</h3>
                      <p>
                        选择一条需求，查看拆解后的开发步骤、验证依据与验收情况。
                      </p>
                    </div>
                    <span className="small-caption">开发提交 ≠ 验收通过</span>
                  </div>
                  {requirements.length ? (
                    <div className="requirement-cards">
                      {requirements.map((r, index) => {
                        const tasks = items.filter(
                            (i) => i.kind === "task" && i.parent_id === r.id,
                          ),
                          s = summarizeTasks(tasks);
                        return (
                          <button
                            key={r.id}
                            className="requirement-card"
                            aria-label={"查看需求拆解：" + r.title}
                            onClick={() => openRequirement(r.id)}
                          >
                            <div className="requirement-card-top">
                              <span className="requirement-index">
                                {String(index + 1).padStart(2, "0")}
                              </span>
                              <code>
                                {r.external_key} · v{r.version}
                              </code>
                              <Status value={r.status} />
                            </div>
                            <h3>{r.title}</h3>
                            <p>{r.description || "尚未补充需求说明"}</p>
                            <ProgressSummary items={tasks} compact />
                            <div className="requirement-card-bottom">
                              <span>
                                <Blocks size={13} />
                                {s.total
                                  ? `${s.total} 个开发单元`
                                  : "等待 AI 拆解"}
                              </span>
                              {s.blocked > 0 && (
                                <span className="risk-label">
                                  {s.blocked} 项阻塞
                                </span>
                              )}
                              {s.review > 0 && (
                                <span className="risk-label">
                                  {s.review} 项待复核
                                </span>
                              )}
                              <strong>
                                查看拆解进度
                                <ArrowRight size={14} />
                              </strong>
                            </div>
                          </button>
                        );
                      })}
                    </div>
                  ) : (
                    <Empty
                      title="需求尚未整理"
                      body="AI 接收开发目标后，会把大需求及拆解出的开发单元同步到当前项目。"
                    />
                  )}
                  {unassigned.length > 0 && (
                    <div className="error">
                      有 {unassigned.length} 个开发单元未找到所属需求，请让开发
                      AI 检查关联关系。
                    </div>
                  )}
                </section>
              ) : (
                <section className="project-activity">
                  <div className="explorer-section-heading">
                    <div>
                      <h3>当前项目的开发动态</h3>
                      <p>按事件记录查看需求变化和逐步交付。</p>
                    </div>
                  </div>
                  <Timeline events={events} projects={[project]} />
                </section>
              )}
            </>
          ) : (
            <>
              <div className="requirement-intro">
                <div className="requirement-heading-line">
                  <span className="eyebrow">REQUIREMENT BREAKDOWN</span>
                  <Status value={requirement.status} />
                </div>
                <h2>{requirement.title}</h2>
                <p>{requirement.description || "尚未补充需求说明"}</p>
                <div className="requirement-context">
                  <span>
                    {requirement.external_key} · 需求版本 {requirement.version}
                  </span>
                  <span>
                    {requirement.actor} · {elapsed(requirement.updated_at)}更新
                  </span>
                </div>
              </div>
              <div className="requirement-scope">
                <div>
                  <h3>
                    <ShieldCheck size={15} />
                    这条需求如何算完成
                  </h3>
                  <p>{requirement.acceptance || "尚未提供验收标准"}</p>
                  {requirement.next_step && (
                    <p className="next-step">
                      <strong>下一步：</strong>
                      {requirement.next_step}
                    </p>
                  )}
                </div>
                <div className="requirement-scope-progress">
                  <ProgressSummary items={children} />
                  <span className="small-caption">
                    按未取消的开发单元统计；需求变化后重新复核。
                  </span>
                </div>
              </div>
              <div className="explorer-section-heading breakdown-heading">
                <div>
                  <h3>
                    需求拆解与开发进度<span>{children.length} 个单元</span>
                  </h3>
                  <p>逐个展开，查看工作内容、验证依据和验收结果。</p>
                </div>
                <button
                  className="secondary"
                  onClick={() => setReviewing(!reviewing)}
                >
                  <ShieldCheck size={14} />
                  {reviewing ? "收起需求验收" : "需求整体验收"}
                </button>
              </div>
              {reviewing && (
                <div className="requirement-review">
                  <Detail
                    embedded
                    item={requirement}
                    user={user}
                    project={project}
                    close={() => setReviewing(false)}
                    saved={saved}
                  />
                </div>
              )}
              <div
                className="breakdown-filters"
                role="group"
                aria-label="筛选开发单元"
              >
                {[
                  ["all", "全部单元"],
                  ["attention", "阻塞与复核"],
                  ["verification", "验证与验收"],
                ].map(([key, label]) => (
                  <button
                    key={key}
                    className={filter === key ? "selected" : ""}
                    onClick={() => setFilter(key)}
                  >
                    {label}
                  </button>
                ))}
                <span>
                  {summary.developing} 项开发中 · {summary.verifying} 项待验证 ·{" "}
                  {summary.pending} 项待验收
                </span>
              </div>
              <div className="breakdown-list">
                {visibleChildren.length ? (
                  visibleChildren.map((task) => {
                    const index = children.findIndex((i) => i.id === task.id),
                      open = taskId === task.id,
                      stage = deliveryStage(task.status),
                      cancelled = task.status === "cancelled";
                    return (
                      <article
                        key={task.id}
                        className={
                          "breakdown-unit " +
                          (open ? "expanded " : "") +
                          task.status
                        }
                      >
                        <button
                          className="breakdown-unit-head"
                          aria-expanded={open}
                          aria-controls={"unit-" + task.id}
                          aria-label={"展开开发单元：" + task.title}
                          onClick={() => setTaskId(open ? "" : task.id)}
                        >
                          <span className="unit-number">
                            {task.status === "accepted" ? (
                              <Check size={17} />
                            ) : (
                              String(index + 1).padStart(2, "0")
                            )}
                          </span>
                          <span className="unit-heading">
                            <code>{task.external_key}</code>
                            <strong>{task.title}</strong>
                            <small>
                              {task.actor} · {elapsed(task.updated_at)}更新
                            </small>
                          </span>
                          <span
                            className="unit-stages"
                            aria-label={
                              cancelled
                                ? "已取消"
                                : `当前状态：${task.status === "accepted" ? "验收通过" : deliverySteps[stage]}`
                            }
                          >
                            {deliverySteps.map((label, n) => (
                              <span
                                className={
                                  cancelled
                                    ? ""
                                    : stage > n
                                      ? "complete"
                                      : stage === n
                                        ? "current"
                                        : ""
                                }
                                key={label}
                              >
                                <i>
                                  {!cancelled && stage > n ? (
                                    <Check size={9} />
                                  ) : (
                                    n + 1
                                  )}
                                </i>
                                {label}
                              </span>
                            ))}
                          </span>
                          <Status value={task.status} />
                          <ChevronDown
                            size={17}
                            className={open ? "rotate" : ""}
                          />
                        </button>
                        {open && (
                          <div className="unit-detail" id={"unit-" + task.id}>
                            <Detail
                              embedded
                              item={task}
                              inheritedAcceptance={requirement.acceptance}
                              user={user}
                              project={project}
                              close={() => setTaskId("")}
                              saved={saved}
                            />
                          </div>
                        )}
                      </article>
                    );
                  })
                ) : (
                  <Empty
                    title={
                      children.length
                        ? "当前筛选下没有开发单元"
                        : "等待开发 AI 拆解"
                    }
                    body={
                      children.length
                        ? "切换到全部单元，查看这条需求的完整开发步骤。"
                        : "这条大需求还没有开发单元，不会将其显示为已完成。"
                    }
                  />
                )}
              </div>
            </>
          )}
        </div>
      </section>
    </div>
  );
}
