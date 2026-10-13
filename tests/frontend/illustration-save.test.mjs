import assert from "node:assert/strict";
import test from "node:test";
import { IllustrationSaveQueue } from "../../src/frontend/js/illustration-save.js";

function deferred() {
  let resolve;
  const promise = new Promise((done) => { resolve = done; });
  return { promise, resolve };
}

test("close waits for a delayed save and drains edits made during it", async () => {
  const first = deferred();
  const second = deferred();
  let calls = 0;
  const queue = new IllustrationSaveQueue(() => (++calls === 1 ? first.promise : second.promise));
  queue.markDirty();
  const closing = queue.flush();
  assert.equal(calls, 1);
  queue.markDirty();
  assert.equal(queue.flush(), closing);
  first.resolve();
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(calls, 2);
  assert.equal(queue.dirty, true);
  let closed = false;
  closing.then(() => { closed = true; });
  assert.equal(closed, false);
  second.resolve();
  await closing;
  assert.equal(closed, true);
  assert.equal(queue.dirty, false);
});

test("failed saves retain dirty state and can be retried", async () => {
  let fails = true;
  const queue = new IllustrationSaveQueue(async () => { if (fails) throw new Error("offline"); });
  queue.markDirty();
  await assert.rejects(queue.flush(), /offline/);
  assert.equal(queue.dirty, true);
  fails = false;
  await queue.flush();
  assert.equal(queue.dirty, false);
});

test("opening or closing a clean preview causes no writes", async () => {
  let calls = 0;
  const queue = new IllustrationSaveQueue(async () => { calls++; });
  await queue.flush();
  assert.equal(calls, 0);
});
