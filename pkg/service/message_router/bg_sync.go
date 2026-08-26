package message_router

import (
	"context"
	"slices"
	"time"

	commonslices "github.com/getoptimum/optimum-common/pkg/slices"
	"github.com/getoptimum/optimum-gateway/pkg/config"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
)

// bgSync polls authMgr for the validator-indexes claim and replaces the
// knownValidators map when it changes. Manager getters are nil-safe; when
// authMgr is nil (auth disabled) ValidatorIndexes returns an empty slice
// and the per-tick work degenerates to a single comparison, so we don't
// bother short-circuiting here.
func (s *Service) bgSync(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.SetKnownValidators(s.authMgr.ValidatorIndexes())
			s.pollAccelerateSlots(ctx)
		}
	}
}

// SetKnownValidators replaces the set of validators this gateway serves, derived from a freshly
// minted JWT. The indexes are sorted and chunked before replacing the current mapping.
func (s *Service) SetKnownValidators(knownValidators []uint64) {
	slices.Sort(knownValidators)
	chunkedValidators := commonslices.ChunkSlice(knownValidators, config.DefaultAttestationSyncChunkSize)

	replacementChunks := make(map[uint64][2]uint64, len(knownValidators))
	for chunkID, validatorIDs := range chunkedValidators {
		for _, validator := range validatorIDs {
			replacementChunks[validator] = [2]uint64{uint64(chunkID), uint64(len(validatorIDs))}
		}
	}

	s.knownValidators.Replace(replacementChunks)
	telemetry.SetKnownValidatorsTotal(len(replacementChunks))
}
