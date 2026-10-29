package domain

import (
	"context"
	"sync"
	"time"
)

// DefaultImageTicker is the image risk-dispatch rate-limit interval.
//
// The upstream constant is exactly 10ms (~100 images/s). The semantics are **limit the dispatch rate, not the concurrency**:
// the `<-ticker.C` throttle gate guarantees at least one interval between adjacent dispatches, but the dispatched
// goroutines still run concurrently — neither serially slow nor bursting the downstream instantly.
const DefaultImageTicker = 10 * time.Millisecond

// ImageAuditResult is the result of one image/text risk-control call.
type ImageAuditResult struct {
	Passed bool
	Reason string
	Err    error
}

// SyncAuditImages issues one sync risk-control request per image.
//
// Mechanism (design deep-dive §6.3): `<-ticker.C` is the throttle gate; adjacent dispatches are at least one interval apart;
// dispatched goroutines still run concurrently, so it **limits dispatch rate, not concurrency** — neither serially slow nor bursting the downstream.
// Aggregation semantics (one-vote veto): err > !pass > pass (errors first, then rejections; only all-pass passes).
func SyncAuditImages(ctx context.Context, backend AuditBackend, req *RiskSubmitRequest, subProduct string) (bool, string, error) {
	ticker := time.NewTicker(DefaultImageTicker)
	defer ticker.Stop()

	var wg sync.WaitGroup
	results := make(chan ImageAuditResult, len(req.Content.Image))

	for _, img := range req.Content.Image {
		select {
		case <-ticker.C: // rate-limit gate: wait one tick before each dispatch
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

	// Aggregation (one-vote veto): collect all results first, then decide by err > !pass > pass priority.
	// Note: do not short-circuit on failure — channels are unordered, and short-circuiting would turn "errors first" into "arrival order first";
	// an error could be preempted by a rejected result. Collecting everything first guarantees deterministic priority.
	return AggregateImageResults(results)
}

// AggregateImageResults aggregates the audit results of multiple images/texts (one-vote veto).
// Priority: err > !pass > pass (errors first, then rejections; only all-pass passes).
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
