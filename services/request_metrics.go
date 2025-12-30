package services

import (
	"sync"
	"time"
)

type APIMetrics struct {
	QPS      float64 `json:"qps"`
	Total10s uint64  `json:"total_10s"`
	Ok2xx10s uint64  `json:"ok_2xx_10s"`
	Err4xx10s uint64 `json:"err_4xx_10s"`
	Err5xx10s uint64 `json:"err_5xx_10s"`
}

// RequestMetricsCollector 以“按秒滑窗”的方式统计请求量，便于计算 QPS。
// 线程安全：写入与读取都加锁，开销很小（每请求一次 O(1)）。
type RequestMetricsCollector struct {
	mu sync.Mutex

	windowSec int64
	// ring buffer
	sec  []int64
	all  []uint64
	ok2  []uint64
	err4 []uint64
	err5 []uint64
}

func NewRequestMetricsCollector(windowSeconds int) *RequestMetricsCollector {
	if windowSeconds <= 0 {
		windowSeconds = 10
	}
	n := int64(windowSeconds)
	return &RequestMetricsCollector{
		windowSec: n,
		sec:       make([]int64, n),
		all:       make([]uint64, n),
		ok2:       make([]uint64, n),
		err4:      make([]uint64, n),
		err5:      make([]uint64, n),
	}
}

func (c *RequestMetricsCollector) Record(status int) {
	now := time.Now().Unix()
	i := now % c.windowSec

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.sec[i] != now {
		// 桶已过期，重置
		c.sec[i] = now
		c.all[i] = 0
		c.ok2[i] = 0
		c.err4[i] = 0
		c.err5[i] = 0
	}

	c.all[i]++
	if status >= 200 && status < 300 {
		c.ok2[i]++
	} else if status >= 400 && status < 500 {
		c.err4[i]++
	} else if status >= 500 {
		c.err5[i]++
	}
}

func (c *RequestMetricsCollector) Snapshot() APIMetrics {
	now := time.Now().Unix()
	var total, ok2, err4, err5 uint64

	c.mu.Lock()
	defer c.mu.Unlock()

	for idx := int64(0); idx < c.windowSec; idx++ {
		sec := c.sec[idx]
		if sec == 0 {
			continue
		}
		if now-sec >= c.windowSec {
			continue
		}
		total += c.all[idx]
		ok2 += c.ok2[idx]
		err4 += c.err4[idx]
		err5 += c.err5[idx]
	}

	qps := float64(total) / float64(c.windowSec)
	return APIMetrics{
		QPS:       qps,
		Total10s:  total,
		Ok2xx10s:  ok2,
		Err4xx10s: err4,
		Err5xx10s: err5,
	}
}


