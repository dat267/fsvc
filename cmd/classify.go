package cmd

import "time"

// Category is the bucket a ticket falls into.
type Category int

const (
	// CategoryNone means the ticket needs no action in this classification.
	CategoryNone Category = iota
	// CategoryUnassigned means the ticket has no responder.
	CategoryUnassigned
	// CategoryStaleAgent means the responder has done their part and the ticket
	// is waiting on the customer: the last message is from the responder (or the
	// ticket has no messages at all) and there has been no reply for
	// olderThanDays business days.
	CategoryStaleAgent
	// CategoryCustomer means the last message came from someone other than the
	// responder and needs an agent reply.
	CategoryCustomer
)

// Classify decides the category of a ticket.
//
// responderID is the ticket's responder_id (nil/0 means unassigned per the
// private API convention where -1 is unassigned and 0 is self). lastMsg is the
// timestamp of the latest conversation message (zero when there is none), and
// lastUserID is the author of that message. created is the ticket creation
// time, used as the reference when there are no messages. A last message from
// anyone other than the responder puts the ticket in the customer-waiting
// bucket; otherwise it is waiting-on-customer when the elapsed business days
// since the last message (or creation, for no-message tickets) exceed
// olderThanDays.
func Classify(responderID *int64, lastMsg time.Time, lastUserID int64, created time.Time, olderThanDays float64, now time.Time) Category {
	if responderID == nil || *responderID < 0 {
		return CategoryUnassigned
	}

	if !lastMsg.IsZero() && lastUserID != *responderID {
		return CategoryCustomer
	}

	ref := lastMsg
	if ref.IsZero() {
		ref = created
	}
	if BusinessDaysBetween(ref, now) > olderThanDays {
		return CategoryStaleAgent
	}
	return CategoryNone
}
