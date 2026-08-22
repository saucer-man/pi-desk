package remotessh

import (
	"strings"
	"testing"
)

func TestClassifyOpenSSHFailure(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		code   FailureCode
		reason FailureReason
	}{
		{
			name:   "unknown host key",
			stderr: "No ED25519 host key is known for build.example.test and you have requested strict checking.\nHost key verification failed.",
			code:   FailureHostKeyUnknown,
			reason: ReasonHostKeyUnknown,
		},
		{
			name:   "changed host key",
			stderr: "@@@@@@@@ WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED! @@@@@@@@\nOffending ED25519 key in /home/test/.ssh/known_hosts:1",
			code:   FailureHostKeyChanged,
			reason: ReasonHostKeyChanged,
		},
		{
			name:   "public key rejected",
			stderr: "deploy@build.example.test: Permission denied (publickey).",
			code:   FailureAuthRequired,
			reason: ReasonAuthenticationRejected,
		},
		{
			name:   "encrypted key",
			stderr: "Load key /home/test/.ssh/id_ed25519: incorrect passphrase supplied to decrypt private key",
			code:   FailureAuthRequired,
			reason: ReasonKeyPassphraseRequired,
		},
		{
			name:   "password-only target",
			stderr: "root@build.example.test: Permission denied (publickey,password).",
			code:   FailureAuthRequired,
			reason: ReasonAuthenticationRejected,
		},
		{
			name:   "proxy jump closed",
			stderr: "kex_exchange_identification: Connection closed by remote host\nConnection closed by UNKNOWN port 65535",
			code:   FailureConnect,
			reason: ReasonConnectionClosed,
		},
		{
			name:   "proxy jump forwarding refused",
			stderr: "channel 0: open failed: connect failed: Connection refused\nstdio forwarding failed",
			code:   FailureConnect,
			reason: ReasonConnectionRefused,
		},
		{
			name:   "banner timeout",
			stderr: "Connection timed out during banner exchange",
			code:   FailureConnect,
			reason: ReasonConnectionTimeout,
		},
		{
			name:   "name resolution",
			stderr: "ssh: Could not resolve hostname build.invalid: Name or service not known",
			code:   FailureConnect,
			reason: ReasonNameResolution,
		},
		{
			name:   "connection refused",
			stderr: "ssh: connect to host 127.0.0.1 port 22: Connection refused",
			code:   FailureConnect,
			reason: ReasonConnectionRefused,
		},
		{
			name:   "config",
			stderr: "command-line line 0: Invalid SetEnv.",
			code:   FailureConnect,
			reason: ReasonConfig,
		},
		{
			name:   "unknown",
			stderr: "unexpected failure for a-private-host",
			code:   FailureConnect,
			reason: ReasonUnknown,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			failure := ClassifyOpenSSHFailure([]byte(test.stderr))
			if failure.Code != test.code || failure.Reason != test.reason {
				t.Fatalf("ClassifyOpenSSHFailure = %#v, want code=%q reason=%q", failure, test.code, test.reason)
			}
		})
	}
}

func TestClassifyOpenSSHFailureDoesNotReturnSensitiveText(t *testing.T) {
	secret := "sensitive-host-and-user"
	failure := ClassifyOpenSSHFailure([]byte("ssh: Could not resolve hostname " + secret))
	projection := string(failure.Code) + string(failure.Reason)
	if strings.Contains(projection, secret) {
		t.Fatalf("failure projection leaked input: %#v", failure)
	}

	oversized := []byte(strings.Repeat("private-prefix", maxSSHFailureInputBytes) + " Permission denied (publickey).")
	failure = ClassifyOpenSSHFailure(oversized)
	if failure.Code != FailureAuthRequired {
		t.Fatalf("tail classification = %#v, want auth failure", failure)
	}
}

func FuzzClassifyOpenSSHFailure(f *testing.F) {
	f.Add([]byte("Permission denied (publickey)."))
	f.Add([]byte("Host key verification failed."))
	f.Fuzz(func(t *testing.T, stderr []byte) {
		failure := ClassifyOpenSSHFailure(stderr)
		if failure.Code == "" || failure.Reason == "" {
			t.Fatalf("empty failure classification: %#v", failure)
		}
	})
}
