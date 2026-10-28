package assistant

// OutgoingReply is the durable delivery record, not a Telegram API object.
type OutgoingReply struct {
	UpdateID, ChatID int64
	Text             string
	Attempts         int
}
