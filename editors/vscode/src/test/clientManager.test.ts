import assert from "node:assert/strict";
import test from "node:test";

import { LanguageClientManager, ManagedClient } from "../lsp/clientManager";

class FakeClient implements ManagedClient {
  startCalls = 0;
  stopCalls = 0;

  private readonly startImpl: () => Promise<void>;

  constructor(startImpl?: () => Promise<void>) {
    this.startImpl = startImpl ?? (async () => undefined);
  }

  async start(): Promise<void> {
    this.startCalls += 1;
    await this.startImpl();
  }

  async stop(): Promise<void> {
    this.stopCalls += 1;
  }
}

test("LanguageClientManager coalesces concurrent start calls", async () => {
  let createCalls = 0;
  const client = new FakeClient(async () => {
    await new Promise((resolve) => setTimeout(resolve, 15));
  });
  const manager = new LanguageClientManager({
    createClient: () => {
      createCalls += 1;
      return {
        startupMessage: "start",
        client
      };
    },
    output: {
      appendLine: () => undefined
    },
    onStartError: async () => undefined
  });

  await Promise.all([manager.start(), manager.start(), manager.start()]);

  assert.equal(createCalls, 1);
  assert.equal(client.startCalls, 1);
});

test("LanguageClientManager restart performs stop then start", async () => {
  const created: FakeClient[] = [];
  const manager = new LanguageClientManager({
    createClient: () => {
      const client = new FakeClient();
      created.push(client);
      return {
        startupMessage: "start",
        client
      };
    },
    output: {
      appendLine: () => undefined
    },
    onStartError: async () => undefined
  });

  await manager.start();
  await manager.restart();

  assert.equal(created.length, 2);
  assert.equal(created[0].stopCalls, 1);
  assert.equal(created[1].startCalls, 1);
});

test("LanguageClientManager clears failed start state and reports error", async () => {
  const failure = new Error("boom");
  let errorCalls = 0;
  let createCalls = 0;
  const manager = new LanguageClientManager({
    createClient: () => {
      createCalls += 1;
      return {
        startupMessage: "start",
        client: new FakeClient(async () => {
          throw failure;
        })
      };
    },
    output: {
      appendLine: () => undefined
    },
    onStartError: async (err) => {
      assert.equal(err, failure);
      errorCalls += 1;
    }
  });

  await manager.start();
  await manager.start();

  assert.equal(createCalls, 2);
  assert.equal(errorCalls, 2);
});
