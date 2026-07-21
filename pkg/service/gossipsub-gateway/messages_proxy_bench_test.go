package gossipsub_gateway

import (
	"testing"
	"time"

	commonhash "github.com/getoptimum/optimum-common/pkg/hash"
	"github.com/getoptimum/optimum-common/pkg/syncx"
)

var benchResultUint64 uint64

func BenchmarkXXHash(b *testing.B) {
	// Simulate typical beacon block message sizes
	smallMsg := make([]byte, 1024)      // 1KB
	mediumMsg := make([]byte, 128*1024) // 128KB
	largeMsg := make([]byte, 1024*1024) // 1MB

	b.Run("1KB", func(b *testing.B) {
		b.SetBytes(int64(len(smallMsg)))
		for i := 0; i < b.N; i++ {
			benchResultUint64 = commonhash.XXHash(smallMsg)
		}
	})

	b.Run("128KB", func(b *testing.B) {
		b.SetBytes(int64(len(mediumMsg)))
		for i := 0; i < b.N; i++ {
			benchResultUint64 = commonhash.XXHash(mediumMsg)
		}
	})

	b.Run("1MB", func(b *testing.B) {
		b.SetBytes(int64(len(largeMsg)))
		for i := 0; i < b.N; i++ {
			benchResultUint64 = commonhash.XXHash(largeMsg)
		}
	})
}

func BenchmarkIsDuplicateMessage(b *testing.B) {
	b.Run("unique_messages", func(b *testing.B) {
		svc := &Service{
			messagesMap: syncx.NewTTLMap[uint64, struct{}](1*time.Minute, 1*time.Minute),
		}
		// Generate unique message per iteration using loop index
		msg := make([]byte, 1024)
		copy(msg, "message")
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Encode i into bytes to make each message unique
			msg[7] = byte(i >> 24)
			msg[8] = byte(i >> 16)
			msg[9] = byte(i >> 8)
			msg[10] = byte(i)
			svc.isDuplicateMessage(msg)
		}
	})

	b.Run("duplicate_messages", func(b *testing.B) {
		// Create fresh service and prime the cache with one message
		svc := &Service{
			messagesMap: syncx.NewTTLMap[uint64, struct{}](1*time.Minute, 1*time.Minute),
		}
		msg := []byte("repeated message for duplicate test")
		svc.isDuplicateMessage(msg)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			svc.isDuplicateMessage(msg)
		}
	})
}
