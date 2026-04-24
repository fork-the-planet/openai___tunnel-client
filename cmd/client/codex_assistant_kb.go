package main

import (
	"strings"

	assistantkb "go.openai.org/api/tunnel-client/docs"
)

func buildCodexAssistantKnowledgeItem(prompt string) map[string]any {
	text := assistantkb.BuildPromptContext(prompt)
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return map[string]any{
		"type": "message",
		"role": "developer",
		"content": []map[string]any{
			{
				"type": "input_text",
				"text": text,
			},
		},
	}
}
