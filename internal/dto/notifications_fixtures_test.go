package dto

// The notification list (TKT-01M29CRJ2T), registered into the corpus from
// here so the shared fixture file is not edited by every list.
func init() {
	corpus["notifications."+tMember] = notifications(tMember)
	corpus["notifications."+tModerator] = notifications(tModerator)
	corpus["notifications-read."+tMember] = NotificationsRead{Marked: 3}
}

// notifications is GET /notifications: a mention, a watched thread's new
// post already read, and the member's approval, newest written first. The
// moderator's page opens with a new_application whose application has been
// decided, served as read. No row carries post text.
func notifications(class string) NotificationList {
	thread := &ThreadLink{ID: "3yymw47g00001", Slug: "why-we-left-slack", Title: "Why we left Slack"}
	forum := &ForumRef{Slug: "general", Name: "General"}
	out := NotificationList{Notifications: []Notification{
		{ID: 41, Kind: "mention", Thread: thread, Forum: forum, Ordinal: ptr(uint32(6)), Actor: &alice,
			At: 1757500600000},
		{ID: 37, Kind: "thread_you_watch", Thread: thread, Forum: forum, Ordinal: ptr(uint32(4)), Actor: &bob,
			At: 1757500400000, Read: true},
		{ID: 12, Kind: "application_decided", Actor: &bob, At: 1757400000000, Read: true},
	}, NextCursor: "MTI"}
	if class == tModerator {
		out.Notifications = append([]Notification{{ID: 44, Kind: "new_application", At: 1757500700000, Read: true}},
			out.Notifications...)
	}
	return out
}
