package db

import (
	"github.com/pkg/errors"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type DB struct {
	db    *gorm.DB
	oldDB *gorm.DB
}

func New(dsn, oldDSN string) (*DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to connect postgres")
	}
	if err := db.AutoMigrate(
		&scannedBlockNumber{},
		&Account{},
		&App{},
		&AppV2{},
		&Bank{},
		&BankRecord{},
		&Device{},
		&DeviceRecord{},
		&Task{},
		&Message{},
	); err != nil {
		return nil, errors.Wrap(err, "failed to migrate model")
	}
	oldDB, err := gorm.Open(postgres.Open(oldDSN), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to connect old postgres")
	}
	return &DB{
		db:    db,
		oldDB: oldDB,
	}, nil
}
