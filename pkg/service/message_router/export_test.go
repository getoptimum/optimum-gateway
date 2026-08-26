package message_router

import "testing"

// PollAccelerateSlotsForTest points the router at bootstrapURL and runs one poll.
func PollAccelerateSlotsForTest(t *testing.T, s *Service, bootstrapURL string) {
	t.Helper()
	s.cfg.RemoteBootstrapURL = bootstrapURL
	s.pollAccelerateSlots(t.Context())
}
