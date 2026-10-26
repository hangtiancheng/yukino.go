package contracts

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"
)

// Base64Encode 对下游算子入参做 base64 编码（对齐契约 §3.2 的 project_input 字段）。
func Base64Encode(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

// BuildFailReason 组装失败原因（阶段 3 起可扩展为结构化文案）。
func BuildFailReason(stage, detail string) string {
	return fmt.Sprintf("%s: %s", stage, detail)
}

// ParseOutputImages 从 base64 的 specialInfo 里解析图片列表。
// 本地复现：specialInfo 为 base64(JSON)，JSON 里含 images 数组；解析失败返回空。
func ParseOutputImages(specialInfo string) []domain.AuditImage {
	if specialInfo == "" {
		return nil
	}
	decoded, err := base64.StdEncoding.DecodeString(specialInfo)
	if err != nil {
		return nil
	}
	var payload struct {
		Images []string `json:"images"`
		URL    string   `json:"url"`
	}
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return nil
	}
	var images []domain.AuditImage
	for _, u := range payload.Images {
		if u != "" {
			images = append(images, domain.AuditImage{URL: u})
		}
	}
	if payload.URL != "" {
		images = append(images, domain.AuditImage{URL: payload.URL})
	}
	return images
}

// ExtractOutputText 从 base64 的 specialInfo 里提取产出文本（标题/描述）。
func ExtractOutputText(specialInfo string) string {
	if specialInfo == "" {
		return ""
	}
	decoded, err := base64.StdEncoding.DecodeString(specialInfo)
	if err != nil {
		return ""
	}
	var payload struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Text        string `json:"text"`
	}
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return ""
	}
	text := payload.Text
	if text == "" {
		text = payload.Title
	}
	if text == "" {
		text = payload.Description
	}
	return text
}
