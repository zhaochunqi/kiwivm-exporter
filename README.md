# kiwivm-exporter

搬瓦工 KiwiVM API 的 Prometheus exporter。兼容 `ghcr.io/icyleaf/bandwagon-exporter` 的 `bandwagon_*` 指标(同名同标签,Grafana 现有面板零改动),并额外暴露 CPU / 内存 / SWAP / 负载 / 磁盘 / 运行状态等 `kiwivm_*` 指标。

## 指标清单

### 兼容旧 exporter(`bandwagon_*`)

| 指标 | 类型 | 标签 | 说明 |
| --- | --- | --- | --- |
| `bandwagon_data_counter` | gauge | `hostname`, `ip_address` | 本计费周期已用流量(字节) |
| `bandwagon_plan_monthly_data` | gauge | `hostname`, `ip_address` | 每月流量配额(字节) |
| `bandwagon_data_next_reset` | gauge | `hostname`, `ip_address` | 流量计数下次重置的 unix 时间戳 |
| `bandwagon_node_info` | gauge | `hostname`, `ip_address`, `location`, `os`, `plan`, `ram`, `disk`, `swap`, `vm_type`, `suspended` | 节点信息,恒为 1 |
| `bandwagon_api_rate_limit_remaining_points_15min` | gauge | `veid` | API 15 分钟窗口剩余点数 |
| `bandwagon_api_rate_limit_remaining_points_24h` | gauge | `veid` | API 24 小时窗口剩余点数 |
| `bandwagon_api_request_total` | counter | `veid` | 本 exporter 进程内累计 API 调用次数 |

### 新增(`kiwivm_*`)

| 指标 | 类型 | 标签 | 说明 |
| --- | --- | --- | --- |
| `kiwivm_up` | gauge | `hostname`, `veid` | VPS 运行状态(`ve_status == "running"`) |
| `kiwivm_cpu_usage_percent` | gauge | `hostname`, `veid` | CPU 使用率(最新 5 分钟采样点) |
| `kiwivm_mem_total_bytes` | gauge | `hostname`, `veid` | 内存总量(`plan_ram`) |
| `kiwivm_mem_available_bytes` | gauge | `hostname`, `veid` | 可用内存 |
| `kiwivm_swap_total_bytes` | gauge | `hostname`, `veid` | SWAP 总量 |
| `kiwivm_swap_available_bytes` | gauge | `hostname`, `veid` | 可用 SWAP |
| `kiwivm_load1` / `kiwivm_load5` / `kiwivm_load15` | gauge | `hostname`, `veid` | 负载均值 |
| `kiwivm_disk_used_bytes` | gauge | `hostname`, `veid` | 已用磁盘 |
| `kiwivm_disk_quota_bytes` | gauge | `hostname`, `veid` | 磁盘配额 |
| `kiwivm_cpu_throttled` / `kiwivm_disk_throttled` | gauge | `hostname`, `veid` | 是否被限速(0/1) |
| `kiwivm_network_in_bytes` / `kiwivm_network_out_bytes` | gauge | `hostname`, `veid` | 最新 5 分钟采样间隔内网络收/发字节数(除以 300 得 Bps) |
| `kiwivm_disk_read_bytes` / `kiwivm_disk_write_bytes` | gauge | `hostname`, `veid` | 最新 5 分钟采样间隔内磁盘读/写字节数(除以 300 得 Bps) |
| `kiwivm_api_up` | gauge | `veid` | exporter 调 KiwiVM API 是否成功 |

## 配置

参考 `config.example.yml`:

```yaml
endpoint: https://api.64clouds.com/v1
nodes:
  - veid: 1807892
    api_key: private_xxx
  - veid: 1063765
    api_key: private_yyy
```

`veid` 与 `api_key` 在 KiwiVM 控制面板的 API 页面获取。真实配置不要提交进仓库(`.gitignore` 已忽略 `config.yml`)。

抓取节奏:`getServiceInfo` 缓存 1 小时,`getLiveServiceInfo` 与 `getRateLimitStatus` 缓存 60 秒,`getRawUsageStats` 缓存 300 秒;每次 `/metrics` 被刮时按 TTL 决定是否真实调 API。API 失败时保留上次成功值继续暴露,并将 `kiwivm_api_up` 置 0。

## 本地运行

```bash
go build ./...
./kiwivm-exporter --config-path config.yml --listen :9103
```

- `--config-path`:配置文件路径,默认 `/etc/kiwivm/config.yml`
- `--listen`:监听地址,默认 `:9103`

端点:

- `http://localhost:9103/metrics` — Prometheus 指标
- `http://localhost:9103/healthz` — 健康检查(200 `ok`)

## 测试

```bash
go test ./...
```

## Docker 运行

```bash
docker build -t kiwivm-exporter .
docker run -d --name kiwivm-exporter \
  -p 9103:9103 \
  -v /path/to/config.yml:/etc/kiwivm/config.yml:ro \
  --restart unless-stopped \
  ghcr.io/zhaochunqi/kiwivm-exporter:latest
```

镜像基于 `gcr.io/distroless/static-debian12:nonroot`,非 root 运行,仅支持 linux/amd64(见 `.github/workflows/docker.yml`)。

## Grafana Dashboard

[`grafana/kiwivm-overview.json`](./grafana/kiwivm-overview.json) 提供一份可导入的总览面板:运行状态、CPU、内存、SWAP、负载、磁盘、流量配额与 API 健康一页看全,支持按 `hostname` 筛选多节点。

导入方式:Grafana → Dashboards → New → Import → 粘贴 JSON,选择你的 Prometheus 数据源。

## License

MIT
