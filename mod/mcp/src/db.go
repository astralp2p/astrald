package mcp

import (
	"time"

	mcpapi "github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
	mcpmod "github.com/astralp2p/astrald/mod/mcp"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type DB struct {
	*gorm.DB
}

// MigrateAgents brings the agent table to the current schema.
//
// why the old columns are dropped rather than left: the node holds no
// reachability, so a column that once carried it answers nothing and a reader
// finding one would take it for a decision something still makes.
func (db *DB) MigrateAgents() error {
	m := db.Migrator()

	if m.HasTable(&dbAgent{}) {
		for _, column := range []string{"visible", "exposed"} {
			if !m.HasColumn(&dbAgent{}, column) {
				continue
			}
			if err := m.DropColumn(&dbAgent{}, column); err != nil {
				return err
			}
		}
	}

	return db.AutoMigrate(&dbAgent{})
}

// CreateAgent inserts the agent row and stamps its creation time. The caller
// builds the row, so visibility lands in the same write as the rest of the record.
func (db *DB) CreateAgent(row *dbAgent) error {
	row.CreatedAt = time.Now()
	return db.Create(row).Error
}

func (db *DB) FindAgent(identity *astral.Identity) (row *dbAgent, err error) {
	err = db.Where("identity = ?", identity).First(&row).Error
	return
}

func (db *DB) DeleteAgent(identity *astral.Identity) error {
	tx := db.Where("identity = ?", identity).Delete(&dbAgent{})
	if tx.Error != nil {
		return tx.Error
	}
	if tx.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (db *DB) ListAgents() (list []dbAgent, _ error) {
	return list, db.Find(&list).Error
}

// dbMessage is one message in a recipient's inbox. The row is the whole of the
// message's state: delivered_at is stamped on arrival, read_at when the
// recipient claims or opens it, and nothing else changes.
type dbMessage struct {
	// note: the id is the sender's, minted before delivery, so a delivery that
	// arrives twice collides here and is stored once.
	//
	// fixme: the key is the id alone, so one id space spans every inbox on the
	// node. A sender re-using an id across two recipients loses the second
	// delivery. The key an inbox needs is (recipient, id).
	ID          mcpapi.MessageID `gorm:"primaryKey"`
	Sender      *astral.Identity `gorm:"index"`
	Recipient   *astral.Identity `gorm:"index:idx_mcp_messages_inbox,priority:1"`
	Content     string
	DeliveredAt time.Time `gorm:"index:idx_mcp_messages_inbox,priority:2"`
	ReadAt      *time.Time
}

func (dbMessage) TableName() string {
	return mcpmod.DBPrefix + "messages"
}

// MigrateMessages brings the message table to the current schema.
func (db *DB) MigrateMessages() error {
	return db.AutoMigrate(&dbMessage{})
}

// InsertMessage stores a delivered message and stamps its arrival. A message
// whose id is already stored is left as it stands, so a sender that retries
// after a lost acknowledgement delivers once.
func (db *DB) InsertMessage(row *dbMessage) error {
	row.DeliveredAt = time.Now().UTC()
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(row).Error
}

// ListInbox returns the messages addressed to the recipient, oldest first.
func (db *DB) ListInbox(recipient *astral.Identity, unreadOnly bool, limit int) (list []dbMessage, _ error) {
	tx := db.Where("recipient = ?", recipient)
	if unreadOnly {
		tx = tx.Where("read_at IS NULL")
	}
	return list, tx.Order("delivered_at").Limit(limit).Find(&list).Error
}

// ReadMessage returns one message addressed to the recipient and stamps it
// read. A message already read is returned as it stands: reading is not a
// claim, and the stamp records the first read.
// Returns gorm.ErrRecordNotFound when the recipient holds no such message.
func (db *DB) ReadMessage(recipient *astral.Identity, id mcpapi.MessageID) (*dbMessage, error) {
	var row dbMessage

	err := db.Where("recipient = ? AND id = ?", recipient, id).First(&row).Error
	if err != nil {
		return nil, err
	}

	if row.ReadAt == nil {
		now := time.Now().UTC()
		if err = db.Model(&dbMessage{}).Where("id = ?", id).Update("read_at", now).Error; err != nil {
			return nil, err
		}
		row.ReadAt = &now
	}

	return &row, nil
}

// ClaimNext stamps the oldest unread message addressed to the recipient and
// returns it. Returns gorm.ErrRecordNotFound when the inbox holds none.
//
// why the update names read_at again: two claims can select the same row, and
// the update is where one of them loses. A claim that changes no row starts
// over and takes the next message rather than the one it lost.
func (db *DB) ClaimNext(recipient *astral.Identity) (*dbMessage, error) {
	for {
		var row dbMessage

		err := db.
			Where("recipient = ? AND read_at IS NULL", recipient).
			Order("delivered_at").
			First(&row).Error
		if err != nil {
			return nil, err
		}

		now := time.Now().UTC()

		tx := db.Model(&dbMessage{}).
			Where("id = ? AND read_at IS NULL", row.ID).
			Update("read_at", now)
		if tx.Error != nil {
			return nil, tx.Error
		}
		if tx.RowsAffected == 0 {
			continue
		}

		row.ReadAt = &now
		return &row, nil
	}
}
