package test_utils

import (
	"fmt"
	"sync"

	mp2pengine "github.com/getoptimum/mump2p-protocol/pkg/engine"
	rlncpbtypes "github.com/getoptimum/mump2p-protocol/pkg/rlncpb/types"
)

// identityCoefficients is the single coefficient a passthrough symbol carries. It
// keeps the wire envelope well-formed for peers that inspect the coefficient
// vector without implying any linear combination.
var identityCoefficients = []byte{1}

// PassthroughCoder is an in-process stand-in for the RLNC coder that normally runs
// as a sidecar and is reached over shared memory. It carries a whole payload in a
// single symbol, so the mump2p data path (envelopes, router, /mump2p/1.0.0/data)
// runs end to end without the sidecar. Everything above the coding arithmetic is
// the real implementation; only the arithmetic is degenerate.
//
// Tests need this because the sidecar's coder cannot be linked in: its protobuf
// packages register the same descriptors as mump2p-protocol's vendored copies, so
// importing both panics the process at init.
type PassthroughCoder struct {
	mu       sync.Mutex
	payloads map[string][]byte
}

var _ mp2pengine.Engine = (*PassthroughCoder)(nil)

func NewPassthroughCoder() *PassthroughCoder {
	return &PassthroughCoder{payloads: make(map[string][]byte)}
}

func (c *PassthroughCoder) Encode(
	topic, msgID string,
	payload []byte,
	_ rlncpbtypes.CodingType,
) ([]*mp2pengine.Envelope, error) {
	if len(payload) == 0 {
		return nil, fmt.Errorf("empty payload")
	}
	return []*mp2pengine.Envelope{c.envelope(topic, msgID, payload)}, nil
}

func (c *PassthroughCoder) AddSymbol(env *mp2pengine.Envelope) (mp2pengine.AddSymbolResult, error) {
	if env == nil || env.Symbol == nil {
		return mp2pengine.AddSymbolResult{Validity: mp2pengine.ValidityInconsistent},
			fmt.Errorf("nil envelope")
	}
	payload := env.Symbol.GetData()

	c.mu.Lock()
	key := stateKey(env.Topic, env.MsgID, env.ChunkID)
	_, seen := c.payloads[key]
	if !seen {
		c.payloads[key] = payload
	}
	c.mu.Unlock()

	// A repeat of a symbol whose chunk is already whole is exactly what the router
	// calls unnecessary, which is what makes it send an IDONTWANT rather than
	// deliver the payload a second time.
	validity := mp2pengine.ValidityHelpful
	if seen {
		validity = mp2pengine.ValidityUnnecessary
	}
	return mp2pengine.AddSymbolResult{
		Complete:    true,
		FullPayload: payload,
		Rank:        1,
		K:           1,
		Validity:    validity,
	}, nil
}

func (c *PassthroughCoder) Recode(topic, msgID string, chunkID, n uint32) ([]*mp2pengine.Envelope, error) {
	if n < 1 {
		return nil, fmt.Errorf("n must be at least 1, got %d", n)
	}
	c.mu.Lock()
	payload, ok := c.payloads[stateKey(topic, msgID, chunkID)]
	c.mu.Unlock()
	if !ok {
		return nil, nil
	}

	out := make([]*mp2pengine.Envelope, 0, n)
	for range n {
		out = append(out, c.envelope(topic, msgID, payload))
	}
	return out, nil
}

func (c *PassthroughCoder) ChunkDecoded(topic, msgID string, chunkID uint32) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	_, ok := c.payloads[stateKey(topic, msgID, chunkID)]
	return ok
}

func (c *PassthroughCoder) Reset(topic, msgID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.payloads, stateKey(topic, msgID, 0))
}

func (c *PassthroughCoder) envelope(topic, msgID string, payload []byte) *mp2pengine.Envelope {
	return &mp2pengine.Envelope{
		Version:     1,
		Topic:       topic,
		MsgID:       msgID,
		ChunkID:     0,
		TotalChunks: 1,
		Symbol: &rlncpbtypes.Symbol{
			Data:         payload,
			Coefficients: identityCoefficients,
		},
	}
}

func stateKey(topic, msgID string, chunkID uint32) string {
	return fmt.Sprintf("%s|%s|%d", topic, msgID, chunkID)
}
