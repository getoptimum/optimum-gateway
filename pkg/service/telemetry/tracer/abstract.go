package tracer

type ForkManager interface {
	CheckForkSupported(forkDigest string) bool
	SetObservedDigest(forkDigest string)
}
