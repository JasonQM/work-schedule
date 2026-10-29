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
    </div>
  );
}
