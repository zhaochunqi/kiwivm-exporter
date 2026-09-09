package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

const serviceInfoJSON = `{
	"hostname": "vps-test-%[1]s",
	"ip_addresses": ["10.0.0.%[1]s"],
	"plan_monthly_data": 2147483648000,
	"data_counter": 406226156746,
	"data_next_reset": 1790478935,
	"plan_ram": 2147483648,
	"plan_disk": 42949672960,
	"plan_swap": 0,
	"os": "ubuntu-26.04-x86_64",
	"plan": "kvmv5-the-plan-v2",
	"node_location": "US, California",
	"vm_type": "kvm",
	"suspended": false,
	"error": 0
}`

const liveInfoJSON = `{
	"ve_status": "running",
	"hostname": "vps-test-1807892",
	"mem_available_kb": 1160716,
	"swap_total_kb": 1048572,
	"swap_available_kb": 447464,
	"load_average": "0.29 0.20 0.18 1/775 3079116",
	"ve_used_disk_space_b": 13352460288,
	"ve_disk_quota_gb": "40",
	"is_cpu_throttled": "",
	"is_disk_throttled": "",
	"screendump_png_base64": "ignored",
	"error": 0
}`

const rawUsageStatsJSON = `{
	"data": [
		{"timestamp": 1788993001, "cpu_usage": 5, "network_in_bytes": 1, "network_out_bytes": 2, "disk_read_bytes": 3, "disk_write_bytes": 4},
		{"timestamp": 1788993301, "cpu_usage": 12, "network_in_bytes": 86073344, "network_out_bytes": 81572864, "disk_read_bytes": 24576, "disk_write_bytes": 5709824}
	],
	"error": 0
}`

const rateLimitJSON = `{
	"remaining_points_15min": 990,
	"remaining_points_24h": 19893,
	"error": 0
}`

type mockAPI struct {
	srv     *httptest.Server
	mu      sync.Mutex
	calls   map[string]int
	failing bool
}

