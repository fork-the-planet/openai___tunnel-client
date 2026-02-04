<script lang="ts">
  import type { ChannelDetail, ChannelStatus } from "../lib/types";

  export let channels: ChannelStatus[] = [];

  let openRows = new Set<number>();

  function toggleRow(index: number): void {
    const next = new Set(openRows);
    if (next.has(index)) {
      next.delete(index);
    } else {
      next.add(index);
    }
    openRows = next;
  }

  function channelDetails(channel: ChannelStatus): ChannelDetail[] {
    const details = Array.isArray(channel.details) ? channel.details : [];
    const allowed = new Set(["http-streamable", "stdio"]);
    if (!allowed.has(channel.transport_kind || "")) {
      return [];
    }
    return details.filter((detail) => detail?.key || detail?.value);
  }
</script>

<table class="oauth-table" style="margin-top: 12px">
  <thead>
    <tr>
      <th class="channel-expand-cell"></th>
      <th>Name</th>
      <th>Status</th>
      <th>Server</th>
      <th>Transport</th>
      <th>Reason</th>
    </tr>
  </thead>
  <tbody>
    {#if !channels || channels.length === 0}
      <tr>
        <td class="muted" colspan="6">No channel data</td>
      </tr>
    {:else}
      {#each channels as channel, idx}
        {@const details = channelDetails(channel)}
        <tr>
          <td class="channel-expand-cell">
            {#if details.length > 0}
              <button class="channel-expand small" type="button" on:click={() => toggleRow(idx)}>
                {openRows.has(idx) ? "− Details" : "+ Details"}
              </button>
            {/if}
          </td>
          <td class="mono">{channel.name || "—"}</td>
          <td>
            <span class={`badge ${channel.enabled ? "ok" : "warn"}`}>
              {channel.enabled ? "enabled" : "disabled"}
            </span>
          </td>
          <td class="mono">{channel.server_kind || "—"}</td>
          <td class="mono">{channel.transport_kind || "—"}</td>
          <td class="small">{channel.reason || "—"}</td>
        </tr>
        {#if details.length > 0}
          <tr class="channel-detail-row" hidden={!openRows.has(idx)}>
            <td class="channel-expand-cell"></td>
            <td colspan="5">
              <div class="channel-detail-wrap">
                <div class="muted small">Details</div>
                <div class="channel-detail-kv">
                  {#each details as detail}
                    <div class="muted mono">{detail.key || "—"}</div>
                    <div class="mono">{detail.value || "—"}</div>
                  {/each}
                </div>
              </div>
            </td>
          </tr>
        {/if}
      {/each}
    {/if}
  </tbody>
</table>
