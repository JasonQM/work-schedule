import {
  Blocks,
  ChevronDown,
  ChevronRight,
  CircleDot,
  FolderGit2,
  Loader2,
  LogOut,
  RefreshCw,
  Search,
  ShieldCheck,
  TriangleAlert,
} from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { Integrations, PasswordModal, Team } from "./components/Management";
import { ProjectExplorer } from "./components/ProjectExplorer";
import {
  Brand,
  Empty,
  ProjectCard,
  Section,
  Stat,
  Timeline,
} from "./components/shared";
import { api } from "./lib/api";
import { icons, roles, statuses, tabs } from "./lib/constants";
import type { Snapshot, User } from "./types";

export function Workspace({
  user,
  logout,
}: {
  user: User;
  logout: () => void;
}) {
  const [tab, setTab] = useState("overview");
  const [data, setData] = useState<Snapshot>({
    user,
    projects: [],
    items: [],
    events: [],
    cursor: 0,
  });
  const [ready, setReady] = useState(false),
    [error, setError] = useState(""),
    [query, setQuery] = useState("");
  const [connected, setConnected] = useState(false),
    [updated, setUpdated] = useState(""),
    [refreshing, setRefreshing] = useState(false);
  const [mobile, setMobile] = useState(false),
    [passwordOpen, setPasswordOpen] = useState(false);
  const [selection, setSelection] = useState<{
    projectId: string;
    itemId?: string;
  } | null>(null);
  const alive = useRef(true),
    sequence = useRef(-1);
  const refresh = useCallback(async () => {
    try {
      const d = await api<Snapshot>("/snapshot");
      if (!alive.current || d.cursor < sequence.current) return;
      setData(d);
      sequence.current = d.cursor;
      setReady(true);
      setError("");
      setUpdated(new Date().toISOString());
    } catch (e) {
      if ((e as Error).message === "AUTH") logout();
      else setError((e as Error).message);
    }
  }, [logout]);
  const refreshRef = useRef(refresh);
  refreshRef.current = refresh;
  useEffect(() => {
    alive.current = true;
    void refreshRef.current();
    const stream = new EventSource("/api/events");
    stream.onopen = () => setConnected(true);
    stream.addEventListener("change", (e) => {
      setConnected(true);
      if (Number((e as MessageEvent).data) !== sequence.current)
        void refreshRef.current();
    });
    stream.onerror = () => setConnected(false);
    const timer = setInterval(() => void refreshRef.current(), 60000);
    return () => {
      alive.current = false;
      stream.close();
      clearInterval(timer);
    };
  }, []);
  const go = (value: string) => {
    setTab(value);
    setQuery("");
    setMobile(false);
  };
  const needle = query.trim().toLowerCase();
  // Search matches a project's descendants but always opens the complete project context.
  const projects = data.projects.filter(
    (p) =>
      !needle ||
      [p.name, p.summary, p.repository]
        .join(" ")
        .toLowerCase()
        .includes(needle) ||
      data.items.some(
        (i) =>
          i.project_id === p.id &&
          [i.title, i.description, i.external_key]
            .join(" ")
            .toLowerCase()
            .includes(needle),
      ),
  );
  const projectIds = new Set(projects.map((p) => p.id));
  const tasks = data.items.filter(
    (i) =>
      i.kind === "task" &&
      i.status !== "cancelled" &&
      projectIds.has(i.project_id),
  );
  const attention = data.items.filter(
    (i) =>
      i.kind !== "session" &&
      projectIds.has(i.project_id) &&
      ["blocked", "needs_review", "rejected", "pending_acceptance"].includes(
        i.status,
      ),
  );
  const pending = tasks.filter((i) => i.status === "pending_acceptance");
  const risks = tasks.filter((i) =>
    ["blocked", "needs_review", "rejected"].includes(i.status),
  );
  const selectedProject =
    selection && data.projects.find((p) => p.id === selection.projectId);
  async function manualRefresh() {
    setRefreshing(true);
    await refresh();
    setRefreshing(false);
  }
  return (
    <div className="app">
      <aside className={mobile ? "sidebar open" : "sidebar"}>
        <Brand />
        <div className="workspace-switch">
          <span className="workspace-avatar">W</span>
          <span>
            团队工作空间<small>按项目组织 · AI 自动同步</small>
          </span>
          <ChevronDown size={15} />
        </div>
        <div className="nav-label">工作空间</div>
        <nav aria-label="工作空间">
          {tabs.slice(0, 2).map(([key, label]) => (
            <button
              key={key}
              onClick={() => go(key)}
              className={tab === key ? "active" : ""}
            >
              {icons[key]}
              {label}
            </button>
          ))}
        </nav>
        <div className="nav-label second">管理与连接</div>
        <nav aria-label="管理与连接">
          {tabs
            .slice(2)
            .filter(([key]) => key !== "team" || user.role === "admin")
            .map(([key, label]) => (
              <button
                key={key}
                onClick={() => go(key)}
                className={tab === key ? "active" : ""}
              >
                {icons[key]}
                {label}
              </button>
            ))}
        </nav>
        <div className="sidebar-bottom">
          <div className="sync-note">
            <span className="green-dot" /> 从项目到每个开发单元
            <p>
              需求分层，逐步交付。
              <br />
              每一步都有进度和依据。
            </p>
          </div>
          <button
            className="profile"
            onClick={async () => {
              try {
                await api("/logout", { method: "POST" });
                logout();
              } catch (e) {
                setError((e as Error).message);
              }
            }}
          >
            <span className="avatar">{user.name.slice(0, 1)}</span>
            <span>
              {user.name}
              <small>{roles[user.role]}</small>
            </span>
            <LogOut size={16} />
          </button>
        </div>
      </aside>
      <div className="main">
        <header className="topbar">
          <div>
            <button
              className="mobile-toggle icon-button"
              onClick={() => setMobile(!mobile)}
              aria-label="展开导航"
            >
              <Blocks size={19} />
            </button>
            <span className="muted">工作空间</span>
            <ChevronRight size={13} />
            <span>{tabs.find((t) => t[0] === tab)?.[1]}</span>
          </div>
          <div>
            <span className={"connection " + (connected ? "online" : "")}>
              <span />
              {connected ? "动态连接正常" : "正在重新连接"}
            </span>
            <button
              className="top-avatar account-button"
              title="修改我的密码"
              aria-label="修改我的密码"
              onClick={() => setPasswordOpen(true)}
            >
              {user.name.slice(0, 1)}
            </button>
          </div>
        </header>
        <main>
          <div className="page-head">
            <div>
              <span className="eyebrow">YOUR DEVELOPMENT, IN SYNC</span>
              <h1>
                {tabs.find((t) => t[0] === tab)?.[1]}
                {tab === "overview" && <span className="heading-dot" />}
              </h1>
              <p>
                {
                  (
                    {
                      overview: "先看项目全貌，再深入每一个需求与开发单元。",
                      projects: "选择项目，查看介绍、大需求及其逐步交付情况。",
                      integrations:
                        "连接你的 AI，让需求拆解和开发进展自动来到这里。",
                      team: "为团队成员创建独立账号与访问身份。",
                    } as Record<string, string>
                  )[tab]
                }
              </p>
            </div>
            <button
              className="secondary"
              onClick={manualRefresh}
              disabled={refreshing}
            >
              <RefreshCw size={15} className={refreshing ? "spin" : ""} />
              刷新数据
            </button>
          </div>
          {error && (
            <div className="error" role="alert">
              {error}
              <button onClick={manualRefresh}>重试</button>
            </div>
          )}
          {["overview", "projects"].includes(tab) && (
            <div className="filters">
              <div className="search">
                <Search size={16} />
                <input
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                  placeholder="搜索项目、需求或开发单元…"
                  aria-label="搜索项目"
                />
              </div>
              <span className="hierarchy-hint">
                项目 <ChevronRight size={12} /> 大需求{" "}
                <ChevronRight size={12} /> 开发单元
              </span>
              <span className="last-sync">
                {updated
                  ? "数据更新于 " +
                    new Date(updated).toLocaleTimeString("zh-CN")
                  : "正在读取数据"}
              </span>
            </div>
          )}
          {!ready && !error ? (
            <div className="empty">
              <Loader2 className="spin" />
              正在读取工作空间
            </div>
          ) : (
            <>
              {tab === "overview" && (
                <>
                  <div className="stats">
                    <Stat
                      label="接入项目"
                      value={projects.length}
                      icon={<FolderGit2 size={19} />}
                      note="进入项目查看需求拆解"
                    />
                    <Stat
                      label="正在开发的单元"
                      value={
                        tasks.filter((i) => i.status === "in_progress").length
                      }
                      icon={<CircleDot size={19} />}
                      note={`${tasks.length} 个有效开发单元`}
                      accent
                    />
                    <Stat
                      label="待验收的单元"
                      value={pending.length}
                      icon={<ShieldCheck size={19} />}
                      note="开发提交与验收分别追踪"
                    />
                    <Stat
                      label="需要复核或处理"
                      value={risks.length}
                      icon={<TriangleAlert size={19} />}
                      note="阻塞、需求变更或验收返工"
                    />
                  </div>
                  <div className="overview-grid">
                    <section className="panel project-panel">
                      <Section
                        title="项目进展"
                        note={`${projects.length} 个项目`}
                        action={() => go("projects")}
                      />
                      {projects.length ? (
                        <div className="project-grid">
                          {projects.slice(0, 6).map((p) => (
                            <ProjectCard
                              key={p.id}
                              project={p}
                              items={data.items.filter(
                                (i) => i.project_id === p.id,
                              )}
                              onOpen={() => setSelection({ projectId: p.id })}
                            />
                          ))}
                        </div>
                      ) : (
                        <Empty
                          title={
                            query ? "没有找到相关项目" : "等待第一个项目接入"
                          }
                          body={
                            query
                              ? "可以搜索项目名称、需求或开发单元；清空搜索查看全部项目。"
                              : "开发 AI 接入后，项目及其需求拆解会自动出现在这里。"
                          }
                          action={query ? undefined : () => go("integrations")}
                        />
                      )}
                    </section>
                    <section className="panel attention-panel">
                      <Section
                        title="需要关注"
                        note={`${attention.length} 项`}
                      />
                      {attention.slice(0, 6).map((i) => (
                        <button
                          className="attention-item"
                          key={i.id}
                          onClick={() =>
                            setSelection({
                              projectId: i.project_id,
                              itemId: i.id,
                            })
                          }
                        >
                          <span className={"attention-icon " + i.status}>
                            {i.status === "blocked" ? (
                              <TriangleAlert size={16} />
                            ) : (
                              <ShieldCheck size={16} />
                            )}
                          </span>
                          <span>
                            <strong>{i.title}</strong>
                            <small>
                              {
                                data.projects.find((p) => p.id === i.project_id)
                                  ?.name
                              }{" "}
                              · {statuses[i.status]}
                            </small>
                          </span>
                          <ChevronRight size={15} />
                        </button>
                      ))}
                      {!attention.length && (
                        <div className="calm">
                          <ShieldCheck size={29} />
                          <h3>当前没有待处理事项</h3>
                          <p>
                            点击项目后，可以逐层查看
                            <br />
                            需求、开发进度和验证记录。
                          </p>
                        </div>
                      )}
                      <div className="health-footer">
                        <span className="green-dot" />
                        点击事项可定位到所属项目与需求
                      </div>
                    </section>
                  </div>
                  <section className="panel recent-project-events">
                    <Section
                      title="最近项目动态"
                      note="进入项目查看完整上下文"
                    />
                    <Timeline
                      events={data.events
                        .filter((e) => projectIds.has(e.project_id))
                        .slice(0, 5)}
                      projects={data.projects}
                    />
                  </section>
                </>
              )}
              {tab === "projects" && (
                <section className="panel">
                  <Section
                    title="全部项目"
                    note="点击项目 → 大需求 → 开发单元与验证"
                  />
                  {projects.length ? (
                    <div className="project-grid all-projects">
                      {projects.map((p) => (
                        <ProjectCard
                          key={p.id}
                          project={p}
                          items={data.items.filter(
                            (i) => i.project_id === p.id,
                          )}
                          onOpen={() => setSelection({ projectId: p.id })}
                        />
                      ))}
                    </div>
                  ) : (
                    <Empty
                      title={query ? "没有找到相关项目" : "还没有项目"}
                      body="项目由开发 AI 自动接入，需求与拆解项会归集到对应项目。"
                      action={query ? undefined : () => go("integrations")}
                    />
                  )}
                </section>
              )}
              {tab === "integrations" && <Integrations onError={setError} />}
              {tab === "team" && user.role === "admin" && (
                <Team onError={setError} />
              )}
            </>
          )}
          <footer className="page-footer">
            <span>
              Work Schedule <span className="muted">/</span> AI 开发协作工作台
            </span>
            <span>
              <span className="green-dot" />
              项目有全貌，开发有步骤，验证有依据
            </span>
          </footer>
        </main>
      </div>
      {passwordOpen && <PasswordModal close={() => setPasswordOpen(false)} />}
      {selectedProject && selection && (
        <ProjectExplorer
          key={selectedProject.id + ":" + (selection.itemId || "")}
          project={selectedProject}
          items={data.items.filter((i) => i.project_id === selectedProject.id)}
          events={data.events.filter(
            (e) => e.project_id === selectedProject.id,
          )}
          initialItemId={selection.itemId}
          user={user}
          close={() => setSelection(null)}
          saved={refresh}
          connected={connected}
          syncError={error}
        />
      )}
    </div>
  );
}
