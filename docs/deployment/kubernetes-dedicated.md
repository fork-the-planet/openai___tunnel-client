# Kubernetes dedicated Deployment/Pod

Run `tunnel-client` as its own Deployment when your MCP server is already reachable via a Service and you want independent upgrades/restarts.

## Example Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tunnel-client
spec:
  replicas: 1
  selector:
    matchLabels: { app: tunnel-client }
  template:
    metadata:
      labels: { app: tunnel-client }
    spec:
      containers:
        - name: tunnel-client
          image: tunnel-client:latest
          env:
            - name: CONTROL_PLANE_TUNNEL_ID
              value: tunnel_0123456789abcdef0123456789abcdef
            - name: MCP_SERVER_URL
              value: http://mcp-server.default.svc.cluster.local:3000/mcp
            - name: CONTROL_PLANE_API_KEY
              valueFrom:
                secretKeyRef:
                  name: openai-api-key
                  key: api_key
          ports:
            - name: health
              containerPort: 8080
          livenessProbe:
            httpGet: { path: /healthz, port: health }
            initialDelaySeconds: 5
          readinessProbe:
            httpGet: { path: /readyz, port: health }
            initialDelaySeconds: 5
```
