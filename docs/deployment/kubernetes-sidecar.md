# Kubernetes sidecar deployment

Run `tunnel-client` as a sidecar container in the same Pod as your MCP server. This is a good fit when the MCP server is reachable via `localhost`.

## Example (snippet)

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: mcp-with-tunnel
spec:
  containers:
    - name: mcp-server
      image: your-mcp-image:latest
      ports:
        - containerPort: 3000
    - name: tunnel-client
      image: tunnel-client:latest
      env:
        - name: CONTROL_PLANE_TUNNEL_ID
          value: tunnel_<abc>
        - name: CONTROL_PLANE_BASE_URL
          value: https://api.openai.com
        - name: MCP_SERVER_URL
          value: http://127.0.0.1:3000/mcp
        - name: LOG_LEVEL
          value: info
        - name: LOG_FORMAT
          value: json
        - name: CONTROL_PLANE_API_KEY
          valueFrom:
            secretKeyRef:
              name: openai-api-key
              key: api_key
      ports:
        - name: health
          containerPort: 8080
      livenessProbe:
        httpGet:
          path: /healthz
          port: health
        initialDelaySeconds: 5
        periodSeconds: 10
      readinessProbe:
        httpGet:
          path: /readyz
          port: health
        initialDelaySeconds: 5
        periodSeconds: 10
```

## Considerations

- Lock down egress with NetworkPolicy: allow `api.openai.com:443` plus access to the MCP port.
