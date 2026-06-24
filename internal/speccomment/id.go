package speccomment

import (
	"crypto/rand"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// idMu serializes the monotonic entropy source: ulid.MonotonicEntropy is not
// safe for concurrent use, and the coordinator mints ids from several
// connection goroutines.
var (
	idMu      sync.Mutex
	idEntropy = ulid.Monotonic(rand.Reader, 0)
)

// NewID mints a coordinator-stable, lexicographically sortable ULID. Ids are
// minted only by the coordinator (the authoritative side) and are stable across
// an NDJSON export with no remapping, the property the future git-export path
// depends on.
func NewID() string {
	idMu.Lock()
	defer idMu.Unlock()
	return ulid.MustNew(ulid.Timestamp(time.Now()), idEntropy).String()
}
