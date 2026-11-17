package vfs

import (
	"github.com/astralp2p/astral-go/astral"
)

// gainContent counts content a file of the VFS is about to hold. The size counts when no file holds it yet.
//
// note: call it before the row holds the content, so the row does not count as a holder.
func (c *change) gainContent(id *astral.ObjectID) error {
	held, err := c.tx.holdsContent(c.vol.Identity, id)
	if err != nil || held {
		return err
	}

	return c.tx.addUsage(c.vol.Identity, int64(id.Size), 0)
}

// dropContent uncounts content a file of the VFS no longer holds. The size is uncounted when no file holds it any
// more.
//
// note: call it after the row dropped the content.
func (c *change) dropContent(id *astral.ObjectID) error {
	held, err := c.tx.holdsContent(c.vol.Identity, id)
	if err != nil || held {
		return err
	}

	return c.tx.addUsage(c.vol.Identity, -int64(id.Size), 0)
}

// deleteSubtree deletes a row and every row below it, and uncounts its rows and the content only it held.
//
// note: the cost grows with the subtree, not with the VFS.
func (c *change) deleteSubtree(rowID int64) error {
	rows, ids, err := c.tx.findSubtreeContent(rowID)
	if err != nil {
		return err
	}

	if err = c.tx.deleteSubtree(rowID, c.now); err != nil {
		return err
	}

	if err = c.tx.addUsage(c.vol.Identity, 0, -rows); err != nil {
		return err
	}

	for _, id := range ids {
		if err = c.dropContent(id); err != nil {
			return err
		}
	}

	return nil
}
