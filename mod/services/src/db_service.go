package services

import (
	"time"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/services"
)

// dbService represents discovered service of a specific identity cached in the database
type dbService struct {
	Name       string           `gorm:"uniqueIndex:idx_db_service_name_provider_id"`
	ProviderID *astral.Identity `gorm:"uniqueIndex:idx_db_service_name_provider_id"`
	Info       *astral.Bundle   `gorm:"serializer:json"`
	CreatedAt  time.Time
}

func (dbService) TableName() string {
	return services.DBPrefix + "services"
}
