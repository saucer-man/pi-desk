package remotessh

import "testing"

func FuzzParseEffectiveConfig(f *testing.F) {
	f.Add([]byte(validEffectiveConfig))
	f.Add([]byte("setenv TOKEN=value\n"))
	f.Add([]byte("hostname\n"))
	f.Fuzz(func(t *testing.T, output []byte) {
		config, err := ParseEffectiveConfig(output)
		if err == nil {
			_ = config.Validate()
		}
	})
}

func FuzzParseHostKeyEvidence(f *testing.F) {
	f.Add([]byte("debug1: kex: host key algorithm: ssh-ed25519\ndebug1: Server host key: ssh-ed25519 SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\n"))
	f.Add([]byte("debug1: no host key here\n"))
	f.Fuzz(func(t *testing.T, debug []byte) {
		_, _ = ParseHostKeyEvidence(debug)
	})
}
