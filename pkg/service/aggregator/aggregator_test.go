package aggregator_test

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	commonhash "github.com/getoptimum/optimum-common/pkg/hash"
	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-common/pkg/syncx"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/topics"
	"github.com/getoptimum/optimum-gateway/pkg/service/aggregator"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

type fakeEmitter struct {
	ch         chan []byte
	msgList    syncx.RWSlice[*aggregator.Msg]
	msgRawList syncx.RWSlice[[]byte]
}

func newFakeEmitter() *fakeEmitter {
	fem := &fakeEmitter{
		ch:         make(chan []byte, 1000),
		msgList:    syncx.RWSlice[*aggregator.Msg]{},
		msgRawList: syncx.RWSlice[[]byte]{},
	}
	return fem
}

func (f *fakeEmitter) EmitAggregatedMessage(payload []byte) {
	cp := make([]byte, len(payload)) // copy to avoid tests aliasing our internal buffer
	copy(cp, payload)
	f.ch <- cp
}

func (f *fakeEmitter) LastBlockReceivedMs() int64 { return 0 }

func (f *fakeEmitter) run(t *testing.T) {
	t.Helper()

	for msg := range f.ch {
		f.msgRawList.Add(msg)
		if bytes.HasPrefix(msg, aggregator.PrefixPacker) {
			continue
		}
		var cnt aggregator.Msg
		require.NoError(t, proto.Unmarshal(msg[len(aggregator.PrefixNaive):], &cnt))
		f.msgList.Add(&cnt)
	}
}

func TestAggregation_NoEmission(t *testing.T) {
	em := newFakeEmitter()
	srv := aggregator.NewAggregator(t.Context(), em, nil, logger.NewAppSLogger(logger.Debug), nil)
	require.NotNil(t, srv)

	select {
	case v := <-em.ch:
		t.Fatal("should not have emitted yet", v)
	case <-time.After(1 * time.Second):
		return
	}
}

func TestAggregation_Emission(t *testing.T) {
	// given
	em := newFakeEmitter()
	go em.run(t)
	srv := aggregator.NewAggregator(t.Context(), em, nil, logger.NewAppSLogger(logger.Debug), nil)
	require.NotNil(t, srv)
	sendItems := make(map[string]aggregator.AggregateItem, 1_000)

	// when
	go func() {
		ticker := time.NewTicker(1 * time.Millisecond)
		defer ticker.Stop()
		for i := range 1_000 {
			<-ticker.C
			key := uuid.NewString()
			topic := fmt.Sprintf("topic%d", i%10)
			item := aggregator.AggregateItem{
				Topic: topic,
				Data:  []byte(key),
			}
			sendItems[key] = item
			srv.Enqueue(item.Topic, topics.ParseTopicMeta(item.Topic), item.Data)
		}
	}()

	// then
	require.Eventually(t, func() bool {
		totalItems := em.msgList.LoadAll()
		counter := 0
		for i := range totalItems {
			for _, item := range totalItems[i].Container {
				counter += len(item.Data)
			}
		}
		// each message per 1ms, emit at 25ms, so should have ~40 messages
		// make a bit less in case of timing issues
		return counter == 1_000 && len(totalItems) > 35
	}, 10*time.Second, 100*time.Millisecond, "should have received aggregated messages")

	// verify that received messages match sent
	totalItems := em.msgList.LoadAll()
	for i := range totalItems {
		for _, item := range totalItems[i].Container {
			for _, data := range item.Data {
				require.Contains(t, sendItems, string(data), "should contain sent item")
				require.Equal(t, item.Topic, sendItems[string(data)].Topic)
				delete(sendItems, string(data))
			}
		}
	}
	require.Empty(t, sendItems, "all sent items should have been received")
}

func TestAggregation(t *testing.T) {
	em := newFakeEmitter()
	go em.run(t)
	srv := aggregator.NewAggregator(t.Context(), em, nil, logger.NewAppSLogger(logger.Debug), nil)

	data := generateAttestationData(t)
	sendMessages := make(map[string]map[string][]byte, 74)
	for i := range items {
		attestation := test_utils.SSZSnappyEncode(t, generateAttestation(t, data))
		topic := generateAttestationTopic(t)
		if _, ok := sendMessages[topic]; !ok {
			sendMessages[topic] = make(map[string][]byte, items)
		}
		sendMessages[topic][commonhash.SHA256(attestation)] = attestation

		// random_topic
		randomTopic := fmt.Sprintf("topic%d", i%10)
		randomData := test_utils.RandBytes(1024)
		if _, ok := sendMessages[randomTopic]; !ok {
			sendMessages[randomTopic] = make(map[string][]byte, items)
		}
		sendMessages[randomTopic][commonhash.SHA256(randomData)] = randomData
	}

	// when
	go func() {
		for topic := range sendMessages {
			for _, generatedData := range sendMessages[topic] {
				srv.Enqueue(topic, topics.ParseTopicMeta(topic), generatedData)
			}
		}
	}()

	// then
	require.Eventually(t, func() bool {
		return em.msgRawList.Len() > 0
	}, 10*time.Second, 100*time.Millisecond, "should have emitted aggregated messages")
	messages := em.msgRawList.LoadAll()
	for i := range messages {
		result, err := srv.DecodeAggregatedMessage(testForkDigest, messages[i])
		require.NoError(t, err)
		for decodedTopic, payloadList := range result {
			_, ok := sendMessages[decodedTopic]
			require.True(t, ok)

			for _, payload := range payloadList {
				_, ok = sendMessages[decodedTopic][commonhash.SHA256(payload)]
				require.True(t, ok)
			}
		}
	}
}

func TestAggregationPackAttestations(t *testing.T) {
	// given - use a long aggregation interval so all items are enqueued before the first tick
	em := newFakeEmitter()
	go em.run(t)
	t.Setenv("OPT_AGGREGATION_INTERVAL_MS", "500")
	cfg := test_utils.GetTestConfig(t)
	srv := aggregator.NewAggregator(t.Context(), em, cfg, logger.NewAppSLogger(logger.Debug), nil)
	data := generateAttestationData(t)
	generatedTopics := make(map[string]struct{}, 64)
	generatedAttestations := make(map[string]struct{}, items)

	// when
	for range items {
		attestation := test_utils.SSZSnappyEncode(t, generateAttestation(t, data))
		topic := generateAttestationTopic(t)
		generatedTopics[topic] = struct{}{}
		generatedAttestations[commonhash.SHA256(attestation)] = struct{}{}
		srv.Enqueue(topic, topics.ParseTopicMeta(topic), attestation)
	}

	// then
	require.Eventually(t, func() bool {
		return em.msgRawList.Len() >= 1
	}, 10*time.Second, 100*time.Millisecond, "should have received packed attestation message")

	seenTopics := make(map[string]struct{})
	seenAttestations := make(map[string]struct{})
	for _, raw := range em.msgRawList.LoadAll() {
		dataDecoded, err := srv.DecodeAggregatedMessage(testForkDigest, raw)
		require.NoError(t, err)
		for topic, attestList := range dataDecoded {
			seenTopics[topic] = struct{}{}
			for _, attestationBytes := range attestList {
				seenAttestations[commonhash.SHA256(attestationBytes)] = struct{}{}
			}
		}
	}
	require.Equal(t, generatedTopics, seenTopics)
	require.Equal(t, generatedAttestations, seenAttestations)
}