func newMockAPI(t *testing.T) *mockAPI {
	t.Helper()
	m := &mockAPI{calls: map[string]int{}}
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		m.calls[r.URL.Path]++
		failing := m.failing
		m.mu.Unlock()
		if failing {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		veid := r.URL.Query().Get("veid")
		switch r.URL.Path {
		case "/getServiceInfo":
			fmt.Fprintf(w, serviceInfoJSON, veid)
		case "/getLiveServiceInfo":
			fmt.Fprint(w, liveInfoJSON)
		case "/getRawUsageStats":
			fmt.Fprint(w, rawUsageStatsJSON)
		case "/getRateLimitStatus":
			fmt.Fprint(w, rateLimitJSON)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(m.srv.Close)
	return m
}

func (m *mockAPI) callCount(action string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls["/"+action]
}

func (m *mockAPI) setFailing(f bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failing = f
}

func newTestCollector(m *mockAPI, veids ...int64) *Collector {
	cfg := &Config{Endpoint: m.srv.URL}
	for _, v := range veids {
		cfg.Nodes = append(cfg.Nodes, NodeConfig{Veid: v, APIKey: "private_test"})
	}
	return NewCollector(cfg)
}

func gather(t *testing.T, c *Collector) map[string]*dto.MetricFamily {
	t.Helper()
	reg := prometheus.NewPedanticRegistry()
	reg.MustRegister(c)
	fams, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	out := map[string]*dto.MetricFamily{}
	for _, f := range fams {
		out[f.GetName()] = f
	}
	return out
}

func findMetric(fams map[string]*dto.MetricFamily, name string, labels map[string]string) *dto.Metric {
	f, ok := fams[name]
	if !ok {
		return nil
	}
outer:
	for _, m := range f.GetMetric() {
		got := map[string]string{}
		for _, l := range m.GetLabel() {
			got[l.GetName()] = l.GetValue()
		}
		for k, v := range labels {
			if got[k] != v {
				continue outer
			}
		}
		return m
	}
	return nil
}

func metricValue(m *dto.Metric) float64 {
	if g := m.GetGauge(); g != nil {
		return g.GetValue()
	}
	if c := m.GetCounter(); c != nil {
		return c.GetValue()
	}
	return -1
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	content := "endpoint: http://example.com/v1\nnodes:\n  - veid: 1807892\n    api_key: private_xxx\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Endpoint != "http://example.com/v1" {
		t.Errorf("endpoint = %q", cfg.Endpoint)
	}
	if len(cfg.Nodes) != 1 || cfg.Nodes[0].Veid != 1807892 || cfg.Nodes[0].APIKey != "private_xxx" {
		t.Errorf("nodes = %+v", cfg.Nodes)
	}

	bad := filepath.Join(dir, "bad.yml")
	if err := os.WriteFile(bad, []byte("nodes: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(bad); err == nil {
		t.Error("expected error for empty nodes")
	}
	if _, err := LoadConfig(filepath.Join(dir, "missing.yml")); err == nil {
		t.Error("expected error for missing file")
	}
}

func TestClientParsing(t *testing.T) {
	m := newMockAPI(t)
	c := NewClient(m.srv.URL, 1807892, "private_test")

	info, err := c.GetServiceInfo()
	if err != nil {
		t.Fatalf("GetServiceInfo: %v", err)
	}
	if info.Hostname != "vps-test-1807892" || info.IPAddresses[0] != "10.0.0.1807892" ||
		info.DataCounter != 406226156746 || info.PlanMonthlyData != 2147483648000 ||
		info.PlanRAM != 2147483648 || info.DataNextReset != 1790478935 ||
		info.NodeLocation != "US, California" || info.VMType != "kvm" || info.Suspended {
		t.Errorf("unexpected ServiceInfo: %+v", info)
	}

	live, err := c.GetLiveServiceInfo()
	if err != nil {
		t.Fatalf("GetLiveServiceInfo: %v", err)
	}
	if live.VeStatus != "running" || live.MemAvailableKB != 1160716 ||
		live.SwapTotalKB != 1048572 || live.SwapAvailableKB != 447464 ||
		live.UsedDiskSpaceB != 13352460288 || live.DiskQuotaGB != "40" {
		t.Errorf("unexpected LiveInfo: %+v", live)
	}
	l1, l5, l15, ok := parseLoadAverage(live.LoadAverage)
	if !ok || l1 != 0.29 || l5 != 0.20 || l15 != 0.18 {
		t.Errorf("parseLoadAverage = %v %v %v %v", l1, l5, l15, ok)
	}

	stats, err := c.GetRawUsageStats()
	if err != nil {
		t.Fatalf("GetRawUsageStats: %v", err)
	}
	latest := stats.Latest()
	if latest == nil || latest.Timestamp != 1788993301 || latest.CPUUsage != 12 {
		t.Errorf("unexpected latest sample: %+v", latest)
	}

	rl, err := c.GetRateLimitStatus()
	if err != nil {
		t.Fatalf("GetRateLimitStatus: %v", err)
	}
	if rl.RemainingPoints15Min != 990 || rl.RemainingPoints24h != 19893 {
		t.Errorf("unexpected RateLimit: %+v", rl)
	}
}

func TestClientAPIError(t *testing.T) {
	m := newMockAPI(t)
	c := NewClient(m.srv.URL, 1807892, "private_test")
	m.setFailing(true)
	if _, err := c.GetServiceInfo(); err == nil {
		t.Error("expected error when server fails")
	}
}

func TestCollectorMetricsAndLabels(t *testing.T) {
	m := newMockAPI(t)
	c := newTestCollector(m, 1807892, 1063765)
	fams := gather(t, c)

	want := []struct {
		name   string
		labels map[string]string
		value  float64
	}{
		{"bandwagon_data_counter", map[string]string{"hostname": "vps-test-1807892", "ip_address": "10.0.0.1807892"}, 406226156746},
		{"bandwagon_plan_monthly_data", map[string]string{"hostname": "vps-test-1807892", "ip_address": "10.0.0.1807892"}, 2147483648000},
		{"bandwagon_data_next_reset", map[string]string{"hostname": "vps-test-1807892", "ip_address": "10.0.0.1807892"}, 1790478935},
		{"bandwagon_node_info", map[string]string{
			"hostname": "vps-test-1807892", "ip_address": "10.0.0.1807892", "location": "US, California",
			"os": "ubuntu-26.04-x86_64", "plan": "kvmv5-the-plan-v2",
			"ram": "2147483648", "disk": "42949672960", "swap": "0",
			"vm_type": "kvm", "suspended": "false",
		}, 1},
		{"kiwivm_up", map[string]string{"hostname": "vps-test-1807892", "veid": "1807892"}, 1},
		{"kiwivm_cpu_usage_percent", map[string]string{"hostname": "vps-test-1807892", "veid": "1807892"}, 12},
		{"kiwivm_mem_total_bytes", map[string]string{"hostname": "vps-test-1807892", "veid": "1807892"}, 2147483648},
		{"kiwivm_mem_available_bytes", map[string]string{"hostname": "vps-test-1807892", "veid": "1807892"}, 1160716 * 1024},
		{"kiwivm_swap_total_bytes", map[string]string{"hostname": "vps-test-1807892", "veid": "1807892"}, 1048572 * 1024},
		{"kiwivm_swap_available_bytes", map[string]string{"hostname": "vps-test-1807892", "veid": "1807892"}, 447464 * 1024},
		{"kiwivm_load1", map[string]string{"hostname": "vps-test-1807892", "veid": "1807892"}, 0.29},
		{"kiwivm_load5", map[string]string{"hostname": "vps-test-1807892", "veid": "1807892"}, 0.20},
		{"kiwivm_load15", map[string]string{"hostname": "vps-test-1807892", "veid": "1807892"}, 0.18},
		{"kiwivm_disk_used_bytes", map[string]string{"hostname": "vps-test-1807892", "veid": "1807892"}, 13352460288},
		{"kiwivm_disk_quota_bytes", map[string]string{"hostname": "vps-test-1807892", "veid": "1807892"}, 40.0 * 1024 * 1024 * 1024},
		{"kiwivm_cpu_throttled", map[string]string{"hostname": "vps-test-1807892", "veid": "1807892"}, 0},
		{"kiwivm_disk_throttled", map[string]string{"hostname": "vps-test-1807892", "veid": "1807892"}, 0},
		{"kiwivm_api_up", map[string]string{"veid": "1807892"}, 1},
		{"bandwagon_api_rate_limit_remaining_points_15min", map[string]string{"veid": "1807892"}, 990},
		{"bandwagon_api_rate_limit_remaining_points_24h", map[string]string{"veid": "1807892"}, 19893},
		{"bandwagon_api_request_total", map[string]string{"veid": "1807892"}, 4},
	}
	for _, w := range want {
		metric := findMetric(fams, w.name, w.labels)
		if metric == nil {
			t.Errorf("metric %s%v not found", w.name, w.labels)
			continue
		}
		if got := metricValue(metric); got != w.value {
			t.Errorf("%s%v = %v, want %v", w.name, w.labels, got, w.value)
		}
	}

	for _, veid := range []string{"1807892", "1063765"} {
		if findMetric(fams, "kiwivm_api_up", map[string]string{"veid": veid}) == nil {
			t.Errorf("kiwivm_api_up{veid=%s} missing", veid)
		}
	}
}

func TestCollectorTTLCache(t *testing.T) {
	m := newMockAPI(t)
	c := newTestCollector(m, 1807892)

	fakeNow := time.Now()
	c.now = func() time.Time { return fakeNow }

	gather(t, c)
	for _, action := range []string{"getServiceInfo", "getLiveServiceInfo", "getRawUsageStats", "getRateLimitStatus"} {
		if got := m.callCount(action); got != 1 {
			t.Fatalf("%s called %d times, want 1", action, got)
		}
	}

	gather(t, c)
	for _, action := range []string{"getServiceInfo", "getLiveServiceInfo", "getRawUsageStats", "getRateLimitStatus"} {
		if got := m.callCount(action); got != 1 {
			t.Errorf("%s called %d times within TTL, want 1", action, got)
		}
	}

	fakeNow = fakeNow.Add(61 * time.Second)
	gather(t, c)
	if got := m.callCount("getServiceInfo"); got != 1 {
		t.Errorf("getServiceInfo called %d times after 61s, want 1 (TTL 1h)", got)
	}
	if got := m.callCount("getLiveServiceInfo"); got != 2 {
		t.Errorf("getLiveServiceInfo called %d times after 61s, want 2 (TTL 60s)", got)
	}
	if got := m.callCount("getRateLimitStatus"); got != 2 {
		t.Errorf("getRateLimitStatus called %d times after 61s, want 2 (follows live TTL)", got)
	}
	if got := m.callCount("getRawUsageStats"); got != 1 {
		t.Errorf("getRawUsageStats called %d times after 61s, want 1 (TTL 300s)", got)
	}

	fakeNow = fakeNow.Add(4 * time.Minute)
	gather(t, c)
	if got := m.callCount("getRawUsageStats"); got != 2 {
		t.Errorf("getRawUsageStats called %d times after 301s, want 2", got)
	}

	fakeNow = fakeNow.Add(time.Hour)
	gather(t, c)
	if got := m.callCount("getServiceInfo"); got != 2 {
		t.Errorf("getServiceInfo called %d times after >1h, want 2", got)
	}
}

func TestCollectorFailureFallback(t *testing.T) {
	m := newMockAPI(t)
	c := newTestCollector(m, 1807892)

	fakeNow := time.Now()
	c.now = func() time.Time { return fakeNow }

	fams := gather(t, c)
	if got := metricValue(findMetric(fams, "kiwivm_api_up", map[string]string{"veid": "1807892"})); got != 1 {
		t.Fatalf("kiwivm_api_up = %v, want 1", got)
	}

	m.setFailing(true)
	fakeNow = fakeNow.Add(2 * time.Hour)
	fams = gather(t, c)

	if got := metricValue(findMetric(fams, "kiwivm_api_up", map[string]string{"veid": "1807892"})); got != 0 {
		t.Errorf("kiwivm_api_up = %v after failure, want 0", got)
	}
	stale := findMetric(fams, "bandwagon_data_counter", map[string]string{"hostname": "vps-test-1807892", "ip_address": "10.0.0.1807892"})
	if stale == nil || metricValue(stale) != 406226156746 {
		t.Errorf("bandwagon_data_counter not retained after failure: %v", stale)
	}
	if got := metricValue(findMetric(fams, "bandwagon_api_request_total", map[string]string{"veid": "1807892"})); got != 8 {
		t.Errorf("bandwagon_api_request_total = %v, want 8", got)
	}
}

func TestCollectorNeverSucceeds(t *testing.T) {
	m := newMockAPI(t)
	m.setFailing(true)
	c := newTestCollector(m, 1807892)
	fams := gather(t, c)

	if got := metricValue(findMetric(fams, "kiwivm_api_up", map[string]string{"veid": "1807892"})); got != 0 {
		t.Errorf("kiwivm_api_up = %v, want 0", got)
	}
	if _, ok := fams["bandwagon_data_counter"]; ok {
		t.Error("bandwagon_data_counter should be absent when the API never succeeded")
	}
}
