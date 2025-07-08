package client

import (
	"github.com/komari-monitor/komari/api"
	"github.com/komari-monitor/komari/common"
	"github.com/patrickmn/go-cache"
)

// SaveClientReport 保存客户端报告到缓存
// 该函数被其他模块使用，保留
func SaveClientReport(uuid string, report common.Report) error {
	reports, _ := api.Records.Get(uuid)
	if reports == nil {
		reports = []common.Report{}
	}
	if report.CPU.Usage < 0.01 {
		report.CPU.Usage = 0.01
	}
	reports = append(reports.([]common.Report), report)
	api.Records.Set(uuid, reports, cache.DefaultExpiration)

	return nil
}
