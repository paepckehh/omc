// Package touch defines the contract between the signing backends and the
// UI for FIDO2 security-key (smartcard/yubikey) touch waits.
//
// Every signature that goes through the ssh-agent with a security-key
// identity blocks until the authenticator sees a physical touch (and
// possibly a PIN). OpenSSH and libfido2 wait for that touch indefinitely,
// so a client that wants to stay responsive has to wrap the wait itself.
// This package is the shared vocabulary for that wrapper: the sign package
// produces Events, the output package consumes them (structured records,
// animated countdown, desktop notification), and neither imports the
// other.
package touch

import "time"

// Event describes one security-key touch wait.
//
// A wait begins when a signature request is forwarded to the smartcard and
// ends when the request returns: either with a signature (touch observed),
// with a timeout, or with any other error. Algorithm is the sk-* signature
// algorithm in effect when known (e.g. "sk-ssh-ed25519@openssh.com"), so
// diagnostics can name exactly what is being signed with.
type Event struct {
	// KeyPath is the on-disk key handle that selected the agent identity
	// (e.g. ~/.ssh/id_ed25519_sk).
	KeyPath string
	// Action names the operation the signature authorises, e.g. "the
	// commit signing". Used for prompts and log records.
	Action string
	// Algorithm is the sk-* algorithm the signer produces, when known.
	Algorithm string
	// Timeout is the maximum wait the signer enforces for this event.
	// Zero means the wait is unbounded (no timeout).
	Timeout time.Duration
	// Err is set on TouchEnd: the outcome of the signature request. It is
	// nil on success (touch observed in time).
	Err error
	// TimedOut reports whether Err was caused by the enforced deadline
	// expiring (as opposed to a plain signing failure).
	TimedOut bool
}

// Hook receives the begin and end of every security-key touch wait. Both
// callbacks are invoked from the goroutine performing the signature, so
// implementations must not block; the UI implementation only renders
// records, animates, and posts desktop notifications.
type Hook interface {
	TouchBegin(e Event)
	TouchEnd(e Event)
}
