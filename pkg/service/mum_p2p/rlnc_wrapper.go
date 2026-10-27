package mum_p2p

import (
	"fmt"
	"time"

	mump2pcfg "github.com/getoptimum/mump2p-protocol/pkg/config"
	rlncpbshm "github.com/getoptimum/mump2p-protocol/pkg/rlncpb/shm"
	rlncshm "github.com/getoptimum/mump2p-protocol/pkg/shm"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
	"google.golang.org/protobuf/proto"
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

func (r *RLNCWrapper) Capacity() int {
	return r.srv.Capacity()
}

func (r *RLNCWrapper) ExecuteOp(op rlncpbshm.OperationType, data []byte) ([]byte, error) {
	start := time.Now()
	res, err := r.srv.ExecuteOp(op, data)
	telemetry.MeasureRLNC(op.String(), time.Since(start))
	return res, err
}

// ExecuteMessage mirrors ExecuteOp for the proto-typed path engine.SHMService
// requires from mump2p-protocol v0.3.0 onward. This type is a telemetry
// decorator only, so the method exists to keep the same measurement around the
// new call rather than to add behaviour.
func (r *RLNCWrapper) ExecuteMessage(op rlncpbshm.OperationType, req, resp proto.Message) error {
	start := time.Now()
	err := r.srv.ExecuteMessage(op, req, resp)
	telemetry.MeasureRLNC(op.String(), time.Since(start))
	return err
}
