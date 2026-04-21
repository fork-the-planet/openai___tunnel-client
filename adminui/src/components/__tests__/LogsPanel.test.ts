import { fireEvent, render, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import LogsPanel from "../LogsPanel.svelte";
import { jsonResponse, mockFetchRequest } from "../../test/mockFetch";

class MockEventSource {
  onopen: ((event: Event) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;

  constructor(_url: string) {
    queueMicrotask(() => {
      this.onopen?.(new Event("open"));
    });
  }

  addEventListener(_type: string, _listener: EventListenerOrEventListenerObject): void {}

  close(): void {}
}

describe("LogsPanel", () => {
  beforeEach(() => {
    vi.stubGlobal("EventSource", MockEventSource);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("updates the runtime log level through the admin API", async () => {
    let currentLevel = "info";
    let lastPutBody = "";

    mockFetchRequest(async ({ url, init }) => {
      if (url.includes("/api/logs?limit=500")) {
        return jsonResponse({ events: [] });
      }
      if (url.endsWith("/api/log-level")) {
        const method = (init?.method || "GET").toUpperCase();
        if (method === "PUT") {
          lastPutBody = String(init?.body || "");
          currentLevel = JSON.parse(lastPutBody).level;
        }
        return jsonResponse({
          level: currentLevel,
          supported_levels: ["debug", "info", "warn"],
        });
      }
      return new Response("not found", { status: 404, statusText: "Not Found" });
    });

    const { getByRole, findByText } = render(LogsPanel, {
      onConnectionChange: () => {},
    });

    const runtimeLevelSelect = getByRole("combobox", { name: "Runtime log level" }) as HTMLSelectElement;
    await waitFor(() => {
      expect(runtimeLevelSelect.value).toBe("info");
    });

    await fireEvent.change(runtimeLevelSelect, { target: { value: "debug" } });
    await fireEvent.click(getByRole("button", { name: "Apply runtime level" }));

    await findByText("runtime log level: debug");
    expect(lastPutBody).toBe(JSON.stringify({ level: "debug" }));
  });
});
