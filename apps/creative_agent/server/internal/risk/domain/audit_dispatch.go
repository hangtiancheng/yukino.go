package domain

import (
	"context"
	"sync"
	"time"
)

// DefaultImageTicker 图片风控派发限速间隔。
//
// 上游实际常量即 10ms（约 100 张/秒）。语义是**限派发速率、不限并发数**：
// `<-ticker.C` 节流闸门保证相邻两次派发至少隔一个 interval，但派发后的
// goroutine 仍并发跑——既不串行慢，也不瞬间打爆下游。
const DefaultImageTicker = 10 * time.Millisecond

// ImageAuditResult 单次图片/文本风控的返回结果。
type ImageAuditResult struct {
	Passed bool
	Reason string
	Err    error
}

// SyncAuditImages 对每张图片单独发起同步风控请求。
//
// 机制（设计精讲 §六.3）：`<-ticker.C` 是节流闸门，相邻两次派发至少隔一个 interval；
// 派发后 goroutine 仍并发跑，所以是**限派发速率、不限并发数**——既不串行慢，也不瞬间打爆下游。
// 聚合语义（一票否决）：err > !pass > pass（有错优先报错，其次不通过，全过才过）。
func SyncAuditImages(ctx context.Context, backend AuditBackend, req *RiskSubmitRequest, subProduct string) (bool, string, error) {
	ticker := time.NewTicker(DefaultImageTicker)
	defer ticker.Stop()

	var wg sync.WaitGroup
	results := make(chan ImageAuditResult, len(req.Content.Image))

	for _, img := range req.Content.Image {
		select {
		case <-ticker.C: // 限速闸门：每次派发前等一个 tick
		case <-ctx.Done():
			return false, "", ctx.Err()
		}
		wg.Add(1)
		go func(image AuditImage) {
			defer wg.Done()
			singleReq := *req
			singleReq.Content = AuditContent{Image: []AuditImage{image}}
			passed, reason, err := backend.Submit(ctx, &singleReq, subProduct)
			results <- ImageAuditResult{Passed: passed, Reason: reason, Err: err}
		}(img)
	}
	wg.Wait()
	close(results)

	// 聚合（一票否决）：先收集全部结果，再按 err > !pass > pass 优先级判定。
	// 注意：不能遇失败就短路返回——channel 无序，短路会让"错误优先"变成"到达顺序优先"，
	// 错误可能被不通过的 result 抢先覆盖。先收齐再判定才保证优先级确定。
	return AggregateImageResults(results)
}

// AggregateImageResults 聚合多张图片/多条文本的审核结果（一票否决）。
// 优先级：err > !pass > pass（有错优先报错，其次拦截，全过才过）。
func AggregateImageResults(results <-chan ImageAuditResult) (bool, string, error) {
	firstFail := ""
	foundFail := false
	for r := range results {
		if r.Err != nil {
			return false, "", r.Err
		}
		if !r.Passed && !foundFail {
			firstFail = r.Reason
			foundFail = true
		}
	}
	if foundFail {
		return false, firstFail, nil
	}
	return true, "", nil
}
