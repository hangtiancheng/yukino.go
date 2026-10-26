package dao

import (
	"context"
	"fmt"
	"strings"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"
)

// DefaultSensitiveWords 默认敏感词表。
var DefaultSensitiveWords = []string{"赌博", "代考", "非法集资", "色情"}

// LocalAuditBackend 本地敏感词判定后端。
type LocalAuditBackend struct {
	keywords []string
}

// NewLocalAuditBackend 创建本地后端。
func NewLocalAuditBackend() *LocalAuditBackend {
	return &LocalAuditBackend{keywords: DefaultSensitiveWords}
}

// SetSensitiveWords 覆盖敏感词表（测试用）。
func (b *LocalAuditBackend) SetSensitiveWords(words []string) {
	b.keywords = append([]string(nil), words...)
}

// 编译期断言：确保实现满足 domain.AuditBackend 接口。
var _ domain.AuditBackend = (*LocalAuditBackend)(nil)

// Submit 实现 domain.AuditBackend。
func (b *LocalAuditBackend) Submit(ctx context.Context, req *domain.RiskSubmitRequest, subProduct string) (bool, string, error) {
	select {
	case <-ctx.Done():
		return false, "", ctx.Err()
	default:
	}
	keywords := b.keywords
	if len(keywords) == 0 {
		keywords = DefaultSensitiveWords
	}
	text := req.Content.Text
	if strings.TrimSpace(text) == "" && len(req.Content.Image) == 0 {
		return false, "", fmt.Errorf("audit content is empty")
	}
	for _, kw := range keywords {
		if strings.Contains(text, kw) {
			return false, fmt.Sprintf("内容包含敏感词[%s]", kw), nil
		}
	}
	return true, "", nil
}
