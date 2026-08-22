package remotessh

import (
	"strings"
	"unicode/utf8"
)

const maxSSHFailureInputBytes = 64 << 10

type FailureCode string

const (
	FailureAuthRequired          FailureCode = "REMOTE_AUTH_REQUIRED"
	FailureHostKeyUnknown        FailureCode = "REMOTE_HOST_KEY_UNKNOWN"
	FailureHostKeyChanged        FailureCode = "REMOTE_HOST_KEY_CHANGED"
	FailureConnect               FailureCode = "REMOTE_CONNECT_FAILED"
	FailureDisconnected          FailureCode = "REMOTE_DISCONNECTED"
	FailureCancelled             FailureCode = "REMOTE_CANCELLED"
	FailureOutputLimit           FailureCode = "REMOTE_OUTPUT_LIMIT"
	FailureResourceLimit         FailureCode = "REMOTE_RESOURCE_LIMIT"
	FailureUnsupportedFileLayout FailureCode = "REMOTE_UNSUPPORTED_FILE_LAYOUT"
	FailureInvalidRequest        FailureCode = "REMOTE_INVALID_REQUEST"
	FailureFileNotFound          FailureCode = "REMOTE_FILE_NOT_FOUND"
	FailureFileConflict          FailureCode = "REMOTE_FILE_CONFLICT"
	FailureFileWrite             FailureCode = "REMOTE_FILE_WRITE_FAILED"
	FailureOutcomeUnknown        FailureCode = "REMOTE_OUTCOME_UNKNOWN"
	FailureGitUnavailable        FailureCode = "REMOTE_GIT_UNAVAILABLE"
	FailureGitConfigUnsafe       FailureCode = "REMOTE_GIT_CONFIG_UNSAFE"
)

type FailureReason string

const (
	ReasonAuthenticationRejected FailureReason = "authentication_rejected"
	ReasonKeyPassphraseRequired  FailureReason = "key_passphrase_required"
	ReasonHostKeyUnknown         FailureReason = "host_key_unknown"
	ReasonHostKeyChanged         FailureReason = "host_key_changed"
	ReasonHostKeyEvidence        FailureReason = "host_key_evidence"
	ReasonNameResolution         FailureReason = "name_resolution"
	ReasonConnectionRefused      FailureReason = "connection_refused"
	ReasonConnectionTimeout      FailureReason = "connection_timeout"
	ReasonConnectionClosed       FailureReason = "connection_closed"
	ReasonConnectionInProgress   FailureReason = "connection_in_progress"
	ReasonIdentityChanged        FailureReason = "identity_changed"
	ReasonDisconnected           FailureReason = "disconnected"
	ReasonConfig                 FailureReason = "config"
	ReasonCancelled              FailureReason = "cancelled"
	ReasonHostOutput             FailureReason = "host_output"
	ReasonOutputLimit            FailureReason = "output_limit"
	ReasonResourceLimit          FailureReason = "resource_limit"
	ReasonUnsupportedFileLayout  FailureReason = "unsupported_file_layout"
	ReasonInvalidRequest         FailureReason = "invalid_request"
	ReasonFileNotFound           FailureReason = "file_not_found"
	ReasonFileConflict           FailureReason = "file_conflict"
	ReasonFileWrite              FailureReason = "file_write"
	ReasonOutcomeUnknown         FailureReason = "outcome_unknown"
	ReasonGitUnavailable         FailureReason = "git_unavailable"
	ReasonGitConfigUnsafe        FailureReason = "git_config_unsafe"
	ReasonUnknown                FailureReason = "unknown"
)

// ConnectionFailure is safe to persist in bounded diagnostics. It contains no
// hostname, username, command, path, stderr fragment, or credential material.
type ConnectionFailure struct {
	Code   FailureCode
	Reason FailureReason
}

// ClassifyOpenSSHFailure maps untrusted OpenSSH stderr to stable, redacted
// failure codes. Classification is intentionally conservative; raw stderr is
// retained only by the caller's bounded in-memory tail and is never returned.
func ClassifyOpenSSHFailure(stderr []byte) ConnectionFailure {
	if len(stderr) > maxSSHFailureInputBytes {
		stderr = stderr[len(stderr)-maxSSHFailureInputBytes:]
	}
	if !utf8.Valid(stderr) {
		stderr = []byte(strings.ToValidUTF8(string(stderr), "\uFFFD"))
	}
	value := strings.ToLower(string(stderr))

	switch {
	case strings.Contains(value, "remote host identification has changed") ||
		(strings.Contains(value, "host key for ") && strings.Contains(value, " has changed and you have requested strict checking")) ||
		(strings.Contains(value, "offending ") && strings.Contains(value, " key in ")):
		return ConnectionFailure{Code: FailureHostKeyChanged, Reason: ReasonHostKeyChanged}
	case strings.Contains(value, "no ") && strings.Contains(value, " host key is known for"):
		return ConnectionFailure{Code: FailureHostKeyUnknown, Reason: ReasonHostKeyUnknown}
	case strings.Contains(value, "host key verification failed"):
		return ConnectionFailure{Code: FailureHostKeyUnknown, Reason: ReasonHostKeyUnknown}
	case containsAny(value,
		"incorrect passphrase supplied to decrypt private key",
		"can't open /dev/tty",
		"cannot read passphrase",
		"enter passphrase for key"):
		return ConnectionFailure{Code: FailureAuthRequired, Reason: ReasonKeyPassphraseRequired}
	case containsAny(value,
		"permission denied (",
		"no supported authentication methods available",
		"authentication failed",
		"sign_and_send_pubkey: signing failed"):
		return ConnectionFailure{Code: FailureAuthRequired, Reason: ReasonAuthenticationRejected}
	case containsAny(value,
		"could not resolve hostname",
		"name or service not known",
		"temporary failure in name resolution"):
		return ConnectionFailure{Code: FailureConnect, Reason: ReasonNameResolution}
	case containsAny(value,
		"connection refused",
		"no route to host",
		"network is unreachable"):
		return ConnectionFailure{Code: FailureConnect, Reason: ReasonConnectionRefused}
	case containsAny(value,
		"connection timed out",
		"operation timed out"):
		return ConnectionFailure{Code: FailureConnect, Reason: ReasonConnectionTimeout}
	case containsAny(value,
		"connection closed by",
		"connection reset by",
		"kex_exchange_identification"):
		return ConnectionFailure{Code: FailureConnect, Reason: ReasonConnectionClosed}
	case containsAny(value,
		"bad configuration option",
		"unsupported option",
		"no argument after keyword",
		"invalid setenv"):
		return ConnectionFailure{Code: FailureConnect, Reason: ReasonConfig}
	default:
		return ConnectionFailure{Code: FailureConnect, Reason: ReasonUnknown}
	}
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}
