package llm

import (
	"testing"

	"google.golang.org/genai"
)

func TestBuildGenerateContentConfigOmitsZeroPenaltyFields(t *testing.T) {
	cfg := genConfig{
		temperature:      0.7,
		maxTokens:        2048,
		topP:             0.95,
		frequencyPenalty: 0,
		presencePenalty:  0,
	}

	got := buildGenerateContentConfig("sys", cfg)
	if got.FrequencyPenalty != nil {
		t.Fatalf("FrequencyPenalty should be omitted when zero")
	}
	if got.PresencePenalty != nil {
		t.Fatalf("PresencePenalty should be omitted when zero")
	}
}

func TestBuildGenerateContentConfigIncludesNonZeroPenaltyFields(t *testing.T) {
	cfg := genConfig{
		temperature:      0.7,
		maxTokens:        2048,
		topP:             0.95,
		frequencyPenalty: 0.3,
		presencePenalty:  0.4,
	}

	got := buildGenerateContentConfig("sys", cfg)
	if got.FrequencyPenalty == nil || *got.FrequencyPenalty != float32(0.3) {
		t.Fatalf("FrequencyPenalty = %v, want 0.3", got.FrequencyPenalty)
	}
	if got.PresencePenalty == nil || *got.PresencePenalty != float32(0.4) {
		t.Fatalf("PresencePenalty = %v, want 0.4", got.PresencePenalty)
	}
}

func TestConvertJSONSchemaToGeminiSchemaHandlesNestedArrayItemsAndAnyOf(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"target_user_ids": map[string]interface{}{
				"type": "array",
			},
			"filters": map[string]interface{}{
				"type":     "object",
				"required": []interface{}{"status"},
				"properties": map[string]interface{}{
					"status": map[string]interface{}{
						"type": "string",
						"enum": []interface{}{"open", "done"},
					},
					"tags": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"anyOf": []interface{}{
								map[string]interface{}{"type": "string"},
								map[string]interface{}{"type": "integer"},
							},
						},
					},
				},
			},
		},
		"required": []interface{}{"target_user_ids"},
	}

	got := convertJSONSchemaToGeminiSchema(schema)
	if got.Type != genai.TypeObject {
		t.Fatalf("root type = %q, want %q", got.Type, genai.TypeObject)
	}

	targetUserIDs := got.Properties["target_user_ids"]
	if targetUserIDs == nil {
		t.Fatalf("missing target_user_ids property")
	}
	if targetUserIDs.Type != genai.TypeArray {
		t.Fatalf("target_user_ids type = %q, want %q", targetUserIDs.Type, genai.TypeArray)
	}
	if targetUserIDs.Items == nil {
		t.Fatalf("target_user_ids.items should be auto-filled for array")
	}
	if targetUserIDs.Items.Type != genai.TypeString {
		t.Fatalf("target_user_ids.items.type = %q, want %q", targetUserIDs.Items.Type, genai.TypeString)
	}

	filters := got.Properties["filters"]
	if filters == nil {
		t.Fatalf("missing filters property")
	}
	if len(filters.Required) != 1 || filters.Required[0] != "status" {
		t.Fatalf("filters.required = %v, want [status]", filters.Required)
	}

	status := filters.Properties["status"]
	if status == nil {
		t.Fatalf("missing filters.status property")
	}
	if len(status.Enum) != 2 {
		t.Fatalf("filters.status.enum = %v, want 2 values", status.Enum)
	}

	tags := filters.Properties["tags"]
	if tags == nil || tags.Items == nil {
		t.Fatalf("missing filters.tags.items")
	}
	if len(tags.Items.AnyOf) != 2 {
		t.Fatalf("filters.tags.items.anyOf = %d, want 2", len(tags.Items.AnyOf))
	}
}
