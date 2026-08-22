package remotessh

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

const maxHostKeyDebugBytes = 64 << 10

var (
	ErrHostKeyEvidenceInvalid = errors.New("invalid OpenSSH host-key evidence")
	ErrHostKeyEvidenceMissing = errors.New("OpenSSH host-key evidence is missing")
)

var (
	hostKeyAlgorithmPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+@:-]{0,127}$`)
	sha256FingerprintPattern = regexp.MustCompile(`^SHA256:[A-Za-z0-9+/]{43}$`)
)

type HostKeyEvidence struct {
	Algorithm  string
	SHA256Hash string
}

type observedHostKey struct {
	evidence      HostKeyEvidence
	authenticated bool
}

// ParseHostKeyEvidence extracts only the final authenticated host-key
// algorithm and SHA-256 fingerprint from OpenSSH debug output. ProxyJump logs
// each hop before the target; multiple distinct keys are accepted only when
// every observation is closed by OpenSSH's own authentication marker.
func ParseHostKeyEvidence(debug []byte) (HostKeyEvidence, error) {
	if len(debug) > maxHostKeyDebugBytes {
		return HostKeyEvidence{}, fmt.Errorf("%w: debug output exceeds %d bytes", ErrHostKeyEvidenceInvalid, maxHostKeyDebugBytes)
	}
	if !utf8.Valid(debug) {
		return HostKeyEvidence{}, fmt.Errorf("%w: debug output is not valid UTF-8", ErrHostKeyEvidenceInvalid)
	}
	var negotiatedAlgorithm string
	var observations []observedHostKey
	commandDispatched := false
	for _, rawLine := range strings.Split(string(debug), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(rawLine, "\r"))
		if strings.HasPrefix(line, "debug1: Sending command:") {
			commandDispatched = true
			break
		}
		if strings.HasPrefix(line, "debug1: kex: host key algorithm:") {
			negotiatedAlgorithm = firstToken(strings.TrimSpace(strings.TrimPrefix(line, "debug1: kex: host key algorithm:")))
		}
		if strings.HasPrefix(line, "debug1: Server host key:") {
			fields := strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, "debug1: Server host key:")))
			if len(fields) < 2 {
				continue
			}
			candidate := HostKeyEvidence{Algorithm: fields[0]}
			for _, field := range fields[1:] {
				if sha256FingerprintPattern.MatchString(field) {
					candidate.SHA256Hash = field
					break
				}
			}
			if candidate.SHA256Hash == "" {
				continue
			}
			if !hostKeyAlgorithmPattern.MatchString(candidate.Algorithm) || negotiatedAlgorithm != "" && negotiatedAlgorithm != candidate.Algorithm {
				return HostKeyEvidence{}, fmt.Errorf("%w: negotiated and server algorithms differ", ErrHostKeyEvidenceInvalid)
			}
			observations = append(observations, observedHostKey{evidence: candidate})
			negotiatedAlgorithm = ""
		}
		if strings.HasPrefix(line, "Authenticated to ") && len(observations) > 0 {
			observations[len(observations)-1].authenticated = true
		}
	}
	if len(observations) == 0 {
		return HostKeyEvidence{}, ErrHostKeyEvidenceMissing
	}
	first := observations[0].evidence
	distinct := false
	for _, observation := range observations[1:] {
		if observation.evidence != first {
			distinct = true
			break
		}
	}
	if distinct {
		if !commandDispatched {
			return HostKeyEvidence{}, fmt.Errorf("%w: proxy chain lacks command boundary", ErrHostKeyEvidenceInvalid)
		}
		for _, observation := range observations {
			if !observation.authenticated {
				return HostKeyEvidence{}, fmt.Errorf("%w: multiple unbound server host keys", ErrHostKeyEvidenceInvalid)
			}
		}
	}
	return observations[len(observations)-1].evidence, nil
}

func firstToken(value string) string {
	if index := strings.IndexAny(value, " \t"); index >= 0 {
		return value[:index]
	}
	return value
}
