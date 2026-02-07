import { fireEvent, render, waitFor } from "@testing-library/svelte";

import App from "../../App.svelte";
import { jsonResponse, mockFetch, textResponse } from "../../test/mockFetch";

function installAppFetchMock() {
  return mockFetch(async (url) => {
    if (url.endsWith("/healthz") || url.endsWith("/readyz")) {
      return textResponse("ok");
    }
    if (url.endsWith("/metrics")) {
      return textResponse("commands_poll_cycles_total 1\n");
    }
    if (url.includes("/api/status")) {
      return jsonResponse({ channels: [], mcp_routes: [], control_plane_route: { route_mode: "direct" } });
    }
    if (url.includes("/api/oauth")) {
      return jsonResponse({});
    }
    if (url.includes("/api/harpoon/status")) {
      return jsonResponse({ enabled: false, reason: "disabled", proxy_routes: [] });
    }
    if (url.includes("/api/system")) {
      return jsonResponse({ tls: { system_trust: { enabled: true, source: "system cert pool" } } });
    }
    if (url.includes("/api/logs?limit=500")) {
      return jsonResponse({ events: [] });
    }
    return new Response("not found", { status: 404, statusText: "Not Found" });
  });
}

describe("App", () => {
  it("registers and switches to the System tab", async () => {
    installAppFetchMock();

    const { container, getByRole } = render(App);

    const systemTab = getByRole("tab", { name: "System" });
    expect(systemTab).toBeTruthy();

    await fireEvent.click(systemTab);

    await waitFor(() => {
      const panel = container.querySelector("#panel-system");
      expect(panel?.getAttribute("aria-hidden")).toBe("false");
    });

    const overviewPanel = container.querySelector("#panel-overview");
    expect(overviewPanel?.getAttribute("aria-hidden")).toBe("true");
  });
});
