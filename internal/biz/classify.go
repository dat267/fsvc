package biz

import "time"

// Category is the bucket a ticket falls into.
type Category int

const (
	// CategoryNone means the ticket needs no action in this classification.
	CategoryNone Category = iota
	// CategoryUnassigned means the ticket has no responder.
	CategoryUnassigned
	// CategoryStaleAgent means the agent's last message is older than the
	// threshold and the ticket is waiting on the customer.
	CategoryStaleAgent
	// CategoryCustomer means the customer's last message needs an agent reply.
	CategoryCustomer
)

// Classify decides the category of a ticket.
//
// responderID is the ticket's responder_id (nil/0 means unassigned per the
// private API convention where -1 is unassigned and 0 is self). lastMsg is the
// timestamp of the latest conversation message (zero when there is none), and
// incoming reports whether that message was from the customer. created is the
// ticket creation time, used as the reference when there are no messages.
func Classify(responderID *int64, lastMsg time.Time, incoming bool, created time.Time, threshold time.Time) Category {
	if responderID == nil || *responderID < 0 {
		return CategoryUnassigned
	}

	ref := lastMsg
	if ref.IsZero() {
		ref = created
	}

	if incoming {
		return CategoryCustomer
	}
	if ref.Before(threshold) {
		return CategoryStaleAgent
	}
	return CategoryNone
}
