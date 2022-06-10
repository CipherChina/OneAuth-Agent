package main

import (
	log "github.com/sirupsen/logrus"
)

// 配置文件相关数据结构
type LogInfo struct {
	Level string `yaml: "level"`
	Path  string `yaml: "path"`
}

type SystemConfig struct {
	Log   LogInfo `yaml: "log"`
	Fiber string  `yaml: "fiber"`
}

type DatabaseUser struct {
	Appkey    string `yaml: "appkey"`
	Appsecret string `yaml: "appsecret"`
	Sign      string
}

type FilterInfo struct {
	Unitcode []string `yaml: "unitcode"`
	Unitname []string `yaml: "unitname"`
	Filter   map[string]string
}

// 数据库端相关配置
type DataBase struct {
	Host        string       `yaml: "host"`
	Port        string       `yaml: "port"`
	User        DatabaseUser `yaml: "user"`
	DefaultTree string       `yaml: "defaulttree"`
	ReadTime    string       `yaml: "readtime"`
	SyncOu      string       `yaml: "syncou"`
	Filter      FilterInfo   `yaml: "filter"`
	// 获取组织架构接口
	OrgInterface string
	// 获取人员接口
	MemberInterface string
}

type UpstreamConfig struct {
	Ssl  string `yaml: "ssl"`
	Host string `yaml: "host"`
	Port string `yaml: "port"`
	// ssl 配置转换，默认true
	Tls bool
}

type OneAuthConfig struct {
	Token    string         `yaml: "token"`
	Upstream UpstreamConfig `yaml: "upstream"`
	RootName string         `yaml: "rootname"`
	BaseUrl  string
}

// 配置文件数据存储结构
type Config struct {
	System   SystemConfig  `yaml:"system"`   // 系统相关配置
	Oneauth  OneAuthConfig `yaml:"oneauth"`  // OneAuth相关配置项
	Database DataBase      `yaml:"database"` // 同步数据库的相关配置项
}

func ConfigCheck() bool {
	if len(GlobalConfig.Oneauth.Token) == 0 {
		log.Error("[config] Oneauth token must be set")
		return false
	}

	if len(GlobalConfig.Oneauth.Upstream.Host) == 0 || len(GlobalConfig.Oneauth.Upstream.Port) == 0 {
		log.Error("[config] Oneauth upsteam host and port must be set")
		return false
	}

	if len(GlobalConfig.Oneauth.RootName) == 0 {
		log.Error("[config] Oneauth rootname must be set")
		return false
	}

	if len(GlobalConfig.Database.Host) == 0 || len(GlobalConfig.Database.Port) == 0 {
		log.Error("[config] Database host and port must be set")
		return false
	}

	if len(GlobalConfig.Database.DefaultTree) == 0 {
		log.Error("[config] Database defaulttree must be set")
		return false
	}

	if len(GlobalConfig.Database.User.Appkey) == 0 || len(GlobalConfig.Database.User.Appsecret) == 0 {
		log.Error("[config] Database user appkey and appsecret must be set")
		return false
	}

	return true
}
