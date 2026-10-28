// Package aggregator encodes and decodes batched attestation messages for mump2p transport.
package aggregator

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/config"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/topics"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
)

var (
	PrefixPacker = []byte{0, 8} // PrefixPacker is the wire prefix marking a payload as packer-encoded aggregated attestations.
	PrefixNaive  = []byte{0, 7} // PrefixNaive is the wire prefix marking a payload as a naive proto-marshaled aggregated Msg.
)

// Emitter publishes the marshaled protobuf blob (e.g., single libp2p topic).
type Emitter interface {
	EmitAggregatedMessage(payload []byte)
}

type AggregateItem struct {
	Topic string
	Meta  *topics.TopicMeta
	Data  []byte // either pooled (Enqueue) or external
}

type Service struct {
	log logger.AppLogger
	em  Emitter
	cfg *config.AppConfig
	ch  chan AggregateItem

	packer *AttestationPacker
	ctx    context.Context
}

const defaultAggregationInterval = time.Duration(config.DefaultAggregationIntervalMs) * time.Millisecond

func NewAggregator(ctx context.Context, em Emitter, cfg *config.AppConfig, log logger.AppLogger, checker ValidatorChecker) *Service {
	a := &Service{
		log:    log.With(logger.WithService("aggregator")),
		em:     em,
		cfg:    cfg,
		ch:     make(chan AggregateItem, 4096),
		ctx:    ctx,
		packer: NewAttestationPacker(checker),
	}
	go a.loop()
	return a
}

func (a *Service) DecodeAggregatedMessage(currentForkDigest string, payload []byte) (map[string][][]byte, error) {
	if bytes.HasPrefix(payload, PrefixPacker) {
		return a.packer.Decode(currentForkDigest, payload[len(PrefixPacker):])
	}
	if bytes.HasPrefix(payload, PrefixNaive) {
		var m Msg
		if err := proto.Unmarshal(payload[len(PrefixNaive):], &m); err != nil {
			return nil, fmt.Errorf("failed to unmarshal naive message: %w", err)
		}
		if m.Tms > 0 {
			telemetry.ObserveAttestationPropagationLatency(float64(time.Now().UnixMilli() - m.Tms))
		}
		result := make(map[string][][]byte, len(m.Container))
		for i := range m.Container {
			topic := m.Container[i].Topic
			if _, ok := result[topic]; !ok {
				result[topic] = make([][]byte, 0, len(m.Container[i].Data))
			}
			result[topic] = append(result[topic], m.Container[i].Data...)
		}
		return result, nil
	}
	return nil, fmt.Errorf("invalid aggregated message, did not find expected prefixes")
}

func (a *Service) Enqueue(topic string, meta *topics.TopicMeta, b []byte) {
	buf := make([]byte, len(b))
	copy(buf, b) // copy to avoid aliasing issues
	a.ch <- AggregateItem{
		Topic: topic,
		Meta:  meta,
		Data:  buf,
	}
}

func (a *Service) loop() {
	byTopic := make(map[string][][]byte, 64)
	t := time.NewTicker(a.currentInterval())
	defer t.Stop()
	// use this ticker to aggregate and log pack failures. If the protocol changes,
	// we need to be informed, but with ~30,000 attestations per slot we cannot log every error individually.
	tFailed := time.NewTicker(5 * time.Second)
	defer tFailed.Stop()
	failedAttempts := int64(0)
	var errPack error

	buildAndEmit := func() {
		packStart := time.Now()
		packedItems := a.packer.TotalItems()
		uniqueKeys := a.packer.UniqueDataKeys()
		sendData, err := a.packer.EncodeCurrent()
		if err != nil {
			a.log.Error("failed to encode attestation messages, fallback to ordinary packing mechanism", err)
		}
		if packedItems > 0 {
			telemetry.IncAttestationPack(packedItems)
			telemetry.ObserveAttestationPackUniqueDataKeys(uniqueKeys)
		}
		if len(sendData) > 0 {
			telemetry.ObserveAttestationPackItems(packedItems)
		}
		for i := range sendData {
			telemetry.ObserveAttestationPackSize(len(sendData[i]))
			a.em.EmitAggregatedMessage(sendData[i])
		}
		if packedItems > 0 {
			telemetry.ObserveAttestationPackLatency(float64(time.Since(packStart).Microseconds()) / 1000.0)
		}
		a.packer.Clean()
		if len(byTopic) == 0 {
			return
		}
		msg := &Msg{
			Tms:       time.Now().UnixMilli(),
			Container: make([]*Container, 0, len(byTopic)),
		}
		for topic, batch := range byTopic {
			if len(batch) == 0 {
				continue
			}
			telemetry.IncreaseAggregationIncluded(topic, len(batch))
			msg.Container = append(msg.Container, &Container{
				Topic: topic,
				Data:  batch,
			})
			delete(byTopic, topic)
		}

		if blob, err := proto.Marshal(msg); err == nil {
			a.em.EmitAggregatedMessage(prefixed(PrefixNaive, blob))
		}
	}

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-tFailed.C:
			if failedAttempts > 0 {
				a.log.Error("failed to pack some attestation messages, fallback to ordinary packing mechanism",
					fmt.Errorf("last pack error: %w", errPack),
					logger.WithInt64("failed_attempts", failedAttempts),
				)
				failedAttempts = 0
			}
		case it := <-a.ch:
			if it.Meta != nil && it.Meta.IsAttestation() {
				if errPack = a.packer.Add(it.Meta, it.Data); errPack == nil {
					continue
				}
				failedAttempts++
				telemetry.IncAttestationPackError()
				// for some reason we can't pack attestation message, fallback to ordinary packing mechanism
			}
			if _, ok := byTopic[it.Topic]; !ok {
				byTopic[it.Topic] = make([][]byte, 0, 3_000)
			}
			byTopic[it.Topic] = append(byTopic[it.Topic], it.Data)
		case <-t.C:
			buildAndEmit()
		}
	}
}

func (a *Service) currentInterval() time.Duration {
	if a.cfg == nil {
		return defaultAggregationInterval
	}
	if d := a.cfg.GetAggregationInterval(); d > 0 {
		return d
	}
	return defaultAggregationInterval
}

// prefixed returns a new slice containing prefix concatenated with data.
func prefixed(prefix, data []byte) []byte {
	out := make([]byte, len(prefix)+len(data))
	copy(out, prefix)
	copy(out[len(prefix):], data)
	return out
}
