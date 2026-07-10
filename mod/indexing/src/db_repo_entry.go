package indexing

import (
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/indexing"
)

// dbRepoEntry is one row in a repo's append-only changelog.
// Each add/remove writes a new row; Version is monotonic per repo.
type dbRepoEntry struct {
	Repo      string           `gorm:"primaryKey;uniqueIndex:idx_repo_version"`
	Version   uint64           `gorm:"primaryKey;uniqueIndex:idx_repo_version"`
	ObjectID  *astral.ObjectID `gorm:"index"`
	Exist     bool
	CreatedAt time.Time
}

func (dbRepoEntry) TableName() string { return indexing.DBPrefix + "repo_entries" }
