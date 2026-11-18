package bootstrapper

func SetExternalIPsFunc(fn func() (string, string, error)) func() {
	prev := getExternalIPs
	getExternalIPs = fn
	return func() { getExternalIPs = prev }
}
