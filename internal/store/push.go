package store

import "time"

// MessagesAfterRow returns the stored messages with a row id above after,
// oldest row first, and the row id of the last one. Row order is storage
// order, so a reader that pages by it keeps its place while a sync adds
// messages. Deleted and revoked messages are in it too: a reader that copied
// them earlier has to hear that they are gone.
func (d *DB) MessagesAfterRow(after int64, limit int) ([]Message, int64, error) {
	msgs, err := d.scanMessages(`SELECT `+d.messageSelectColumns("")+`
		FROM messages m
		LEFT JOIN chats c ON c.jid = m.chat_jid
		LEFT JOIN starred s ON s.chat_jid = m.chat_jid AND s.msg_id = m.msg_id
		WHERE m.rowid > ?
		ORDER BY m.rowid LIMIT ?`, after, limit)
	if err != nil || len(msgs) == 0 {
		return nil, after, err
	}
	return msgs, msgs[len(msgs)-1].rowID, nil
}

// CallEventsAfterRow returns the call events stored after row id after, plus
// every event timed after since: an event's outcome and duration can be filled
// in after it was first stored, and call events have no change journal. Oldest
// first, with the highest row id seen.
func (d *DB) CallEventsAfterRow(after int64, since time.Time) ([]CallEvent, int64, error) {
	calls, err := d.scanCallEvents(callEventSelect+`
		WHERE rowid > ? OR ts > ?
		ORDER BY ts ASC, rowid ASC`, after, unix(since))
	if err != nil {
		return nil, after, err
	}
	last := after
	for _, c := range calls {
		last = max(last, c.rowID)
	}
	return calls, last, nil
}
