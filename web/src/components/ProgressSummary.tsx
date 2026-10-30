import { summarizeTasks } from "../lib/progress";
import type { Item } from "../types";
export function ProgressSummary({
  items,
  compact = false,
}: {
  items: Item[];
  compact?: boolean;
}) {
  const s = summarizeTasks(items);
  const stages = [
    ["待开发", s.todo],
    ["进行中", s.developing],
    ["待验证", s.verifying],
    ["待验收", s.pending],
    ["已验收", s.accepted],
    ["阻塞", s.blocked],
    ["待复核", s.needsReview],
    ["验收未通过", s.rejected],
  ] as const;
  return (
    <div
      className={
        compact ? "delivery-summary compact-summary" : "delivery-summary"
      }
    >
      <div>
        <span>
          开发已提交{" "}
          <strong>
            {s.submitted}
            <small> / {s.total}</small>
          </strong>
        </span>
        <div className="progress development">
          <span style={{ width: s.submittedPercent + "%" }} />
        </div>
      </div>
      <div>
        <span>
          验收已通过{" "}
          <strong>
            {s.accepted}
            <small> / {s.total}</small>
          </strong>
        </span>
        <div className="progress acceptance">
          <span style={{ width: s.acceptedPercent + "%" }} />
        </div>
      </div>
      <div className="delivery-states" aria-label="开发单元当前阶段">
        {stages
          .filter(([, count]) => count > 0)
          .map(([label, count]) => (
            <span
              key={label}
              className={label === "待复核" ? "review-state" : ""}
            >
              {label} <b>{count}</b>
            </span>
          ))}
        {s.total === 0 && <span>尚无开发单元</span>}
      </div>
      {s.needsReview > 0 && (
        <p className="delivery-review-note">
          {s.needsReview} 个单元待按当前需求复核，暂不计入已提交；开发 AI
          核对后需同步实际状态和验证依据。
        </p>
      )}
    </div>
  );
}
