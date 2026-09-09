package main

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	defaultServiceInfoTTL = time.Hour
	defaultLiveInfoTTL    = 60 * time.Second
	defaultRawStatsTTL    = 300 * time.Second
)

var (
	hostIPLabels   = []string{"hostname", "ip_address"}
	hostVeidLabels = []string{"hostname", "veid"}
	veidLabels     = []string{"veid"}

	descDataCounter     = prometheus.NewDesc("bandwagon_data_counter", "Bandwidth used in the current billing cycle, in bytes.", hostIPLabels, nil)
	descPlanMonthlyData = prometheus.NewDesc("bandwagon_plan_monthly_data", "Monthly bandwidth quota, in bytes.", hostIPLabels, nil)
	descDataNextReset   = prometheus.NewDesc("bandwagon_data_next_reset", "Unix timestamp of the next bandwidth counter reset.", hostIPLabels, nil)
	descNodeInfo        = prometheus.NewDesc("bandwagon_node_info", "KiwiVM node information.", []string{"hostname", "ip_address", "location", "os", "plan", "ram", "disk", "swap", "vm_type", "suspended"}, nil)
	descRateLimit15Min  = prometheus.NewDesc("bandwagon_api_rate_limit_remaining_points_15min", "KiwiVM API rate limit points remaining in the 15 minute window.", veidLabels, nil)
	descRateLimit24h    = prometheus.NewDesc("bandwagon_api_rate_limit_remaining_points_24h", "KiwiVM API rate limit points remaining in the 24 hour window.", veidLabels, nil)
	descAPIRequests     = prometheus.NewDesc("bandwagon_api_request_total", "Total KiwiVM API requests made by this exporter.", veidLabels, nil)

	descUp            = prometheus.NewDesc("kiwivm_up", "Whether the VPS is running (ve_status == running).", hostVeidLabels, nil)
	descCPUUsage      = prometheus.NewDesc("kiwivm_cpu_usage_percent", "CPU usage percent from the latest raw usage sample.", hostVeidLabels, nil)
	descMemTotal      = prometheus.NewDesc("kiwivm_mem_total_bytes", "Total memory (plan_ram), in bytes.", hostVeidLabels, nil)
	descMemAvailable  = prometheus.NewDesc("kiwivm_mem_available_bytes", "Available memory, in bytes.", hostVeidLabels, nil)
	descSwapTotal     = prometheus.NewDesc("kiwivm_swap_total_bytes", "Total swap, in bytes.", hostVeidLabels, nil)
	descSwapAvailable = prometheus.NewDesc("kiwivm_swap_available_bytes", "Available swap, in bytes.", hostVeidLabels, nil)
	descLoad1         = prometheus.NewDesc("kiwivm_load1", "1 minute load average.", hostVeidLabels, nil)
	descLoad5         = prometheus.NewDesc("kiwivm_load5", "5 minute load average.", hostVeidLabels, nil)
	descLoad15        = prometheus.NewDesc("kiwivm_load15", "15 minute load average.", hostVeidLabels, nil)
	descDiskUsed      = prometheus.NewDesc("kiwivm_disk_used_bytes", "Used disk space, in bytes.", hostVeidLabels, nil)
	descDiskQuota     = prometheus.NewDesc("kiwivm_disk_quota_bytes", "Disk quota, in bytes.", hostVeidLabels, nil)
	descCPUThrottled  = prometheus.NewDesc("kiwivm_cpu_throttled", "Whether the VPS is currently CPU throttled.", hostVeidLabels, nil)
	descDiskThrottled = prometheus.NewDesc("kiwivm_disk_throttled", "Whether the VPS is currently disk throttled.", hostVeidLabels, nil)
	descAPIUp         = prometheus.NewDesc("kiwivm_api_up", "Whether the last KiwiVM API refresh for this node succeeded.", veidLabels, nil)
)

type nodeState struct {
	client *Client

	mu           sync.Mutex
	info         *ServiceInfo
	infoFetched  time.Time
	live         *LiveInfo
	liveFetched  time.Time
	stats        *RawUsageStats
	statsFetched time.Time
	rateLimit    *RateLimit
	apiUp        float64
	requests     int64
}

type Collector struct {
	nodes      []*nodeState
	now        func() time.Time
	serviceTTL time.Duration
	liveTTL    time.Duration
	statsTTL   time.Duration
}

func NewCollector(cfg *Config) *Collector {
	c := &Collector{
		now:        time.Now,
		serviceTTL: defaultServiceInfoTTL,
		liveTTL:    defaultLiveInfoTTL,
		statsTTL:   defaultRawStatsTTL,
	}
	for _, n := range cfg.Nodes {
		c.nodes = append(c.nodes, &nodeState{client: NewClient(cfg.Endpoint, n.Veid, n.APIKey)})
	}
	return c
}

func (c *Collector) Describe(chan<- *prometheus.Desc) {}

func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	var wg sync.WaitGroup
	for _, n := range c.nodes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.refreshNode(n)
		}()
	}
	wg.Wait()
	for _, n := range c.nodes {
		c.collectNode(ch, n)
	}
}

