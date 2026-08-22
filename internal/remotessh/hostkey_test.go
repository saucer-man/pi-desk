package remotessh

import (
	"errors"
	"strings"
	"testing"
)

func TestParseHostKeyEvidence(t *testing.T) {
	fingerprint := "SHA256:" + strings.Repeat("A", 43)
	debug := "debug1: kex: host key algorithm: ssh-ed25519\n" +
		"debug1: Server host key: ssh-ed25519 " + fingerprint + "\n"
	evidence, err := ParseHostKeyEvidence([]byte(debug))
	if err != nil {
		t.Fatalf("ParseHostKeyEvidence returned an error: %v", err)
	}
	if evidence.Algorithm != "ssh-ed25519" || evidence.SHA256Hash != fingerprint {
		t.Fatalf("unexpected evidence: %#v", evidence)
	}
}

func TestParseHostKeyEvidenceSelectsFinalAuthenticatedProxyHop(t *testing.T) {
	bastion := "SHA256:" + strings.Repeat("B", 43)
	target := "SHA256:" + strings.Repeat("T", 43)
	debug := "debug1: kex: host key algorithm: ssh-ed25519\n" +
		"debug1: Server host key: ssh-ed25519 " + bastion + "\n" +
		"Authenticated to bastion ([127.0.0.1]:22) using \"publickey\".\n" +
		"debug1: kex: host key algorithm: ssh-ed25519\n" +
		"debug1: Server host key: ssh-ed25519 " + target + "\n" +
		"Authenticated to target (via proxy) using \"publickey\".\n" +
		"debug1: Sending command: true\n" +
		"debug1: Server host key: ssh-ed25519 SHA256:" + strings.Repeat("X", 43) + "\n"
	evidence, err := ParseHostKeyEvidence([]byte(debug))
	if err != nil || evidence.Algorithm != "ssh-ed25519" || evidence.SHA256Hash != target {
		t.Fatalf("ProxyJump evidence=%#v err=%v", evidence, err)
	}
}

func TestParseHostKeyEvidenceRejectsProxyChainWithoutCommandBoundary(t *testing.T) {
	debug := "debug1: Server host key: ssh-ed25519 SHA256:" + strings.Repeat("B", 43) + "\n" +
		"Authenticated to bastion using \"publickey\".\n" +
		"debug1: Server host key: ssh-ed25519 SHA256:" + strings.Repeat("T", 43) + "\n" +
		"Authenticated to target (via proxy) using \"publickey\".\n"
	if _, err := ParseHostKeyEvidence([]byte(debug)); !errors.Is(err, ErrHostKeyEvidenceInvalid) {
		t.Fatalf("proxy chain without command boundary error=%v", err)
	}
}

func TestParseHostKeyEvidenceRejectsPartiallyAuthenticatedProxyChain(t *testing.T) {
	debug := "debug1: Server host key: ssh-ed25519 SHA256:" + strings.Repeat("B", 43) + "\n" +
		"Authenticated to bastion using \"publickey\".\n" +
		"debug1: Server host key: ssh-ed25519 SHA256:" + strings.Repeat("T", 43) + "\n"
	if _, err := ParseHostKeyEvidence([]byte(debug)); !errors.Is(err, ErrHostKeyEvidenceInvalid) {
		t.Fatalf("partially authenticated chain error=%v", err)
	}
}

func TestParseHostKeyEvidenceRequiresBoundEvidence(t *testing.T) {
	fingerprint := "SHA256:" + strings.Repeat("B", 43)
	for _, test := range []struct {
		name string
		data string
		want error
	}{
		{name: "missing", data: "debug1: kex: host key algorithm: ssh-ed25519\n", want: ErrHostKeyEvidenceMissing},
		{name: "mismatch", data: "debug1: kex: host key algorithm: ssh-ed25519\ndebug1: Server host key: ecdsa-sha2-nistp256 " + fingerprint + "\n", want: ErrHostKeyEvidenceInvalid},
		{name: "multiple keys", data: "debug1: Server host key: ssh-ed25519 " + fingerprint + "\ndebug1: Server host key: ssh-ed25519 SHA256:" + strings.Repeat("C", 43) + "\n", want: ErrHostKeyEvidenceInvalid},
		{name: "bad fingerprint", data: "debug1: Server host key: ssh-ed25519 SHA256:short\n", want: ErrHostKeyEvidenceMissing},
		{name: "invalid utf8", data: string([]byte{0xff}), want: ErrHostKeyEvidenceInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseHostKeyEvidence([]byte(test.data))
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestParseHostKeyEvidenceBoundsDebugOutput(t *testing.T) {
	_, err := ParseHostKeyEvidence([]byte(strings.Repeat("x", maxHostKeyDebugBytes+1)))
	if !errors.Is(err, ErrHostKeyEvidenceInvalid) {
		t.Fatalf("oversized output error = %v, want ErrHostKeyEvidenceInvalid", err)
	}
}
