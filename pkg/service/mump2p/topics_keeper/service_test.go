package topics_keeper_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	commonio "github.com/getoptimum/optimum-common/pkg/io"
	"github.com/getoptimum/optimum-gateway/pkg/service/mump2p/topics_keeper"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

const topicsDumpFile = "topics.dump"

func TestNewServiceWithMissingFileStartsEmpty(t *testing.T) {
	cnt := test_utils.GetClean(t)

	svc := topics_keeper.NewService(cnt.Ctx, cnt.Log, t.TempDir())
	require.Empty(t, svc.GetAllTopics())
}

func TestNewServiceLoadsPersistedTopics(t *testing.T) {
	cnt := test_utils.GetClean(t)

	dir := t.TempDir()
	writeTopicsDump(t, dir, []string{"topic-a", "", "   ", "topic-b"})

	svc := topics_keeper.NewService(cnt.Ctx, cnt.Log, dir)

	require.ElementsMatch(t, []string{"topic-a", "topic-b"}, svc.GetAllTopics())
}

func TestAddTopicPersistsTopics(t *testing.T) {
	cnt := test_utils.GetClean(t)

	dir := t.TempDir()
	svc := topics_keeper.NewService(cnt.Ctx, cnt.Log, dir)

	svc.AddTopic("topic-c")
	svc.AddTopic("topic-d")

	require.Eventually(t, func() bool {
		topics, ok := tryReadTopicsDump(t, dir)
		return ok && len(topics) == 2 && containsAll(topics, "topic-c", "topic-d")
	}, 5*time.Second, 20*time.Millisecond)
}

func TestRemoveTopicPersistsTopics(t *testing.T) {
	cnt := test_utils.GetClean(t)

	dir := t.TempDir()
	writeTopicsDump(t, dir, []string{"topic-e", "topic-f"})

	svc := topics_keeper.NewService(cnt.Ctx, cnt.Log, dir)
	svc.RemoveTopic("topic-e")

	require.Eventually(t, func() bool {
		topics, ok := tryReadTopicsDump(t, dir)
		return ok && len(topics) == 1 && topics[0] == "topic-f"
	}, 5*time.Second, 20*time.Millisecond)
	require.Equal(t, []string{"topic-f"}, svc.GetAllTopics())
}

func TestDumpDataWritesCurrentTopics(t *testing.T) {
	cnt := test_utils.GetClean(t)

	ctx, cancel := context.WithCancel(cnt.Ctx)
	t.Cleanup(cancel)
	cancel() // stop async persist before it starts; this test exercises DumpData directly

	dir := t.TempDir()
	svc := topics_keeper.NewService(ctx, cnt.Log, dir)

	svc.AddTopic("topic-g")
	svc.AddTopic("topic-h")
	svc.DumpData()

	require.ElementsMatch(t, []string{"topic-g", "topic-h"}, readTopicsDump(t, dir))
}

func writeTopicsDump(t *testing.T, dir string, topics []string) {
	t.Helper()

	data, err := json.Marshal(topics)
	require.NoError(t, err)
	require.NoError(t, commonio.AtomicallySaveToFile(filepath.Join(dir, topicsDumpFile), data))
}

func readTopicsDump(t *testing.T, dir string) []string {
	t.Helper()

	data, err := commonio.LoadFromFile(filepath.Join(dir, topicsDumpFile))
	require.NoError(t, err)

	var topics []string
	require.NoError(t, json.Unmarshal(data, &topics))
	return topics
}

func tryReadTopicsDump(t *testing.T, dir string) ([]string, bool) {
	t.Helper()

	data, err := commonio.LoadFromFile(filepath.Join(dir, topicsDumpFile))
	if err != nil {
		return nil, false
	}

	var topics []string
	require.NoError(t, json.Unmarshal(data, &topics))
	return topics, true
}

func containsAll(items []string, expected ...string) bool {
	set := make(map[string]struct{}, len(items))
	for _, item := range items {
		set[item] = struct{}{}
	}
	for _, item := range expected {
		if _, ok := set[item]; !ok {
			return false
		}
	}
	return true
}
