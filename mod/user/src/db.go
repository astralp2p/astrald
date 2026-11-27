package user

import "gorm.io/gorm"

type DB struct {
	mod *Module
	*gorm.DB
}
