package domain

// BlockType classifies prompt segments in the Dynamic Context Assembly pipeline.
type BlockType int

const (
	// BlockSystem is the immutable system instruction (identity + thinking protocol + format).
	BlockSystem BlockType = iota
	// BlockAnchor holds few-shot examples for style anchoring.
	BlockAnchor
	// BlockSummary is a compressed narrative of older conversation (legacy model only).
	BlockSummary
	// BlockRecent is the rolling window of raw recent messages.
	BlockRecent
	// BlockReinforcement is the hidden format reminder (legacy model only).
	BlockReinforcement
)

// Block is a single segment in the assembled context.
type Block struct {
	Type    BlockType
	Role    Role
	Content string
}