func (c *Collector) refreshNode(n *nodeState) {
	n.mu.Lock()
	defer n.mu.Unlock()

	now := c.now()
	ok := true

	if n.info == nil || now.Sub(n.infoFetched) >= c.serviceTTL {
		if info, err := n.client.GetServiceInfo(); err == nil {
			n.info, n.infoFetched = info, now
		} else {
			ok = false
		}
		n.requests++
	}

	if n.live == nil || now.Sub(n.liveFetched) >= c.liveTTL {
		if live, err := n.client.GetLiveServiceInfo(); err == nil {
			n.live, n.liveFetched = live, now
		} else {
			ok = false
		}
		n.requests++
		if rl, err := n.client.GetRateLimitStatus(); err == nil {
			n.rateLimit = rl
		} else {
			ok = false
		}
		n.requests++
	}

	if n.stats == nil || now.Sub(n.statsFetched) >= c.statsTTL {
		if stats, err := n.client.GetRawUsageStats(); err == nil {
			n.stats, n.statsFetched = stats, now
		} else {
			ok = false
		}
		n.requests++
	}

	if ok {
		n.apiUp = 1
	} else {
		n.apiUp = 0
	}
}

func (c *Collector) collectNode(ch chan<- prometheus.Metric, n *nodeState) {
	n.mu.Lock()
	defer n.mu.Unlock()

	veid := strconv.FormatInt(n.client.veid, 10)
	ch <- prometheus.MustNewConstMetric(descAPIUp, prometheus.GaugeValue, n.apiUp, veid)
	ch <- prometheus.MustNewConstMetric(descAPIRequests, prometheus.CounterValue, float64(n.requests), veid)

	if n.rateLimit != nil {
		ch <- prometheus.MustNewConstMetric(descRateLimit15Min, prometheus.GaugeValue, float64(n.rateLimit.RemainingPoints15Min), veid)
		ch <- prometheus.MustNewConstMetric(descRateLimit24h, prometheus.GaugeValue, float64(n.rateLimit.RemainingPoints24h), veid)
	}

	hostname := ""
	if n.info != nil {
		hostname = n.info.Hostname
	} else if n.live != nil {
		hostname = n.live.Hostname
	}

	if n.info != nil {
		ip := ""
		if len(n.info.IPAddresses) > 0 {
			ip = n.info.IPAddresses[0]
		}
		ch <- prometheus.MustNewConstMetric(descDataCounter, prometheus.GaugeValue, float64(n.info.DataCounter), hostname, ip)
		ch <- prometheus.MustNewConstMetric(descPlanMonthlyData, prometheus.GaugeValue, float64(n.info.PlanMonthlyData), hostname, ip)
		ch <- prometheus.MustNewConstMetric(descDataNextReset, prometheus.GaugeValue, float64(n.info.DataNextReset), hostname, ip)
		ch <- prometheus.MustNewConstMetric(descNodeInfo, prometheus.GaugeValue, 1,
			hostname, ip, n.info.NodeLocation, n.info.OS, n.info.Plan,
			strconv.FormatInt(n.info.PlanRAM, 10), strconv.FormatInt(n.info.PlanDisk, 10),
			strconv.FormatInt(n.info.PlanSwap, 10), n.info.VMType, strconv.FormatBool(n.info.Suspended))
		ch <- prometheus.MustNewConstMetric(descMemTotal, prometheus.GaugeValue, float64(n.info.PlanRAM), hostname, veid)
	}

	if n.live != nil {
		up := 0.0
		if n.live.VeStatus == "running" {
			up = 1
		}
		ch <- prometheus.MustNewConstMetric(descUp, prometheus.GaugeValue, up, hostname, veid)
		ch <- prometheus.MustNewConstMetric(descMemAvailable, prometheus.GaugeValue, float64(n.live.MemAvailableKB)*1024, hostname, veid)
		ch <- prometheus.MustNewConstMetric(descSwapTotal, prometheus.GaugeValue, float64(n.live.SwapTotalKB)*1024, hostname, veid)
		ch <- prometheus.MustNewConstMetric(descSwapAvailable, prometheus.GaugeValue, float64(n.live.SwapAvailableKB)*1024, hostname, veid)
		if l1, l5, l15, ok := parseLoadAverage(n.live.LoadAverage); ok {
			ch <- prometheus.MustNewConstMetric(descLoad1, prometheus.GaugeValue, l1, hostname, veid)
			ch <- prometheus.MustNewConstMetric(descLoad5, prometheus.GaugeValue, l5, hostname, veid)
			ch <- prometheus.MustNewConstMetric(descLoad15, prometheus.GaugeValue, l15, hostname, veid)
		}
		ch <- prometheus.MustNewConstMetric(descDiskUsed, prometheus.GaugeValue, float64(n.live.UsedDiskSpaceB), hostname, veid)
		if gb, err := strconv.ParseFloat(strings.TrimSpace(n.live.DiskQuotaGB), 64); err == nil {
			ch <- prometheus.MustNewConstMetric(descDiskQuota, prometheus.GaugeValue, gb*1024*1024*1024, hostname, veid)
		}
		ch <- prometheus.MustNewConstMetric(descCPUThrottled, prometheus.GaugeValue, throttledValue(n.live.IsCPUThrottled), hostname, veid)
		ch <- prometheus.MustNewConstMetric(descDiskThrottled, prometheus.GaugeValue, throttledValue(n.live.IsDiskThrottled), hostname, veid)
	}

	if n.stats != nil {
		if s := n.stats.Latest(); s != nil {
			ch <- prometheus.MustNewConstMetric(descCPUUsage, prometheus.GaugeValue, s.CPUUsage, hostname, veid)
		}
	}
}

func throttledValue(s string) float64 {
	if strings.TrimSpace(s) == "" {
		return 0
	}
	return 1
}
