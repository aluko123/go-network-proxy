# Prometheus & Grafana Monitoring Guide

## Quick Start

### 1. Start Prometheus & Grafana
```bash
docker-compose up -d
```

This starts:
- **Prometheus** on http://localhost:9090
- **Grafana** on http://localhost:3000

### 2. Start Your Proxy
```bash
cd http-proxy
./proxy-server -proto http
```

### 3. Generate Some Traffic
```bash
# Make requests through your proxy
curl -x http://localhost:8080 http://httpbin.org/ip
curl -x http://localhost:8080 https://www.google.com
curl -x http://localhost:8080 http://example.com  # Blocked
```

---

## Accessing Prometheus

**URL:** http://localhost:9090

### View Metrics
1. Go to **Graph** tab
2. Enter queries like:
   - `proxy_requests_total` - Total requests
   - `proxy_blocked_requests_total` - Blocked count
   - `proxy_active_connections` - Active connections
   - `rate(proxy_requests_total[1m])` - Requests per second

### Check Targets
1. Go to **Status → Targets**
2. Verify your proxy (`localhost:8080`) is **UP** and green

---

## Accessing Grafana

**URL:** http://localhost:3000  
**Login:** `admin` / `admin` (change password on first login)

### Auto-Configured Setup
✅ Prometheus datasource already added  
✅ Dashboard auto-loaded (if provisioning worked)

### Manual Dashboard Setup (if needed)

#### 1. Add Prometheus Data Source
- **Left menu** → **Configuration** (⚙️) → **Data Sources**
- Click **Add data source**
- Select **Prometheus**
- URL: `http://prometheus:9090`
- Click **Save & Test**

#### 2. Create Dashboard
- **Left menu** → **Dashboards** → **New Dashboard**
- Click **Add new panel**

#### 3. Add Panels

**Panel 1: Request Rate (Time Series)**
- **Query:** `rate(proxy_requests_total[5m])`
- **Legend:** `{{method}} - {{status}}`
- **Panel type:** Time series
- **Title:** Request Rate

**Panel 2: Blocked Requests (Stat)**
- **Query:** `proxy_blocked_requests_total`
- **Panel type:** Stat
- **Title:** Total Blocked Requests
- **Color scheme:** Red

**Panel 3: Active Connections (Gauge)**
- **Query:** `proxy_active_connections`
- **Panel type:** Gauge
- **Title:** Active Connections
- **Min:** 0, **Max:** 100

**Panel 4: Request Duration p95 (Graph)**
- **Query:** `histogram_quantile(0.95, rate(proxy_request_duration_seconds_bucket[5m]))`
- **Legend:** `{{method}} p95`
- **Panel type:** Time series
- **Title:** Request Duration (95th Percentile)
- **Unit:** seconds (s)

**Panel 5: Requests by Method (Pie Chart)**
- **Query:** `sum by (method) (proxy_requests_total)`
- **Panel type:** Pie chart
- **Title:** Requests by Method

**Panel 6: Average Duration (Stat)**
- **Query:** `rate(proxy_request_duration_seconds_sum[5m]) / rate(proxy_request_duration_seconds_count[5m])`
- **Panel type:** Stat
- **Title:** Average Request Duration
- **Unit:** seconds (s)

---

## Useful Prometheus Queries

```promql
# Request rate over last 5 minutes
rate(proxy_requests_total[5m])

# Total requests grouped by method
sum by (method) (proxy_requests_total)

# Blocked request percentage
proxy_blocked_requests_total / sum(proxy_requests_total) * 100

# 99th percentile latency
histogram_quantile(0.99, rate(proxy_request_duration_seconds_bucket[5m]))

# Requests per second
rate(proxy_requests_total[1m])
```

---

## Grafana Dashboard Tips

### Visualization Types
- **Time series:** Trends over time (request rate, duration)
- **Stat:** Single number (total blocked, active connections)
- **Gauge:** Visual meter (connection count)
- **Pie/Donut:** Distribution (methods, status codes)
- **Heatmap:** Latency distribution
- **Table:** Detailed metrics breakdown

### Best Practices
1. **Use variables** - Create dropdown for time ranges
2. **Set refresh rate** - Auto-refresh every 5-10 seconds
3. **Add annotations** - Mark deployment times
4. **Use colors** - Red for errors, green for success
5. **Add thresholds** - Alert when metrics are abnormal

### Example Dashboard Layout
```
Row 1: [Request Rate Graph] [Blocked Count] [Active Connections]
Row 2: [Duration p95 Graph] [Requests by Method Pie Chart]
Row 3: [Recent Requests Table] [Error Rate Graph]
```

---

## Managing Services

### Start Services
```bash
docker-compose up -d
```

### Stop Services
```bash
docker-compose down
```

### View Logs
```bash
# Prometheus logs
docker logs proxy-prometheus

# Grafana logs
docker logs proxy-grafana
```

### Restart Services
```bash
docker-compose restart
```

### Remove Everything (including data)
```bash
docker-compose down -v
```

---

## Troubleshooting

### Prometheus can't scrape proxy
- **Fix prometheus.yml:** Change `localhost:8080` to `host.docker.internal:8080` (on Windows/Mac)
- Or use `network_mode: "host"` in docker-compose.yml

### Grafana can't connect to Prometheus
- Ensure both are on same Docker network
- Use `http://prometheus:9090` (container name, not localhost)

### No data showing
- Check proxy is running: `curl http://localhost:8080/metrics`
- Check Prometheus targets: http://localhost:9090/targets
- Generate traffic through proxy

---

## Next Steps

1. ✅ Start services: `docker-compose up -d`
2. ✅ Start proxy: `./http-proxy/proxy-server -proto http`
3. ✅ Open Grafana: http://localhost:3000
4. ✅ Generate traffic through proxy
5. ✅ Watch metrics update in real-time!
