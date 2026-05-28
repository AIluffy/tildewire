package dedupe

import (
	"fmt"
	"hash/fnv"
	"math/bits"
	"strconv"
	"strings"
	"unicode"
)

// SimHash computes a 64-bit similarity hash from normalized text tokens.
func SimHash(text string) uint64 {
	tokens := simHashTokens(text)
	if len(tokens) == 0 {
		return 0
	}
	var weights [64]int
	for _, token := range tokens {
		hash := hashToken(token)
		for bit := 0; bit < 64; bit++ {
			if hash&(uint64(1)<<bit) != 0 {
				weights[bit]++
			} else {
				weights[bit]--
			}
		}
	}
	var result uint64
	for bit, weight := range weights {
		if weight > 0 {
			result |= uint64(1) << bit
		}
	}
	return result
}

// SimHashHex formats a similarity hash as a stable hex string.
func SimHashHex(hash uint64) string {
	if hash == 0 {
		return ""
	}
	return fmt.Sprintf("%016x", hash)
}

// ParseSimHashHex parses a hash created by SimHashHex.
func ParseSimHashHex(value string) (uint64, bool) {
	value = strings.TrimSpace(value)
	if len(value) != 16 {
		return 0, false
	}
	hash, err := strconv.ParseUint(value, 16, 64)
	if err != nil {
		return 0, false
	}
	return hash, true
}

// Distance returns the hamming distance between two SimHash values.
func Distance(first, second uint64) int {
	return bits.OnesCount64(first ^ second)
}

// DedupeCandidateKey creates a stable key for a pair of item IDs.
func DedupeCandidateKey(first, second string) string {
	if first > second {
		first, second = second, first
	}
	return first + ":" + second
}

func simHashTokens(text string) []string {
	var tokens []string
	var current strings.Builder
	flush := func() {
		if current.Len() == 0 {
			return
		}
		token := strings.ToLower(current.String())
		if len([]rune(token)) >= 2 {
			tokens = append(tokens, token)
		}
		current.Reset()
	}
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return tokens
}

func hashToken(token string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(token))
	return h.Sum64()
}
