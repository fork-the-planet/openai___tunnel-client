<script lang="ts">
  import { onDestroy, onMount } from "svelte";
  import { fetchJSON } from "../lib/api";
  import { fmtUptime } from "../lib/format";
  import type { StatusResponse } from "../lib/types";
  import ChannelTable from "./ChannelTable.svelte";

  let status: StatusResponse | null = null;
  let errorMessage = "";
  let refreshTimer: number | undefined;

  const warningHeading = "Warnings";

  async function refreshStatus(): Promise<void> {
    errorMessage = "";
    try {
      status = await fetchJSON<StatusResponse>("/api/status");
    } catch (err) {
      errorMessage = `error: ${String(err)}`;
    }
  }

  function copyTunnelId(): void {
    const id = status?.control_plane_tunnel_id;
    if (!id) return;
    if (navigator?.clipboard?.writeText) {
      navigator.clipboard.writeText(id);
    }
  }

  onMount(() => {
    refreshStatus();
    refreshTimer = window.setInterval(refreshStatus, 10000);
    return () => {
      if (refreshTimer) window.clearInterval(refreshTimer);
    };
  });

  onDestroy(() => {
    if (refreshTimer) window.clearInterval(refreshTimer);
  });
</script>

<div class="grid">
  <div class="card span-6">
    <div class="muted small">Client</div>
    <div class="kv" style="margin-top: 8px">
      <div class="muted">Version</div>
      <div class="mono">{status?.version || "—"}</div>
      <div class="muted">Uptime</div>
      <div class="mono">{fmtUptime(status?.uptime_seconds || 0)}</div>
      <div class="muted">Health addr</div>
      <div class="mono">{status?.health_listen_addr || "—"}</div>
    </div>
  </div>

  <div class="card span-6">
    <div class="muted small">Control plane</div>
    <div class="kv" style="margin-top: 8px">
      <div class="muted">Base URL</div>
      <div class="mono">{status?.control_plane_base_url || "—"}</div>
      <div class="muted">Tunnel ID</div>
      <div class="row" style="align-items: baseline">
        <span class="mono">{status?.control_plane_tunnel_id || "—"}</span>
        <button class="small" type="button" on:click={copyTunnelId}>Copy</button>
      </div>
      <div class="muted">Tunnel name</div>
      <div class="mono">{status?.tunnel_metadata?.name || "—"}</div>
      <div class="muted">Description</div>
      <div class="mono">{status?.tunnel_metadata?.description || "—"}</div>
      <div class="muted">Poll timeout</div>
      <div class="mono">{status?.control_plane_poll_timeout || "—"}</div>
      <div class="muted">Max inflight</div>
      <div class="mono">
        {status?.control_plane_max_inflight ?? "—"}
      </div>
    </div>
  </div>

  {#if status?.warnings?.length}
    <div class="card span-12">
      <div class="muted small">{warningHeading}</div>
      <div style="margin-top: 6px">
        {#each status.warnings as warning}
          <div>• {warning}</div>
        {/each}
      </div>
    </div>
  {/if}

  <div class="card span-12">
    <div class="muted small">Channels</div>
    <ChannelTable channels={status?.channels ?? []} />
  </div>

  <div class="card span-12">
    <details>
      <summary class="muted">Raw status JSON</summary>
      <pre class="pre mono" style="margin-top: 10px">
{status ? JSON.stringify(status, null, 2) : errorMessage || "—"}</pre
      >
    </details>
  </div>
</div>
