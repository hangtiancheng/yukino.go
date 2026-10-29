package contracts

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"
)

// Base64Encode base64-encodes the downstream operator inputs (aligned with the contract §3.2 project_input field).
func Base64Encode(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

// BuildFailReason assembles the failure reason (extensible to structured copy from stage 3).
func BuildFailReason(stage, detail string) string {
	return fmt.Sprintf("%s: %s", stage, detail)
}

// ParseOutputImages parses the image list from the base64 specialInfo.
// Local reproduction: specialInfo is base64(JSON) where the JSON contains an images array; returns empty on parse failure.
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

// ExtractOutputText extracts the output text (title/description) from the base64 specialInfo.
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
