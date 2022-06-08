package main

import (
	"strconv"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

// 将时间转化为秒数，基准为当天0点0分0秒的时间差
func TimeToSec(timeStr string) (sec time.Duration) {
	if len(timeStr) == 0 {
		return 0
	}

	var timeArr = strings.Split(timeStr, ":")
	var hour, _ = strconv.Atoi(timeArr[0])
	var minute, _ = strconv.Atoi(timeArr[1])
	var second, _ = strconv.Atoi(timeArr[2])

	return time.Duration(hour*3600 + minute*60 + second)
}

// 设置每天定时执行的定时器
func InitTimer(f func(), sec time.Duration) {
	go func() {
		for {
			// 执行定时任务
			f()

			// 设置定时器
			now := time.Now()
			// 计算下一个零点
			next := now.Add(time.Hour * 24)
			next = time.Date(next.Year(), next.Month(), next.Day(), 0, 0, 0, 0, next.Location())
			// 计算下一次触发的时间点
			next = next.Add(time.Second * sec)

			log.Info("timer: ", next.Sub(now), ", sec: ", sec)
			t := time.NewTimer(next.Sub(now))
			<-t.C
		}
	}()
}
