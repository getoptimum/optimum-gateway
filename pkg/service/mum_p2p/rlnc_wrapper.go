package mum_p2p

import (
	"fmt"
	"time"

	mump2pcfg "github.com/getoptimum/mump2p-protocol/pkg/config"
	rlncpbshm "github.com/getoptimum/mump2p-protocol/pkg/rlncpb/shm"
	rlncshm "github.com/getoptimum/mump2p-protocol/pkg/shm"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
)

type RLNCWrapper struct {
	srv *rlncshm.Service
}

func NewRLNCWrapper(psCfg *mump2pcfg.Config) (*RLNCWrapper, error) {
	shmSvc, err := rlncshm.New(psCfg)
	if err != nil {
		return nil, fmt.Errorf("failed init shm: %w", err)
	}
	return &RLNCWrapper{srv: shmSvc}, nil
}

func (r *RLNCWrapper) ExecuteOp(op rlncpbshm.OperationType, data []byte) ([]byte, error) {
	start := time.Now()
	res, err := r.srv.ExecuteOp(op, data)
	telemetry.MeasureRLNC(op.String(), time.Since(start))
	return res, err
}
