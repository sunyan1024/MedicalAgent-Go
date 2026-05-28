package mysql

import (
	"GoAI/config"
	"GoAI/model"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// 全局数据库连接池
var DB *gorm.DB

// 数据库初始化
func InitMysql() error {
	// 读取配置文件
	conf := config.GetConfig()
	host := conf.MysqlConfig.MysqlHost
	port := conf.MysqlConfig.MysqlPort
	dbname := conf.MysqlConfig.MysqlDatabaseName
	username := conf.MysqlConfig.MysqlUser
	password := conf.MysqlConfig.MysqlPassword
	charset := conf.MysqlConfig.MysqlCharset

	// 使用fmt.Sprintf构建MySQL连接字符串(DSN)
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=true&loc=Local", username, password, host, port, dbname, charset)

	// 配置 GORM 日志
	// 根据 Gin 的运行环境(debug/release)配置不同的日志级别
	var log logger.Interface
	if gin.Mode() == "debug" {
		log = logger.Default.LogMode(logger.Info)
	} else {
		log = logger.Default
	}

	// 用 GORM 打开 MySQL 连接
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       dsn,   // 数据库连接字符串
		DefaultStringSize:         256,   // 默认字符串大小
		DisableDatetimePrecision:  true,  // 禁用日期时间精度
		DontSupportRenameIndex:    true,  // 不支持重命名索引
		DontSupportRenameColumn:   true,  // 不支持重命名列
		SkipInitializeWithVersion: false, // 跳过版本初始化
	}), &gorm.Config{
		Logger: log,
	})
	if err != nil {
		return err
	}

	// 连接池配置
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	sqlDB.SetMaxIdleConns(10)           // 最大空闲连接
	sqlDB.SetMaxOpenConns(100)          // 最大打开连接
	sqlDB.SetConnMaxLifetime(time.Hour) // 连接最大生命周期

	// 将连接对象赋值给全局变量DB
	DB = db

	return migration() // 自动迁移数据库表结构
}

// 自动数据迁移
func migration() error {
	return DB.AutoMigrate( // 自动迁移以下模型
		new(model.User),
		new(model.Session),
		new(model.Message),
		new(model.KnowledgeFile),
	)
}
