package topics_keeper

import (
	"context"
	"encoding/json"
	"path"
	"strings"

	commonio "github.com/getoptimum/optimum-common/pkg/io"
	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-common/pkg/syncx"
)

const (
	fileNameTopicsDump = "topics.dump"
)

// Service manages persistent storage of P2P topic subscriptions.
// It automatically persists topic changes to disk and restores them on initialization.
type Service struct {
	ctx context.Context
	log logger.AppLogger

	topicsMap   *syncx.RWMap[string, struct{}]
	topicsChan  chan string
	storagePath string
}

func NewService(ctx context.Context, log logger.AppLogger, storageFolder string) *Service {
	srv := &Service{
		ctx:         ctx,
		log:         log,
		topicsMap:   syncx.NewRWMap[string, struct{}](),
		topicsChan:  make(chan string, 100),
		storagePath: path.Join(storageFolder, fileNameTopicsDump),
	}
	srv.loadData()
	go srv.persist()
	return srv
}

func (tk *Service) persist() {
	for {
		select {
		case <-tk.ctx.Done():
			return
		case <-tk.topicsChan:
			tk.DumpData()
		}
	}
}

// Stop flushes the current topics to disk. The persist loop stops when the
// root context passed to NewService is canceled.
func (tk *Service) Stop() {
	tk.DumpData()
}

func (tk *Service) AddTopic(topic string) {
	tk.topicsMap.Store(topic, struct{}{})
	select {
	case tk.topicsChan <- topic:
	case <-tk.ctx.Done():
	default:
	}
}

func (tk *Service) GetAllTopics() []string {
	return tk.topicsMap.Keys()
}

func (tk *Service) RemoveTopic(topic string) {
	tk.topicsMap.Delete(topic)
	select {
	case tk.topicsChan <- topic:
	case <-tk.ctx.Done():
	default:
	}
}

func (tk *Service) loadData() {
	data, err := commonio.LoadFromFile(tk.storagePath)
	if err != nil {
		tk.log.Error("failed to load topics data", err)
		return
	}
	var topics []string
	if err = json.Unmarshal(data, &topics); err != nil {
		tk.log.Error("failed to unmarshal topics data", err)
		return
	}
	for _, topic := range topics {
		if strings.TrimSpace(topic) == "" {
			continue
		}
		tk.topicsMap.Store(topic, struct{}{})
	}
}

func (tk *Service) DumpData() {
	data, _ := json.Marshal(tk.topicsMap.Keys()) // ignore error, as marshaling a slice of strings should not fail
	if err := commonio.AtomicallySaveToFile(tk.storagePath, data); err != nil {
		tk.log.Error("failed to dump topics data", err)
	}
}
