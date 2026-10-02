package lens

import "github.com/terva-sh/ketju/internal/store/threadlog"

// odd keeps every odd ordinal: the framework's own filter, which no kind
// serves.
type odd struct{}

func (odd) keep(o uint32, _ threadlog.IndexEntry) bool { return o%2 == 1 }

// Odd is the test filter.
var Odd Filter = odd{}

// PastLast is the reply chain's bound check, threadlog's since
// TKT-01M3VVZE4G.
var PastLast = threadlog.PastLast
