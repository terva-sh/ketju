package domain

// Notification kinds (docs/04 "notifications"). The notifications table's
// CHECK constraint is the same list, and a test holds the two together.
const (
	NotifyReplyToYou         = "reply_to_you"
	NotifyMention            = "mention"
	NotifyQuoted             = "quoted"
	NotifyThreadYouWatch     = "thread_you_watch"
	NotifyApplicationDecided = "application_decided"
	NotifyNewApplication     = "new_application"
)

// NotificationKinds is the closed set.
var NotificationKinds = []string{NotifyReplyToYou, NotifyMention, NotifyQuoted,
	NotifyThreadYouWatch, NotifyApplicationDecided, NotifyNewApplication}

// Watch levels (docs/04 "watches").
const (
	WatchWatch = "watch"
	WatchMute  = "mute"
)

// WatchNone is no row: a thread with it follows its forum, and a forum with
// it adds nothing.
const WatchNone = ""

// ValidWatchLevel reports whether level is one a member may set: watch,
// mute, or none, which clears the row.
func ValidWatchLevel(level string) bool {
	switch level {
	case WatchNone, WatchWatch, WatchMute:
		return true
	}
	return false
}

// EffectiveWatch is a thread's level for a member (docs/04 "watches"): the
// thread's own row when it has one, then the row for the forum the thread is
// in now, then none. The more specific setting wins, so a muted thread in a
// watched forum is muted and a watched thread in a muted forum is watched.
// A forum's row covers its own threads, not its children's, so forum is the
// thread's own forum's level and never a parent's.
//
// SQL states the same rule as a coalesce of the thread's level, the forum's
// and the empty string, and TestEffectiveWatchSQLAgrees holds the two
// together.
func EffectiveWatch(thread, forum string) string {
	if thread != WatchNone {
		return thread
	}
	return forum
}
