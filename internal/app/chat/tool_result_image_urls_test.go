package chat

import (
	"context"
	"strings"
	"testing"

	"github.com/KoukeNeko/KoukeBOT/internal/domain"
	"github.com/KoukeNeko/KoukeBOT/internal/infra/llm"
)

func TestCollectGeneratedImageURLsExtractsAndDeduplicates(t *testing.T) {
	results := []domain.ToolResult{
		{
			Content: "Generated 1 image(s):\n- https://sd_img.koukeneko.cafe/outputs/a.png\n",
		},
		{
			Content: "Preview: https://sd_img.koukeneko.cafe/outputs/a.png\nAlt: https://sd_img.koukeneko.cafe/outputs/b.webp",
		},
		{
			IsError: true,
			Content: "https://sd_img.koukeneko.cafe/outputs/ignored.jpg",
		},
	}

	got := collectGeneratedImageURLs(results)
	if len(got) != 2 {
		t.Fatalf("len(urls) = %d, want 2", len(got))
	}
	if got[0] != "https://sd_img.koukeneko.cafe/outputs/a.png" {
		t.Fatalf("urls[0] = %q", got[0])
	}
	if got[1] != "https://sd_img.koukeneko.cafe/outputs/b.webp" {
		t.Fatalf("urls[1] = %q", got[1])
	}
}

func TestAppendGeneratedImageURLsAppendsMissingOnly(t *testing.T) {
	raw := "[Response]: done\nhttps://sd_img.koukeneko.cafe/outputs/a.png"
	urls := []string{
		"https://sd_img.koukeneko.cafe/outputs/a.png",
		"https://sd_img.koukeneko.cafe/outputs/b.png",
	}

	got := appendGeneratedImageURLs(raw, urls)
	if strings.Count(got, "https://sd_img.koukeneko.cafe/outputs/a.png") != 1 {
		t.Fatalf("expected existing URL to remain single, got: %s", got)
	}
	if !strings.Contains(got, "https://sd_img.koukeneko.cafe/outputs/b.png") {
		t.Fatalf("expected missing URL appended, got: %s", got)
	}
}

func TestFinalizeToolLoopResponseAppendsGeneratedImageURLs(t *testing.T) {
	service := &ChatService{
		assembler: NewContextAssembler(),
	}
	state := newToolLoopState(DefaultToolExecutionPolicy(), service.assembler.PromptGuardPolicy())
	state.fullResponse = "[Status]: Complete\n[Analysis]: Tool call finished.\n[Response]: 生成完成。"
	state.generatedImageURLs = []string{"https://sd_img.koukeneko.cafe/outputs/final.png"}
	outputCh := make(chan llm.StreamChunk, 2)

	view := service.finalizeToolLoopResponse(context.Background(), &domain.Conversation{}, nil, outputCh, state)
	if !strings.Contains(view.Display, "https://sd_img.koukeneko.cafe/outputs/final.png") {
		t.Fatalf("expected final display to include image URL, got: %s", view.Display)
	}
	if len(outputCh) == 0 {
		t.Fatalf("expected one final chunk")
	}
	chunk := <-outputCh
	if !strings.Contains(chunk.Content, "https://sd_img.koukeneko.cafe/outputs/final.png") {
		t.Fatalf("expected emitted chunk to include image URL, got: %s", chunk.Content)
	}
}
