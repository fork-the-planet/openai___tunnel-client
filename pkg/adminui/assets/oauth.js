(function () {
  const adminUI = (window.adminUI = window.adminUI || {});
  const $ = adminUI.$ || ((id) => document.getElementById(id));
  const fetchJSON =
    adminUI.fetchJSON ||
    (async (path) => {
      const res = await fetch(path, { cache: "no-store" });
      if (!res.ok) throw new Error(res.status + " " + res.statusText);
      return await res.json();
    });

  const expandedRows = new Set();

  const discoveryOrder = [
    { source: "www_authenticate" },
    { source: "well_known_path" },
    { source: "well_known_root" },
  ];

  function fallbackURLs(discoveryUrls) {
    const urls = Array.isArray(discoveryUrls) ? discoveryUrls : [];
    const out = { well_known_path: "", well_known_root: "" };
    if (urls.length === 1) {
      out.well_known_root = urls[0];
    } else if (urls.length >= 2) {
      if (urls.length >= 3) {
        out.well_known_path = urls[1];
        out.well_known_root = urls[2];
      } else {
        out.well_known_path = urls[0];
        out.well_known_root = urls[1];
      }
    }
    return out;
  }

  function fmtTimestamp(ts) {
    if (!ts) return "—";
    const d = new Date(ts);
    if (Number.isNaN(d.getTime())) return ts;
    return d.toISOString();
  }

  function reasonForAttempt(attempt, pending, type, probe) {
    if (attempt) {
      if (attempt.error) return attempt.error;
      if (!attempt.tried && !pending) {
        return "Skipped: higher-priority selected";
      }
    }
    if (type === "www_authenticate" && probe && probe.error) {
      return probe.error;
    }
    return "";
  }

  function buildPRMDRows(payload) {
    const pending = payload ? payload.pending : false;
    const probe = payload ? payload.www_authenticate_probe : null;
    const attempts =
      payload && payload.metadata && Array.isArray(payload.metadata.attempts)
        ? payload.metadata.attempts
        : [];
    const attemptsBySource = new Map();
    attempts.forEach((attempt) => {
      if (attempt && attempt.source) attemptsBySource.set(attempt.source, attempt);
    });

    const fallback = fallbackURLs(payload ? payload.discovery_urls : []);
    const rows = [];
    discoveryOrder.forEach((entry, idx) => {
      const type = entry.source;
      const attempt = attemptsBySource.get(type);
      const reason = reasonForAttempt(attempt, pending, type, probe);

      let urlText = attempt ? attempt.url : "";
      if (type === "www_authenticate") {
        if (!urlText && probe && probe.url) urlText = probe.url;
      } else if (!urlText) {
        urlText = type === "well_known_path" ? fallback.well_known_path : fallback.well_known_root;
      }
      if (!urlText) urlText = "—";

      let status = "skipped";
      if (attempt && attempt.selected) {
        status = "fetched";
      } else if (attempt && attempt.tried && attempt.error) {
        status = "error";
      } else if (attempt && attempt.tried) {
        status = "tried";
      } else if (pending) {
        status = "pending";
      }

      rows.push({
        key: "prmd|" + type + "|" + urlText,
        priority: idx + 1,
        step: type,
        url: urlText,
        status,
        details: {
          statusCode: attempt ? attempt.status_code : 0,
          status: status,
          fetchedAt: payload && payload.metadata ? payload.metadata.fetched_at : "",
          source: payload ? payload.metadata_source : "",
          sourceURL: urlText,
          headers: attempt && attempt.selected && payload && payload.metadata ? payload.metadata.headers : null,
          body: attempt && attempt.selected && payload && payload.metadata ? payload.metadata.body : null,
          bodyText: attempt && attempt.selected && payload && payload.metadata ? payload.metadata.body_text : "",
          error: reason,
        },
      });
    });

    return rows;
  }

  function buildAuthMetaRows(payload, startPriority) {
    const rows = [];
    const authMeta = payload && payload.metadata ? payload.metadata.auth_server_metadata : null;
    const attempts = authMeta && Array.isArray(authMeta.attempts) ? authMeta.attempts : [];
    attempts.forEach((attempt, idx) => {
      let status = "skipped";
      if (attempt && attempt.selected) {
        status = "fetched";
      } else if (attempt && attempt.tried && attempt.error) {
        status = "error";
      } else if (attempt && attempt.tried) {
        status = "tried";
      }

      const stepBits = ["auth_server_meta"];
      if (attempt && attempt.document) stepBits.push(String(attempt.document));
      if (attempt && attempt.path_style) stepBits.push("(" + String(attempt.path_style) + ")");

      rows.push({
        key: "auth|" + idx + "|" + (attempt && attempt.url ? attempt.url : "—"),
        priority: startPriority + idx,
        step: stepBits.join(" "),
        url: attempt && attempt.url ? attempt.url : "—",
        status,
        details: {
          statusCode: attempt ? attempt.status_code : 0,
          status: status,
          fetchedAt: payload && payload.metadata ? payload.metadata.fetched_at : "",
          source: "auth_server_metadata",
          sourceURL: attempt && attempt.url ? attempt.url : "—",
          headers: attempt ? attempt.headers : null,
          body: attempt ? attempt.body : null,
          bodyText: attempt ? attempt.body_text : "",
          error: attempt ? attempt.error : "",
        },
      });
    });
    return rows;
  }

  function hasDetails(details) {
    if (!details) return false;
    return !!(
      details.statusCode ||
      details.error ||
      (details.headers && Object.keys(details.headers).length) ||
      details.body ||
      details.bodyText ||
      details.fetchedAt
    );
  }

  function appendKVRow(parent, key, value) {
    const kEl = document.createElement("div");
    kEl.className = "muted";
    kEl.textContent = key;
    const vEl = document.createElement("div");
    vEl.className = "mono";
    vEl.textContent = value;
    parent.appendChild(kEl);
    parent.appendChild(vEl);
  }

  function appendCollapsibleSection(parent, titleText, contentText) {
    const detailsEl = document.createElement("details");
    detailsEl.className = "oauth-detail-collapse";
    detailsEl.open = false;

    const summary = document.createElement("summary");
    summary.className = "oauth-detail-summary";
    summary.textContent = titleText;
    detailsEl.appendChild(summary);

    if (contentText && contentText.trim() !== "") {
      const pre = document.createElement("pre");
      pre.className = "pre mono oauth-detail-pre";
      pre.textContent = contentText;
      detailsEl.appendChild(pre);
    } else {
      const empty = document.createElement("div");
      empty.className = "oauth-detail-empty";
      detailsEl.appendChild(empty);
    }
    parent.appendChild(detailsEl);
  }

  function renderDetailContent(detailCell, details) {
    const wrapper = document.createElement("div");
    wrapper.className = "oauth-detail-wrap";

    const title = document.createElement("div");
    title.className = "oauth-detail-title";
    title.textContent = "OAuth metadata";
    wrapper.appendChild(title);

    const kv = document.createElement("div");
    kv.className = "oauth-detail-kv";
    appendKVRow(kv, "Status", details && details.status ? details.status : "—");
    appendKVRow(kv, "Fetched at", fmtTimestamp(details ? details.fetchedAt : ""));
    appendKVRow(kv, "Source URL", details && details.sourceURL ? details.sourceURL : "—");
    appendKVRow(kv, "Metadata source", details && details.source ? details.source : "—");
    appendKVRow(kv, "HTTP status", details && details.statusCode ? "HTTP " + details.statusCode : "—");
    appendKVRow(kv, "Error", details && details.error ? details.error : "—");
    wrapper.appendChild(kv);

    const headersText =
      details && details.headers && Object.keys(details.headers).length
        ? JSON.stringify(details.headers, null, 2)
        : "";
    appendCollapsibleSection(wrapper, "Headers", headersText);

    const bodyText =
      details && details.body
        ? JSON.stringify(details.body, null, 2)
        : details && details.bodyText
          ? details.bodyText
          : "";
    appendCollapsibleSection(wrapper, "Body", bodyText);

    detailCell.appendChild(wrapper);
  }

  function renderDiscoveryTable(payload) {
    const tbody = $("oauthDiscoveryRows");
    if (!tbody) return;
    tbody.textContent = "";

    const prmdRows = buildPRMDRows(payload || null);
    const authRows = buildAuthMetaRows(payload || null, prmdRows.length + 1);
    const rows = prmdRows.concat(authRows);

    if (!rows.length) {
      const tr = document.createElement("tr");
      const td = document.createElement("td");
      td.colSpan = 5;
      td.className = "muted";
      td.textContent = "No discovery rows.";
      tr.appendChild(td);
      tbody.appendChild(tr);
      return;
    }

    rows.forEach((row) => {
      const expandable = hasDetails(row.details);

      const tr = document.createElement("tr");

      const expandCell = document.createElement("td");
      expandCell.className = "oauth-expand-cell";
      let detailRow = null;
      if (expandable) {
        const btn = document.createElement("button");
        btn.type = "button";
        btn.className = "oauth-expand";
        btn.setAttribute("aria-label", "Toggle details");
        expandCell.appendChild(btn);

        detailRow = document.createElement("tr");
        detailRow.className = "oauth-detail-row";
        const detailCell = document.createElement("td");
        detailCell.colSpan = 5;
        renderDetailContent(detailCell, row.details);
        detailRow.appendChild(detailCell);

        const setExpanded = (expanded) => {
          detailRow.style.display = expanded ? "table-row" : "none";
          btn.textContent = expanded ? "−" : "+";
          if (expanded) {
            expandedRows.add(row.key);
          } else {
            expandedRows.delete(row.key);
          }
        };

        btn.addEventListener("click", () => {
          setExpanded(detailRow.style.display === "none");
        });

        setExpanded(expandedRows.has(row.key));
      }

      const priorityCell = document.createElement("td");
      priorityCell.textContent = String(row.priority);

      const stepCell = document.createElement("td");
      stepCell.textContent = row.step;

      const urlCell = document.createElement("td");
      urlCell.className = "mono oauth-url";
      urlCell.textContent = row.url;

      const statusCell = document.createElement("td");
      const statusPill = document.createElement("span");
      statusPill.className = "oauth-status oauth-status-" + row.status;
      statusPill.textContent = row.status;
      statusCell.appendChild(statusPill);

      tr.appendChild(expandCell);
      tr.appendChild(priorityCell);
      tr.appendChild(stepCell);
      tr.appendChild(urlCell);
      tr.appendChild(statusCell);

      tbody.appendChild(tr);
      if (detailRow) tbody.appendChild(detailRow);
    });
  }

  async function refreshOAuth() {
    const errEl = $("oauthErr");
    if (errEl) errEl.textContent = "";
    try {
      const payload = await fetchJSON("/api/oauth");
      renderDiscoveryTable(payload);
      if (payload.error && errEl) {
        errEl.textContent = payload.error;
      }
    } catch (e) {
      if (errEl) errEl.textContent = "error: " + e;
      renderDiscoveryTable(null);
    }
  }

  const refreshButton = $("refreshOAuth");
  if (refreshButton) {
    refreshButton.addEventListener("click", refreshOAuth);
  }

  adminUI.oauth = {
    refresh: refreshOAuth,
  };
})();
