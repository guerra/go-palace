package embed

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

type Embedder interface {
	Embed(texts []string) ([][]float32, error)
	Dimension() int
}

type FakeEmbedder struct {
	dim int
}

func NewFakeEmbedder(dim int) *FakeEmbedder {
	if dim <= 0 {
		dim = 384
	}
	return &FakeEmbedder{dim: dim}
}

func (f *FakeEmbedder) Dimension() int { return f.dim }

func (f *FakeEmbedder) Embed(texts []string) ([][]float32, error) {
	if texts == nil {
		return nil, fmt.Errorf("embed: nil input")
	}
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = hashVector(t, f.dim)
	}
	return out, nil
}

func hashVector(text string, dim int) []float32 {
	seed := sha256.Sum256([]byte(text))
	vec := make([]float32, dim)
	for i := 0; i < dim; i++ {
		offset := (i * 4) % len(seed)
		end := offset + 4
		if end > len(seed) {
			offset = len(seed) - 4
			end = len(seed)
		}
		bits := binary.LittleEndian.Uint32(seed[offset:end])
		vec[i] = float32(bits) / float32(^uint32(0))
	}
	return vec
}
