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
