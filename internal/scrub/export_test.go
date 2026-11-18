package scrub

// Forget removes every registered value, so one test does not change the next.
func Forget() {
	knownLock.Lock()
	defer knownLock.Unlock()
	known = map[string]struct{}{}
}
