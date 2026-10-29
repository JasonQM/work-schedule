import { FolderGit2, LayoutDashboard, Link2, Users } from "lucide-react";
import React from "react";

export const statuses: Record<string, string> = {
  todo: "待处理",
  in_progress: "进行中",
  blocked: "已阻塞",
  pending_verification: "待验证",
  pending_acceptance: "待验收",
  accepted: "已验收",
  rejected: "验收未通过",
  needs_review: "待复核",
  cancelled: "已取消",
  ended: "已结束",
};
export const roles: Record<string, string> = {
  admin: "管理员",
  member: "开发成员",
  viewer: "只读成员",
};
export const actionNames: Record<string, string> = {
  "project.created": "接入了项目",
  "item.created": "创建了记录",
  "item.updated": "更新了开发状态",
  "requirement.changed": "变更了需求",
};
export const tabs = [
  ["overview", "工作总览"],
  ["projects", "项目空间"],
  ["integrations", "AI 接入"],
  ["team", "团队成员"],
] as const;
export const icons: Record<string, React.ReactNode> = {
  overview: <LayoutDashboard size={18} />,
  projects: <FolderGit2 size={18} />,
  integrations: <Link2 size={18} />,
  team: <Users size={18} />,
};
