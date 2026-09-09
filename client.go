package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	endpoint   string
	veid       int64
	apiKey     string
	httpClient *http.Client
}

func NewClient(endpoint string, veid int64, apiKey string) *Client {
	return &Client{
		endpoint:   strings.TrimSuffix(endpoint, "/"),
		veid:       veid,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) get(action string, out interface{ apiError() int }) error {
	u := fmt.Sprintf("%s/%s?veid=%d&api_key=%s", c.endpoint, action, c.veid, url.QueryEscape(c.apiKey))
	resp, err := c.httpClient.Get(u)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: http %d", action, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%s: decode: %w", action, err)
	}
	if e := out.apiError(); e != 0 {
		return fmt.Errorf("%s: api error %d", action, e)
	}
	return nil
}

type ServiceInfo struct {
	Error           int      `json:"error"`
	Hostname        string   `json:"hostname"`
	IPAddresses     []string `json:"ip_addresses"`
	PlanMonthlyData int64    `json:"plan_monthly_data"`
	DataCounter     int64    `json:"data_counter"`
	DataNextReset   int64    `json:"data_next_reset"`
	PlanRAM         int64    `json:"plan_ram"`
	PlanDisk        int64    `json:"plan_disk"`
	PlanSwap        int64    `json:"plan_swap"`
	OS              string   `json:"os"`
	Plan            string   `json:"plan"`
	NodeLocation    string   `json:"node_location"`
	VMType          string   `json:"vm_type"`
	Suspended       bool     `json:"suspended"`
}

func (s *ServiceInfo) apiError() int { return s.Error }

type LiveInfo struct {
	Error           int    `json:"error"`
	VeStatus        string `json:"ve_status"`
	Hostname        string `json:"hostname"`
	MemAvailableKB  int64  `json:"mem_available_kb"`
	SwapTotalKB     int64  `json:"swap_total_kb"`
	SwapAvailableKB int64  `json:"swap_available_kb"`
	LoadAverage     string `json:"load_average"`
	UsedDiskSpaceB  int64  `json:"ve_used_disk_space_b"`
	DiskQuotaGB     string `json:"ve_disk_quota_gb"`
	IsCPUThrottled  string `json:"is_cpu_throttled"`
	IsDiskThrottled string `json:"is_disk_throttled"`
}

func (l *LiveInfo) apiError() int { return l.Error }

type UsageSample struct {
	Timestamp       int64   `json:"timestamp"`
	CPUUsage        float64 `json:"cpu_usage"`
	NetworkInBytes  int64   `json:"network_in_bytes"`
	NetworkOutBytes int64   `json:"network_out_bytes"`
	DiskReadBytes   int64   `json:"disk_read_bytes"`
	DiskWriteBytes  int64   `json:"disk_write_bytes"`
}

type RawUsageStats struct {
	Error int           `json:"error"`
	Data  []UsageSample `json:"data"`
}

func (s *RawUsageStats) apiError() int { return s.Error }

func (s *RawUsageStats) Latest() *UsageSample {
	var latest *UsageSample
	for i := range s.Data {
		if latest == nil || s.Data[i].Timestamp > latest.Timestamp {
			latest = &s.Data[i]
		}
	}
	return latest
}

type RateLimit struct {
	Error                int   `json:"error"`
	RemainingPoints15Min int64 `json:"remaining_points_15min"`
	RemainingPoints24h   int64 `json:"remaining_points_24h"`
}

func (r *RateLimit) apiError() int { return r.Error }

func (c *Client) GetServiceInfo() (*ServiceInfo, error) {
	var r ServiceInfo
	if err := c.get("getServiceInfo", &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (c *Client) GetLiveServiceInfo() (*LiveInfo, error) {
	var r LiveInfo
	if err := c.get("getLiveServiceInfo", &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (c *Client) GetRawUsageStats() (*RawUsageStats, error) {
	var r RawUsageStats
	if err := c.get("getRawUsageStats", &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (c *Client) GetRateLimitStatus() (*RateLimit, error) {
	var r RateLimit
	if err := c.get("getRateLimitStatus", &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func parseLoadAverage(s string) (l1, l5, l15 float64, ok bool) {
	fields := strings.Fields(s)
	if len(fields) < 3 {
		return 0, 0, 0, false
	}
	var err error
	if l1, err = strconv.ParseFloat(fields[0], 64); err != nil {
		return 0, 0, 0, false
	}
	if l5, err = strconv.ParseFloat(fields[1], 64); err != nil {
		return 0, 0, 0, false
	}
	if l15, err = strconv.ParseFloat(fields[2], 64); err != nil {
		return 0, 0, 0, false
	}
	return l1, l5, l15, true
}
