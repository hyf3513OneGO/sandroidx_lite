package models

import (
	"fmt"
	"log"

	"sandroidx.com/sandroidx_lite/configs"

	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

func InitDB() error {
	var err error
	var dialector gorm.Dialector

	dbType := configs.AppConfig.Database.Type
	dsn := configs.GetDSN()

	switch dbType {
	case "sqlite":
		dialector = sqlite.Open(dsn)
	case "mysql":
		dialector = mysql.Open(dsn)
	default:
		return fmt.Errorf("不支持的数据库类型: %s", dbType)
	}

	DB, err = gorm.Open(dialector, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return fmt.Errorf("数据库连接失败: %w", err)
	}

	log.Printf("数据库连接成功，类型: %s", dbType)
	return nil
}

func AutoMigrate(models ...interface{}) error {
	return DB.AutoMigrate(models...)
}
