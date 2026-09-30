package chat_pipeline

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/prompt"
	"github.com/cloudwego/eino/schema"
	"github.com/hangtiancheng/yukino.go/yukino_agent/internal/config"
)

// ChatTemplateConfig defines the prompt template structure for the chat agent.
type ChatTemplateConfig struct {
	FormatType schema.FormatType
	Templates  []schema.MessagesTemplate
}

// newChatTemplate creates a chat prompt template that includes:
// - A system prompt with role definition and context information
// - A placeholder for conversation history
// - A user message template with the current query
//
// The log topic line is injected only when both LogTopicRegion and LogTopicID
// are configured; otherwise it is omitted (mirrors Next.js LOG_TOPIC_* behavior).
func newChatTemplate(ctx context.Context, cfg *config.Config) (prompt.ChatTemplate, error) {
	tplCfg := &ChatTemplateConfig{
		FormatType: schema.FString,
		Templates: []schema.MessagesTemplate{
			schema.SystemMessage(buildSystemPrompt(cfg)),
			schema.MessagesPlaceholder("history", false),
			schema.UserMessage("{content}"),
		},
	}
	return prompt.FromMessages(tplCfg.FormatType, tplCfg.Templates...), nil
}

// buildSystemPrompt assembles the system prompt string. The log topic line is
// conditionally included based on configuration.
func buildSystemPrompt(cfg *config.Config) string {
	logTopicLine := ""
	if cfg.LogTopicRegion != "" && cfg.LogTopicID != "" {
		logTopicLine = "  * Log topic region: " + cfg.LogTopicRegion + "; log topic ID: " + cfg.LogTopicID
	}
	return fmt.Sprintf(`# Role: Conversational Assistant

## Core Capabilities

- Context-aware conversation and dialogue
- Web search for information retrieval

## Interaction Guidelines

- Before responding, ensure you:
  - Fully understand the user's needs and questions; ask for clarification if unclear
  - Consider the most appropriate solution approach
    %s
- When providing assistance:
  - Use clear and concise language
  - Provide practical examples when appropriate
  - Reference documentation when helpful
  - Suggest improvements or next steps when applicable
- If a request exceeds your capabilities:
  - Clearly state your limitations and suggest alternative approaches
- For complex or compound questions, think step by step rather than rushing to a low-quality answer.

## Output Requirements

- Readable and well-structured with line breaks where necessary
- Output markdown only

## Context Information

- Current date: {date}
- Related documents: |-
  ==== Documents Start ====
  {documents}
  ==== Documents End ====
`, logTopicLine)
}
