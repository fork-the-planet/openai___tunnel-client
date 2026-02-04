export type BadgeKind = "ok" | "warn" | "bad";

export interface StatusResponse {
  version?: string;
  started_at?: string;
  uptime_seconds?: number;
  health_listen_addr?: string;
  control_plane_base_url?: string;
  control_plane_tunnel_id?: string;
  control_plane_max_inflight?: number;
  control_plane_poll_timeout?: string;
  mcp_server_url?: string;
  mcp_resource_metadata_urls?: string[];
  channels?: ChannelStatus[];
  raw_http_logging_enabled?: boolean;
  tunnel_metadata?: TunnelMetadata;
  tunnel_metadata_error?: string;
  warnings?: string[];
}

export interface TunnelMetadata {
  name?: string;
  description?: string;
}

export interface ChannelStatus {
  name?: string;
  enabled?: boolean;
  server_kind?: string;
  transport_kind?: string;
  reason?: string;
  details?: ChannelDetail[];
}

export interface ChannelDetail {
  key?: string;
  value?: string;
}

export interface LogsResponse {
  events?: LogEvent[];
}

export interface LogEvent {
  seq?: number;
  time?: string;
  level?: string;
  message?: string;
  attrs?: Record<string, unknown>;
}

export interface OAuthStatusResponse {
  discovery_urls?: string[];
  metadata?: OAuthMetadata;
  error?: string;
  pending?: boolean;
  www_authenticate_probe?: OAuthProbe;
  metadata_source?: string;
  auth_server_metadata_mode?: string;
  authorization_server_count?: number;
  selected_authorization_server?: string;
}

export interface OAuthProbe {
  url?: string;
  error?: string;
}

export interface OAuthMetadata {
  fetched_at?: string;
  headers?: Record<string, string>;
  body?: unknown;
  body_text?: string;
  attempts?: OAuthAttempt[];
  auth_server_metadata?: OAuthAuthServerMetadata;
}

export interface OAuthAttempt {
  source?: string;
  url?: string;
  tried?: boolean;
  selected?: boolean;
  error?: string;
  status_code?: number;
  headers?: Record<string, string>;
  body?: unknown;
  body_text?: string;
}

export interface OAuthAuthServerMetadata {
  attempts?: OAuthAuthAttempt[];
}

export interface OAuthAuthAttempt {
  url?: string;
  tried?: boolean;
  selected?: boolean;
  error?: string;
  status_code?: number;
  headers?: Record<string, string>;
  body?: unknown;
  body_text?: string;
  document?: string;
  path_style?: string;
}

export interface OAuthRowDetails {
  statusCode?: number;
  status?: string;
  fetchedAt?: string;
  source?: string;
  sourceURL?: string;
  headers?: Record<string, string> | null;
  body?: unknown;
  bodyText?: string;
  error?: string;
}

export interface OAuthRow {
  key: string;
  priority: number;
  step: string;
  url: string;
  status: string;
  details: OAuthRowDetails;
}

export interface HarpoonStatusResponse {
  enabled?: boolean;
  reason?: string;
  capture_payloads?: boolean;
  allow_plaintext_http?: boolean;
  max_response_bytes?: number;
  max_redirects?: number;
}

export interface HarpoonTargetsResponse {
  targets?: HarpoonTarget[];
}

export interface HarpoonTarget {
  label?: string;
  url?: string;
  description?: string;
  source?: string;
  inclusion_reason?: string;
}

export interface HarpoonCallsResponse {
  calls?: HarpoonCall[];
}

export interface HarpoonCall {
  timestamp?: string;
  label?: string;
  url?: string;
  method?: string;
  status?: number;
  latency_ms?: number;
  req_bytes?: number;
  resp_bytes?: number;
  error?: string;
  request_body?: string;
  response_body?: string;
  response_body_transformed?: string;
  body_is_base64?: boolean;
}

export interface MetricSample {
  labels: string;
  value: number;
}

export type MetricMap = Map<string, MetricSample[]>;
