package btoolssrv

// APILoadBalancer 移植自 DouYinRecorder/lib/loadBalancer/loadBalancer.js。
// 四条 API 通道按优先级加权选择，连续失败 3 次禁用 3 分钟（指数退避）。

import (
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

type balEndpoint struct {
	Name     string
	Priority int
	Weight   int
}

type balStatus struct {
	endpoint    balEndpoint
	failureCount int
	isBlocked    bool
	nextRetryTime int64
}

type apiLoadBalancer struct {
	mu        sync.Mutex
	endpoints []*balStatus
}

var globalLoadBalancer = newAPILoadBalancer()

func newAPILoadBalancer() *apiLoadBalancer {
	lb := &apiLoadBalancer{}
	defaults := []balEndpoint{
		{Name: "web", Priority: 2, Weight: 1},
		{Name: "webHTML", Priority: 1, Weight: 1},
		{Name: "mobile", Priority: 6, Weight: 1},
		{Name: "userHTML", Priority: 4, Weight: 1},
	}
	for _, e := range defaults {
		lb.endpoints = append(lb.endpoints, &balStatus{endpoint: e})
	}
	return lb
}

func (lb *apiLoadBalancer) getNextEndpoint() *balStatus {
	now := time.Now().UnixMilli()
	for _, st := range lb.endpoints {
		if st.isBlocked && now >= st.nextRetryTime {
			st.isBlocked = false
			if st.failureCount > 0 {
				st.failureCount--
			}
		}
	}
	available := make([]*balStatus, 0, len(lb.endpoints))
	for _, st := range lb.endpoints {
		if !st.isBlocked {
			available = append(available, st)
		}
	}
	if len(available) == 0 {
		return nil
	}
	// 优先级小者靠前；同优先级权重不再细分（全为 1，与 JS 行为一致）
	for i := 1; i < len(available); i++ {
		for j := i; j > 0 && available[j-1].endpoint.Priority > available[j].endpoint.Priority; j-- {
			available[j-1], available[j] = available[j], available[j-1]
		}
	}
	totalWeight := 0
	for _, st := range available {
		totalWeight += st.endpoint.Weight
	}
	r := rand.Float64() * float64(totalWeight)
	cur := 0.0
	for _, st := range available {
		cur += float64(st.endpoint.Weight)
		if r <= cur {
			return st
		}
	}
	return available[0]
}

func (lb *apiLoadBalancer) recordFailure(apiType string) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	for _, st := range lb.endpoints {
		if st.endpoint.Name == apiType {
			st.failureCount++
			if st.failureCount >= 3 {
				st.isBlocked = true
				mult := 1.5
				for i := 3; i < st.failureCount; i++ {
					mult *= 1.5
				}
				st.nextRetryTime = time.Now().UnixMilli() + int64(3*60*1000*mult)
			}
			return
		}
	}
}

func (lb *apiLoadBalancer) recordSuccess(apiType string) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	for _, st := range lb.endpoints {
		if st.endpoint.Name == apiType {
			if st.failureCount > 0 {
				st.failureCount--
			}
			if st.isBlocked && st.failureCount == 0 {
				st.isBlocked = false
				st.nextRetryTime = 0
			}
			return
		}
	}
}

func (lb *apiLoadBalancer) callWithLoadBalance(webRoomID string, opts getRoomInfoOpts) (*roomInfoFull, error) {
	lb.mu.Lock()
	maxAttempts := len(lb.endpoints)
	lb.mu.Unlock()
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		st := lb.getNextEndpoint()
		if st == nil {
			return nil, errors.New("所有 API 端点都不可用，请稍后重试")
		}
		apiType := st.endpoint.Name
		sub := opts
		sub.API = apiType
		result, err := getRoomInfo(webRoomID, sub)
		if err == nil {
			lb.recordSuccess(apiType)
			return result, nil
		}
		lastErr = err
		lb.recordFailure(apiType)
		if attempt == maxAttempts-1 {
			break
		}
	}
	return nil, fmt.Errorf("所有 API 调用都失败了。最后一个错误: %v", lastErr)
}
