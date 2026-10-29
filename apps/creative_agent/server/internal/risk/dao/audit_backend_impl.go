package dao

import (
	"context"
	"fmt"
	"strings"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"
)

// DefaultSensitiveWords is the default sensitive-word list (product content, kept in Chinese).
var DefaultSensitiveWords = []string{"赌博", "代考", "非法集资", "色情"}

// LocalAuditBackend is the local sensitive-word decision backend.
type LocalAuditBackend struct {
	keywords []string
}

// NewLocalAuditBackend creates the local backend.
func NewLocalAuditBackend() *LocalAuditBackend {
	return &LocalAuditBackend{keywords: DefaultSensitiveWords}
}

// SetSensitiveWords overrides the sensitive-word list (for tests).
func (b *LocalAuditBackend) SetSensitiveWords(words []string) {
	b.keywords = append([]string(nil), words...)
}

// Compile-time assertion: the implementation satisfies the domain.AuditBackend interface.
var _ domain.AuditBackend = (*LocalAuditBackend)(nil)

// Submit implements domain.AuditBackend.
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
