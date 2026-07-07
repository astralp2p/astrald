package user

import "github.com/cryptopunkscc/astral-go/astral"

// AddAsset adds an object to user's assets
func (mod *Module) AddAsset(objectID *astral.ObjectID) (err error) {
	_, err = mod.db.AddAsset(objectID, false)
	if err == nil {
		mod.notifySiblings("assets")
	}
	return
}

// RemoveAsset removes an object from user's assets
func (mod *Module) RemoveAsset(objectID *astral.ObjectID) (err error) {
	err = mod.db.RemoveAsset(objectID)
	if err == nil {
		mod.notifySiblings("assets")
	}
	return err
}

// AssetsContain returns true if user's assets contain the object
func (mod *Module) AssetsContain(objectID *astral.ObjectID) bool {
	return mod.db.assetExists(objectID)
}

// Assets returns all object IDs in the user's asset set that have not been removed.
func (mod *Module) Assets() []*astral.ObjectID {
	assets, err := mod.db.Assets()
	if err != nil {
		mod.log.Error("error getting assets: %v", err)
	}

	return assets
}
