package dto

// The moderator queue's shapes (TKT-01M29CRJ4E), as a moderator sees them.
// Registered into the shared corpus, as admin_fixtures_test.go's are.

func init() {
	corpus["queue."+tModerator] = queuePage()
	corpus["queue-resolution."+tModerator] = QueueResolution{Action: "dismiss", Post: QueuePostRestored, Flags: []int64{7, 8, 9}}
}

// queuePage is one page with both kinds: a post hidden pending review with
// three flags, newest first, one of them "other" with its note; a pending
// application; and a post whose thread cannot be read just now, so its
// post is null. More items follow.
func queuePage() Queue {
	hidden := singlePost(tModerator)
	hidden.Deleted = true
	hidden.Removal = ptr(RemovalReview)
	return Queue{
		Items: []QueueItem{
			{Kind: QueueKindFlag, CreatedAt: 1757610000000, Flag: &QueueFlag{
				Thread:  QueueThread{ID: "3yymw47g00001", Slug: "the-dinner-bill", Title: "The dinner bill"},
				Ordinal: 63,
				Post:    &hidden,
				Flags: []QueueReport{
					{ID: 9, Reason: "other", Note: "Posts our home address", Reporter: alice, CreatedAt: 1757610000000},
					{ID: 8, Reason: "abuse", Note: "", Reporter: scribe, CreatedAt: 1757609000000},
					{ID: 7, Reason: "spam", Note: "", Reporter: bob, CreatedAt: 1757608000000},
				},
			}},
			{Kind: QueueKindApplication, CreatedAt: 1757600000000, Application: &carolPending},
			{Kind: QueueKindFlag, CreatedAt: 1757590000000, Flag: &QueueFlag{
				Thread:  QueueThread{ID: "3yymw47g00002", Slug: "quarantined", Title: "Quarantined"},
				Ordinal: 2,
				Post:    nil,
				Flags:   []QueueReport{{ID: 5, Reason: "off-topic", Note: "", Reporter: alice, CreatedAt: 1757590000000}},
			}},
		},
		NextCursor: "MTc1NzU5MDAwMDAwMDoxOjE6Mg",
	}
}
