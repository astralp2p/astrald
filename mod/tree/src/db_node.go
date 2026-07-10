package tree

import (
	"time"

	"github.com/astralp2p/astrald/mod/tree"
)

type dbNode struct {
	ID        int    `gorm:"primarykey"`
	ParentID  int    `gorm:"uniqueIndex:idx_tree__node_parent_name;not null"`
	Name      string `gorm:"uniqueIndex:idx_tree__node_parent_name;not null"`
	Type      string `gorm:"index;not null"`
	Payload   []byte
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (dbNode) TableName() string { return tree.DBPrefix + "nodes" }
