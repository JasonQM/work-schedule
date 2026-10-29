import {
  Check,
  ChevronRight,
  Command,
  Copy,
  FileText,
  FolderGit2,
  Link2,
  Plus,
  ShieldCheck,
  X,
} from "lucide-react";
import React, { useEffect, useState } from "react";

import { api, elapsed } from "../lib/api";
import { roles } from "../lib/constants";
import type { Item, Project, Token, User } from "../types";
import { Status } from "./shared";
export function PasswordModal({ close }: { close: () => void }) {
  const [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [success, setSuccess] = useState(false);
  async function submit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const d = new FormData(e.currentTarget);
    if (d.get("next") !== d.get("confirm")) {
      setError("两次输入的新密码不一致");
      return;
    }
    setBusy(true);
    setError("");
    try {
      await api("/password", {
        method: "POST",
        body: JSON.stringify({
          current: d.get("current"),
          next: d.get("next"),
        }),
      });
      setSuccess(true);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="modal-backdrop" onClick={close}>
      <section
        className="password-dialog"
        role="dialog"
        aria-modal="true"
        aria-label="修改我的密码"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="section-title">
          <h2>修改我的密码</h2>
          <button className="icon-button" aria-label="关闭" onClick={close}>
            <X size={18} />
          </button>
        </div>
        {success ? (
          <div className="padded">
            <p>密码已更新，其他登录会话及开发凭据已撤销。当前会话保留。</p>
            <button className="primary" onClick={close}>
              完成
            </button>
          </div>
        ) : (
          <form className="password-form" onSubmit={submit}>
            <p>修改后将撤销其他会话及所有开发凭据，AI 接入需要重新创建凭据。</p>
            <label>
              当前密码
              <input
                name="current"
                type="password"
                required
                autoComplete="current-password"
              />
            </label>
            <label>
              新密码
              <input
                name="next"
                type="password"
                required
                minLength={12}
                maxLength={72}
                autoComplete="new-password"
              />
            </label>
            <label>
              确认新密码
              <input
                name="confirm"
                type="password"
                required
                minLength={12}
                maxLength={72}
                autoComplete="new-password"
              />
            </label>
            {error && (
              <div className="error" role="alert">
                {error}
              </div>
            )}
            <button className="primary" disabled={busy}>
              保存新密码
            </button>
          </form>
        )}
      </section>
    </div>
  );
}

export function Detail({
  item: i,
  user,
  project,
  close,
  saved,
  embedded = false,
  inheritedAcceptance,
}: {
  item: Item;
  user: User;
  project?: Project;
  close: () => void;
  saved: () => Promise<void>;
  embedded?: boolean;
  inheritedAcceptance?: string;
}) {
  const [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [note, setNote] = useState("");
  const [reviewedVersion] = useState(i.version);
  const stale = reviewedVersion !== i.version;
  async function decide(status: string) {
    if (stale) {
      setError("记录已更新，请收起并重新展开后检查最新版本。");
      return;
    }
    setBusy(true);
    setError("");
    try {
      const { actor, created_at, updated_at, version, ...fields } = i;
      void actor;
      void created_at;
      void updated_at;
      await api("/items", {
        method: "PUT",
        body: JSON.stringify({
          ...fields,
          request_id: crypto.randomUUID(),
          expected_version: version,
          status,
          evidence: i.evidence + (note ? "\n验收意见：" + note : ""),
          next_step: status === "rejected" ? note : i.next_step,
        }),
      });
      await saved();
      close();
    } catch (e) {
      setError((e as Error).message);
      await saved();
    } finally {
      setBusy(false);
    }
  }
  useEffect(() => {
    const fn = (e: KeyboardEvent) => {
      if (!embedded && e.key === "Escape") close();
    };
    window.addEventListener("keydown", fn);
    return () => window.removeEventListener("keydown", fn);
  }, [close, embedded]);
  return (
    <div
      className={embedded ? "embedded-detail" : "modal-backdrop"}
      onClick={embedded ? undefined : close}
    >
      <section
        className={embedded ? "detail detail-inline" : "detail"}
        role={embedded ? undefined : "dialog"}
        aria-modal={embedded ? undefined : true}
        aria-label="事项详情"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="detail-head">
          <span>
            {project?.name} <ChevronRight size={13} /> {i.external_key}
          </span>
          <button
            className="icon-button"
            aria-label={embedded ? "收起开发详情" : "关闭"}
            onClick={close}
          >
            <X size={21} />
          </button>
        </div>
        <h2>{i.title}</h2>
        {stale && (
          <div className="error" role="status">
            该单元已同步新版本。请收起后重新展开，检查最新内容再作出验收决定。
          </div>
        )}
        <div className="detail-meta">
          <Status value={i.status} />
          <span>版本 {i.version}</span>
          <span>
            {i.actor} · {elapsed(i.updated_at)}
          </span>
        </div>
        {[
          ["需求与工作说明", i.description],
          [
            "验收标准",
            i.acceptance ||
              (inheritedAcceptance
                ? "沿用所属需求的验收标准：\n" + inheritedAcceptance
                : ""),
          ],
          ["验证依据", i.evidence],
          ["阻塞与下一步", i.next_step],
        ].map(([label, value]) => (
          <div className="detail-section" key={label}>
            <h3>{label}</h3>
            <p>{value || "尚未提供"}</p>
          </div>
        ))}
        {user.role === "admin" && i.status === "pending_acceptance" && (
          <div className="review-box">
            <h3>确认本次交付</h3>
            <p>检查上方验证依据后确认。验收不通过时，请说明需要调整的内容。</p>
            <textarea
              value={note}
              onChange={(e) => setNote(e.target.value)}
              placeholder="验收意见；不通过时必填"
            />
            {error && (
              <div className="error" role="alert">
                {error}
              </div>
            )}
            <div>
              <button
                className="secondary"
                disabled={busy || stale || !note.trim()}
                onClick={() => decide("rejected")}
              >
                验收不通过
              </button>
              <button
                className="primary"
                disabled={busy || stale}
                onClick={() => decide("accepted")}
              >
                <Check size={16} />
                确认验收通过
              </button>
            </div>
          </div>
        )}
        <div className="footnote">
          验收结果以当前需求版本为准。需求范围变化后，关联任务需要重新复核。
        </div>
      </section>
    </div>
  );
}

export function Integrations({ onError }: { onError: (s: string) => void }) {
  const [tokens, setTokens] = useState<Token[]>([]),
    [name, setName] = useState(""),
    [created, setCreated] = useState<{
      token: string;
      endpoint: string;
    } | null>(null),
    [busy, setBusy] = useState(false),
    [copied, setCopied] = useState(false);
  const load = () =>
    api<Token[]>("/tokens")
      .then(setTokens)
      .catch((e) => onError(e.message));
  useEffect(() => {
    void load();
  }, []);
  async function create(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    try {
      setCreated(
        await api("/tokens", {
          method: "POST",
          body: JSON.stringify({ name }),
        }),
      );
      setName("");
      await load();
    } catch (e) {
      onError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  const config = JSON.stringify(
    {
      mcpServers: {
        "work-schedule": {
          url: created?.endpoint || window.location.origin + "/mcp",
          headers: {
            Authorization: "Bearer " + (created?.token || "<你的个人凭据>"),
          },
        },
      },
    },
    null,
    2,
  );
  return (
    <div className="integration-layout">
      <section className="connect-banner">
        <div className="connect-graphic">
          <Command size={30} />
          <span />
          <Link2 size={23} />
          <span />
          <FolderGit2 size={30} />
        </div>
        <span className="eyebrow">ONE CONNECTION, CONTINUOUS CONTEXT</span>
        <h2>开发照常进行，进展自动同步。</h2>
        <p>
          将团队工作台连接到支持远程 MCP 的开发工具，
          <br />
          再启用同步 Skill，让需求、任务与交付始终在一起。
        </p>
      </section>
      <div className="integration-grid">
        <section className="panel padded">
          <span className="step-number">01</span>
          <h2>创建个人开发凭据</h2>
          <p className="muted">
            每个成员独立使用，可随时撤销，有效期 90
            天。开发凭据不具备验收通过权限。
          </p>
          <form onSubmit={create}>
            <label>
              凭据名称
              <input
                required
                maxLength={120}
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="例如：我的开发电脑"
              />
            </label>
            <button className="primary" disabled={busy}>
              <Plus size={16} />
              创建凭据
            </button>
          </form>
          {created && (
            <div className="secret-notice">
              <ShieldCheck size={17} />
              <span>
                凭据仅在本次显示，请保存到开发工具的安全配置中。离开页面后无法再次查看。
              </span>
            </div>
          )}
          <div className="token-list">
            {tokens.map((t) => (
              <div key={t.id}>
                <span>
                  <strong>{t.name}</strong>
                  <small>
                    到期：{new Date(t.expires_at).toLocaleDateString("zh-CN")}
                  </small>
                </span>
                <button
                  className="text-button danger"
                  onClick={async () => {
                    if (
                      !window.confirm(
                        "撤销后，使用此凭据的 AI 将无法同步。确认撤销？",
                      )
                    )
                      return;
                    try {
                      await api("/tokens/" + t.id, { method: "DELETE" });
                      await load();
                    } catch (e) {
                      onError((e as Error).message);
                    }
                  }}
                >
                  撤销
                </button>
              </div>
            ))}
          </div>
        </section>
        <section className="panel padded">
          <span className="step-number">02</span>
          <h2>连接 MCP 并启用 Skill</h2>
          <p className="muted">
            以下是常见配置结构，具体字段以你的开发工具为准。首次连接后，启用仓库中的同步
            Skill。
          </p>
          <div className="code-header">
            <span>MCP 连接配置</span>
            <button
              className="icon-button"
              aria-label="复制配置"
              onClick={async () => {
                try {
                  await navigator.clipboard.writeText(config);
                  setCopied(true);
                  setTimeout(() => setCopied(false), 2000);
                } catch {
                  onError("复制失败，请手动选择配置内容");
                }
              }}
            >
              {copied ? <Check size={15} /> : <Copy size={15} />}
            </button>
          </div>
          <pre>{config}</pre>
          <div className="skill-path">
            <FileText size={16} />
            <code>skills/work-schedule-sync/SKILL.md</code>
          </div>
          <a
            className="secondary skill-download"
            href="/api/skill-package"
            download="work-schedule-sync.zip"
          >
            <FileText size={15} />
            下载完整 Skill 包
          </a>
          <p className="footnote">
            连接提供调用能力，Skill 规定何时同步。仅连接 MCP 并不保证 AI
            会持续上报。
          </p>
        </section>
      </div>
    </div>
  );
}

export function Team({ onError }: { onError: (s: string) => void }) {
  const [members, setMembers] = useState<User[]>([]),
    [show, setShow] = useState(false),
    [busy, setBusy] = useState(false);
  const load = () =>
    api<User[]>("/members")
      .then(setMembers)
      .catch((e) => onError(e.message));
  useEffect(() => {
    void load();
  }, []);
  async function create(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const form = e.currentTarget;
    const d = Object.fromEntries(new FormData(form));
    setBusy(true);
    try {
      await api("/members", { method: "POST", body: JSON.stringify(d) });
      form.reset();
      setShow(false);
      await load();
    } catch (e) {
      onError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="panel">
      <div className="section-title">
        <h2>团队成员</h2>
        <span>{members.length} 位成员</span>
        <button className="primary" onClick={() => setShow(!show)}>
          <Plus size={16} />
          添加成员
        </button>
      </div>
      <div className="team-note">
        第一版为单团队共享空间：成员可查看全部项目；开发成员可同步，只有管理员可确认验收。
      </div>
      {show && (
        <form className="member-form" onSubmit={create}>
          <label>
            姓名
            <input name="name" required maxLength={120} />
          </label>
          <label>
            邮箱
            <input name="email" type="email" required maxLength={190} />
          </label>
          <label>
            初始密码
            <input
              name="password"
              type="password"
              required
              minLength={12}
              maxLength={72}
              autoComplete="new-password"
              placeholder="至少 12 字符"
            />
          </label>
          <label>
            角色
            <select name="role">
              <option value="member">开发成员</option>
              <option value="viewer">只读成员</option>
              <option value="admin">管理员</option>
            </select>
          </label>
          <button className="primary" disabled={busy}>
            创建账号
          </button>
        </form>
      )}
      <div className="table-scroll">
        <table>
          <thead>
            <tr>
              <th>成员</th>
              <th>邮箱</th>
              <th>角色</th>
            </tr>
          </thead>
          <tbody>
            {members.map((m) => (
              <tr key={m.id}>
                <td>
                  <strong>{m.name}</strong>
                </td>
                <td>{m.email}</td>
                <td>{roles[m.role]}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}
