package tree

import (
	"bytes"
	"fmt"

	"github.com/astralp2p/astral-go/astral"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type DB struct {
	*gorm.DB
}

func (db *DB) createNode(parentID int, name string) (row *dbNode, err error) {
	row = &dbNode{ParentID: parentID, Name: name}
	err = db.Create(row).Error
	return
}

func (db *DB) setNodeValue(nodeID int, object astral.Object) (err error) {
	var payload = &bytes.Buffer{}
	_, err = object.WriteTo(payload)
	if err != nil {
		return err
	}

	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"payload", "type"}),
	}).Create(&dbNode{
		ID:      nodeID,
		Type:    object.ObjectType(),
		Payload: payload.Bytes(),
	}).Error
}

func (db *DB) getNodeValue(nodeID int, allowUnparsed bool) (object astral.Object, err error) {
	var row dbNode
	err = db.First(&row, "id = ?", nodeID).Error
	if err != nil {
		return nil, err
	}

	if len(row.Type) == 0 {
		return nil, nil
	}

	object = astral.New(row.Type)
	if object == nil {
		if allowUnparsed {
			return astral.NewUnparsedObject(row.Type, row.Payload), nil
		}
		return nil, fmt.Errorf("%w: %s", astral.ErrBlueprintNotFound, row.Type)
	}

	_, err = object.ReadFrom(bytes.NewReader(row.Payload))
	if err != nil {
		return nil, err
	}

	return
}

func (db *DB) deleteNode(nodeID int) error {
	return db.Delete(&dbNode{}, "id = ?", nodeID).Error
}

func (db *DB) getSubNodes(parentID int) ([]dbNode, error) {
	var rows []dbNode
	err := db.Find(&rows, "parent_id = ?", parentID).Error
	return rows, err
}
