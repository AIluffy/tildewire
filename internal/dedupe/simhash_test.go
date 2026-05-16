package dedupe

import "testing"

func TestSimHashDistanceRewardsSimilarText(t *testing.T) {
	first := SimHash("SQLite FTS search for terminal source health")
	second := SimHash("SQLite full text search for terminal source health")
	third := SimHash("GPU diffusion model benchmark image generation")

	if first == 0 || second == 0 || third == 0 {
		t.Fatalf("simhash values should be non-zero: %x %x %x", first, second, third)
	}
	if Distance(first, second) >= Distance(first, third) {
		t.Fatalf("similar text distance should be smaller: similar=%d unrelated=%d", Distance(first, second), Distance(first, third))
	}
}

func TestDedupeCandidateKeyOrdersIDs(t *testing.T) {
	first := DedupeCandidateKey("b", "a")
	second := DedupeCandidateKey("a", "b")
	if first != second || first != "a:b" {
		t.Fatalf("candidate key mismatch: first=%q second=%q", first, second)
	}
}
