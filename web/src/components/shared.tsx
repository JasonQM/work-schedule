import {
  Activity,
  ArrowDownLeft,
  ArrowRight,
  ArrowUpRight,
  Blocks,
  ChevronRight,
  Command,
  FileText,
  FolderGit2,
  GitBranch,
  MoreHorizontal,
  Radio,
} from "lucide-react";
import React from "react";

import { elapsed } from "../lib/api";
import { actionNames, statuses } from "../lib/constants";
import type { Event, Item, Project } from "../types";
import { ProgressSummary } from "./ProgressSummary";
export function Status({ value }: { value: string }) {
  return (
    <span className={"status " + value}>
      <span />
      {statuses[value] || value}
    </span>
  );
}

export function Empty({
  title,
  body,
  action,
}: {
  title: string;
  body: string;
  action?: () => void;
}) {
  return (
    <div className="empty">
      <div className="empty-icon">
        <FolderGit2 size={29} />
      </div>
      <h3>{title}</h3>
      <p>{body}</p>
      {action && (
        <button className="primary" onClick={action}>
          连接开发 AI <ArrowRight size={15} />
        </button>
      )}
    </div>
  );
}

export function Brand() {
  return (
    <div className="brand">
      <span className="brand-icon">
        <Command size={21} />
      </span>
      <span>
        work<span className="brand-light">schedule</span>
        <small>AI DEVELOPMENT WORKSPACE</small>
      </span>
    </div>
  );
}

export function Stat({
  label,
  value,
  icon,
  note,
  accent = false,
}: {
  label: string;
  value: number;
  icon: React.ReactNode;
  note: string;
  accent?: boolean;
}) {
  return (
    <div className={"stat " + (accent ? "accent" : "")}>
      <div>
        <span>{label}</span>
        <span className="stat-icon">{icon}</span>
      </div>
      <strong>
        {value}
        <span>项</span>
      </strong>
      <small>{note}</small>
    </div>
  );
}

export function Section({
  title,
  note,
  action,
}: {
  title: string;
  note?: string;
  action?: () => void;
}) {
  return (
    <div className="section-title">
      <h2>{title}</h2>
      <span>{note}</span>
      {action && (
        <button onClick={action} aria-label={"查看全部" + title}>
          <ArrowUpRight size={18} />
        </button>
      )}
    </div>
  );
}

export function ProjectCard({
  project: p,
  items,
  onOpen,
}: {
  project: Project;
  items: Item[];
  onOpen: () => void;
}) {
  const tasks = items.filter(
    (i) => i.kind === "task" && i.status !== "cancelled",
  );
  const sessions = items.filter(
    (i) => i.kind === "session" && i.status === "in_progress",
  );
  const active = sessions.some(
    (i) => Date.now() - new Date(i.updated_at).getTime() < 10 * 60000,
  );
  const stale = !active && sessions.length > 0;
  return (
    <button
      className="project-card"
      aria-label={"打开项目：" + p.name}
      onClick={onOpen}
    >
      <div className="project-top">
        <span className="project-symbol">
          <FolderGit2 size={21} />
        </span>
        <span className={"project-state " + (active ? "live" : "")}>
          {active ? (
            <>
              <Radio size={12} />
              AI 开发中
            </>
          ) : stale ? (
            "会话待确认"
          ) : (
            "已接入"
          )}
        </span>
        <MoreHorizontal size={18} />
      </div>
      <h3>{p.name}</h3>
      <p>{p.summary || "开发 AI 尚未补充项目简介"}</p>
      <div className="project-meta">
        <span>
          <FileText size={13} />
          {items.filter((i) => i.kind === "requirement").length} 个大需求
        </span>
        <span>
          <Blocks size={13} />
          {tasks.length} 个开发单元
        </span>
      </div>
      <div className="project-card-progress">
        <ProgressSummary items={items} compact />
      </div>
      <footer>
        <span>
          <GitBranch size={13} />
          {p.repository ? "已关联仓库" : "未关联仓库"}
        </span>
        <span>
          {elapsed(p.updated_at)} <ArrowUpRight size={13} />
        </span>
      </footer>
    </button>
  );
}

export function Timeline({
  events,
  projects,
}: {
  events: Event[];
  projects: Project[];
}) {
  return events.length ? (
    <div className="timeline">
      {events.map((e) => (
        <div key={e.sequence} className="timeline-row">
          <span
            className={
              "event-icon " +
              (e.action === "requirement.changed" ? "changed" : "")
            }
          >
            {e.action === "project.created" ? (
              <FolderGit2 size={15} />
            ) : e.action === "requirement.changed" ? (
              <GitBranch size={15} />
            ) : (
              <ArrowDownLeft size={15} />
            )}
          </span>
          <div>
            <p>
              <strong>{e.actor}</strong> {actionNames[e.action] || e.action}
            </p>
            <span>{e.summary}</span>
            <small>{projects.find((p) => p.id === e.project_id)?.name}</small>
          </div>
          <time title={new Date(e.created_at).toLocaleString("zh-CN")}>
            {elapsed(e.created_at)}
          </time>
        </div>
      ))}
    </div>
  ) : (
    <div className="empty compact">
      <Activity size={25} />
      <p>开发事件会自动出现在这里</p>
    </div>
  );
}

export function ItemTable({
  items,
  projects,
  onOpen,
}: {
  items: Item[];
  projects: Project[];
  onOpen: (i: Item) => void;
}) {
  return items.length ? (
    <div className="table-scroll">
      <table>
        <thead>
          <tr>
            <th>开发事项</th>
            <th>所属项目</th>
            <th>当前状态</th>
            <th>更新人</th>
            <th>最近更新</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {items.map((i) => (
            <tr
              key={i.id}
              onClick={() => onOpen(i)}
              tabIndex={0}
              onKeyDown={(e) => {
                if (e.key === "Enter") onOpen(i);
              }}
            >
              <td>
                <code>
                  {i.external_key} · v{i.version}
                </code>
                <strong>{i.title}</strong>
              </td>
              <td>{projects.find((p) => p.id === i.project_id)?.name}</td>
              <td>
                <Status value={i.status} />
              </td>
              <td>{i.actor}</td>
              <td className="muted">{elapsed(i.updated_at)}</td>
              <td>
                <ChevronRight size={15} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  ) : (
    <Empty
      title="暂无相关事项"
      body="AI 同步需求、任务或验证结果后，这里会自动更新。"
    />
  );
}
