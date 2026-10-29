import type { Item } from "../types";

export const deliverySteps = ["待开发", "开发", "验证", "验收"];
export function deliveryStage(status: string): number {
  if (status === "accepted") return 4;
  if (status === "pending_acceptance") return 3;
  if (status === "pending_verification") return 2;
  if (["in_progress", "blocked", "rejected", "needs_review"].includes(status))
    return 1;
  return 0;
}
export function summarizeTasks(items: Item[]) {
  const tasks = items.filter(
    (i) => i.kind === "task" && i.status !== "cancelled",
  );
  const submitted = tasks.filter((i) =>
    ["pending_verification", "pending_acceptance", "accepted"].includes(
      i.status,
    ),
  ).length;
  const accepted = tasks.filter((i) => i.status === "accepted").length;
  return {
    total: tasks.length,
    submitted,
    accepted,
    developing: tasks.filter((i) => i.status === "in_progress").length,
    verifying: tasks.filter((i) => i.status === "pending_verification").length,
    pending: tasks.filter((i) => i.status === "pending_acceptance").length,
    blocked: tasks.filter((i) => i.status === "blocked").length,
    review: tasks.filter((i) => ["needs_review", "rejected"].includes(i.status))
      .length,
    submittedPercent: tasks.length
      ? Math.round((submitted / tasks.length) * 100)
      : 0,
    acceptedPercent: tasks.length
      ? Math.round((accepted / tasks.length) * 100)
      : 0,
  };
}
