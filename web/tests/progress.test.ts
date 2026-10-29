import assert from "node:assert/strict";
import test from "node:test";
import { deliveryStage, summarizeTasks } from "../src/lib/progress.ts";
import type { Item } from "../src/types.ts";
function item(status: string, kind = "task"): Item {
  return { status, kind } as Item;
}
test("开发已提交不等于验收通过", () => {
  const s = summarizeTasks([
    item("pending_verification"),
    item("pending_acceptance"),
    item("accepted"),
    item("in_progress"),
  ]);
  assert.equal(s.total, 4);
  assert.equal(s.submitted, 3);
  assert.equal(s.accepted, 1);
  assert.equal(s.submittedPercent, 75);
  assert.equal(s.acceptedPercent, 25);
});
test("取消的单元和需求、会话不计入开发进度", () => {
  const s = summarizeTasks([
    item("accepted"),
    item("cancelled"),
    item("accepted", "requirement"),
    item("ended", "session"),
  ]);
  assert.equal(s.total, 1);
  assert.equal(s.accepted, 1);
  assert.equal(s.acceptedPercent, 100);
});
test("未拆解的需求不能显示为开发完成", () => {
  const s = summarizeTasks([item("in_progress", "requirement")]);
  assert.equal(s.total, 0);
  assert.equal(s.submittedPercent, 0);
  assert.equal(s.acceptedPercent, 0);
});
test("需求变更和验收返工需要重新开发复核", () => {
  const s = summarizeTasks([
    item("needs_review"),
    item("rejected"),
    item("blocked"),
  ]);
  assert.equal(s.submitted, 0);
  assert.equal(s.accepted, 0);
  assert.equal(s.review, 2);
  assert.equal(s.blocked, 1);
  assert.equal(deliveryStage("needs_review"), 1);
  assert.equal(deliveryStage("rejected"), 1);
  assert.equal(deliveryStage("blocked"), 1);
});
