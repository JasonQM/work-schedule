import {
  ArrowRight,
  Code2,
  Command,
  FileText,
  Loader2,
  ShieldCheck,
} from "lucide-react";
import React, { useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import "./style.css";

import { Brand } from "./components/shared";
import { api } from "./lib/api";
import type { User } from "./types";
import { Workspace } from "./Workspace";
function App() {
  const [user, setUser] = useState<User | null>(null),
    [loading, setLoading] = useState(true),
    [error, setError] = useState("");
  useEffect(() => {
    api<User>("/me")
      .then(setUser)
      .catch((e) => {
        if (e.message !== "AUTH") setError(e.message);
      })
      .finally(() => setLoading(false));
  }, []);
  if (loading)
    return (
      <div className="loading">
        <Loader2 className="spin" /> 正在连接工作台
      </div>
    );
  return user ? (
    <Workspace user={user} logout={() => setUser(null)} />
  ) : (
    <Login onLogin={setUser} initialError={error} />
  );
}
function Login({
  onLogin,
  initialError,
}: {
  onLogin: (u: User) => void;
  initialError: string;
}) {
  const [email, setEmail] = useState(""),
    [password, setPassword] = useState(""),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(initialError);
  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api("/login", {
        method: "POST",
        body: JSON.stringify({ email, password }),
      });
      onLogin(await api<User>("/me"));
    } catch (e) {
      setError(
        (e as Error).message === "AUTH"
          ? "邮箱或密码错误"
          : (e as Error).message,
      );
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="login">
      <div className="login-story">
        <Brand />
        <div>
          <span className="eyebrow">BUILT FOR AI-NATIVE TEAMS</span>
          <h1>
            开发在继续，
            <br />
            进展始终可见。
          </h1>
          <p>
            从需求到交付，让 AI 自动连接每一段工作。
            <br />
            团队共用一份清晰、可追溯的项目进度。
          </p>
          <div className="story-flow">
            <span>
              <FileText size={19} /> 需求
            </span>
            <ArrowRight size={17} />
            <span>
              <Code2 size={19} /> 开发
            </span>
            <ArrowRight size={17} />
            <span>
              <ShieldCheck size={19} /> 验收
            </span>
          </div>
        </div>
        <small>Work Schedule / 团队开发工作台</small>
      </div>
      <div className="login-form">
        <form onSubmit={submit}>
          <span className="login-mark">
            <Command size={26} />
          </span>
          <h2>进入你的工作空间</h2>
          <p>使用团队账号，查看正在发生的开发。</p>
          <label>
            邮箱
            <input
              type="email"
              required
              autoComplete="username"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="you@company.com"
            />
          </label>
          <label>
            密码
            <input
              type="password"
              required
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="输入账号密码"
            />
          </label>
          {error && (
            <div role="alert" className="error">
              {error}
            </div>
          )}
          <button className="primary full" disabled={busy}>
            {busy ? (
              <Loader2 className="spin" size={17} />
            ) : (
              <>
                进入工作台 <ArrowRight size={17} />
              </>
            )}
          </button>
          <small>账号由团队管理员创建，项目由开发 AI 自动接入。</small>
        </form>
      </div>
    </div>
  );
}
createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
