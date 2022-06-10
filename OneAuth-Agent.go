package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	rotatelogs "github.com/lestrrat-go/file-rotatelogs"
	log "github.com/sirupsen/logrus"
	yaml "gopkg.in/yaml.v2"
)

type DataOrgMemNode struct {
	NodeCode string
	NodeName string
	OuName   string
	Children map[string]*DataOrgMemNode
	parent   *DataOrgMemNode
	Value    *DataApiOrgNode
	//Members  map[string]*PersonNode

	Root        bool   // 是否是根节点
	OrgId       string // Oneauth根节点id
	DepId       string // oneauth部门id
	FatherId    string // oneauth新父级部门id，用于部门创建和移动
	DiffCompare bool   // 用于新老数据对比，给Bak数据使用
	Action      int    // 操作类型，1 << 0创建，1 << 1更新名字，1 << 2移动，6更新+移动，1 << 3删除
}

var GlobalConfig Config

/*
func GetPersonMd5(newPerson PersonNode) string {
	md5Str := newPerson.DN + newPerson.Person.Name + newPerson.Person.SAMAccountName
	md5Str += newPerson.Person.UserPrincipalName + newPerson.Person.Department + newPerson.Person.Manager
	md5Str += strconv.Itoa(newPerson.Person.Status)
	return fmt.Sprintf("%x", md5.Sum([]byte(md5Str)))
}
*/

// 判断目录是否存在
func PathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// 解析配置文件
func InitConfig(fileConf string) bool {
	// 默认开启tls
	GlobalConfig.Oneauth.Upstream.Tls = true
	GlobalConfig.System.Log.Level = "4"
	GlobalConfig.System.Log.Path = "log/OneAuth.log"
	GlobalConfig.System.Fiber = "10"

	// 检查log目录是否存在
	if ok, _ := PathExists("log"); !ok {
		fmt.Println("Init..., Create log dir.")
		os.Mkdir("log", 777)
	}

	// 读取配置文件内容
	data, err := ioutil.ReadFile(fileConf)
	if err != nil {
		fmt.Println("Ldap read config file error: ", err)
		return false
	}

	if err = yaml.Unmarshal(data, &GlobalConfig); err != nil {
		fmt.Println("Ldap yaml config parse error: ", err)
		return false
	}

	if GlobalConfig.Oneauth.Upstream.Ssl == "false" {
		GlobalConfig.Oneauth.Upstream.Tls = false
	}

	// 初始化日志相关配置
	level, _ := strconv.Atoi(GlobalConfig.System.Log.Level)
	InitLog(GlobalConfig.System.Log.Path, level)

	// 解析目录过滤
	GlobalConfig.Database.Filter.Filter = make(map[string]string)
	for _, key := range GlobalConfig.Database.Filter.Unitcode {
		GlobalConfig.Database.Filter.Filter[key] = "1"
	}
	for _, key := range GlobalConfig.Database.Filter.Unitname {
		GlobalConfig.Database.Filter.Filter[key] = "1"
	}

	// 初始化主数据相关接口
	var BaseUrl = "https://" + GlobalConfig.Database.Host + ":" + GlobalConfig.Database.Port
	GlobalConfig.Database.OrgInterface = BaseUrl + OrgUrl + GlobalConfig.Database.User.Appkey
	GlobalConfig.Database.MemberInterface = BaseUrl + MemberUrl + GlobalConfig.Database.User.Appkey

	// 初始化oneauth
	InitUpstreamBaseUrl()

	// 配置文件检查
	return ConfigCheck()
}

func InitLog(pathlog string, level int) {
	write, _ := rotatelogs.New(
		pathlog+".%Y%m%d%H",
		rotatelogs.WithLinkName(pathlog),
		rotatelogs.WithMaxAge(time.Duration(24)*time.Hour),
		rotatelogs.WithRotationTime(time.Duration(1)*time.Hour),
	)

	log.SetOutput(write)
	log.SetLevel(log.Level(level))

	customFormat := new(log.TextFormatter)
	customFormat.TimestampFormat = "2006-01-02 15:04:05.000000"
	log.SetFormatter(customFormat)
	log.Info("[log] Init success")
}

/*


func updateCurrentOrgData(ou, basedn string, dataOrg *DataOrgMemNode) {
	//log.Info("current ou: ", basedn)
	currentOrg, _ := SearchCurrentOUAll(ou, basedn)
	// 目录不存在
	if currentOrg == nil {
		log.Info("[service] ou unexist: ", basedn)
		AddNewOU(basedn, dataOrg.OuName)
		AddOuAllData(basedn, dataOrg)
		return
	}

	// 目录下没有子目录或目录不存在
	if currentOrg.Children == nil {
		AddOuAllData(basedn, dataOrg)
	} else {
		// 更新目录
		// TODO: 使用协程解决并发速度问题
		//var wg sync.WaitGroup
		for _, node := range dataOrg.Children {
			//wg.Add(1)
			//go func() {
			if _, ok := currentOrg.Children[node.OuName]; ok {
				baseStr := "OU=" + node.OuName + "," + basedn
				updateCurrentOrgData(node.OuName, baseStr, node)
				delete(currentOrg.Children, node.OuName)
			} else {
				baseStr := "OU=" + node.OuName + "," + basedn
				log.Info("add ou dn: ", baseStr, ", ou: "+ou+", basedn: "+basedn)
				AddNewOU(baseStr, node.OuName)
				AddOuAllData(baseStr, node)
			}
			//      wg.Done()
			//}()
		}
		//wg.Wait()

		// 删除目录
		for _, value := range currentOrg.Children {
			baseStr := "OU=" + value.OuName + "," + basedn
			log.Info("[service] delete ou: ", baseStr)
			DelOUNode(baseStr)
		}
	}
}

*/

func CompareDataBakAndCreateOrgTask(orgQueue *Queue) (*Queue, *Queue, *Queue) {
	// 需要新建org的任务队列
	taskNewQueue := new(Queue)
	// 需要更新org的任务队列
	taskUpdateQueue := new(Queue)
	// 需要删除org的任务队列
	taskDelQueue := new(Queue)

	var orgId string
	for {
		if orgQueue.Len() <= 0 {
			break
		}

		orgNode := orgQueue.Pop().(*DataOrgMemNode)
		if node, ok := DataBaseOrgMapBak[orgNode.NodeCode]; ok {
			// 根节点不需要做更新判断
			if orgNode.Root == true {
				orgId = node.OrgId
				node.DiffCompare = true
				continue
			}

			// 先填充原来的oneauth相关信息
			orgNode.OrgId = node.OrgId
			orgNode.DepId = node.DepId
			orgNode.FatherId = node.FatherId

			if orgNode.NodeName != node.NodeName || orgNode.parent.NodeCode != node.parent.NodeCode {
				// 更新名字
				if orgNode.NodeName != node.NodeName {
					orgNode.Action = 1 << 1
				}

				// 移动部门
				if orgNode.parent.NodeCode != node.parent.NodeCode {
					orgNode.Action |= 1 << 2
					// 查找新的父级id，如果没找到，就需要在新建的部门里面去查
					if father, exist := DataBaseOrgMapBak[node.parent.NodeCode]; exist {
						orgNode.FatherId = father.DepId
					}
				}

				taskUpdateQueue.Push(orgNode)
			}

			node.DiffCompare = true
		} else {
			// 需要新建org，压入队列
			if orgNode.Root == false {
				orgNode.OrgId = orgId
				// 查找父级id
				if father, exist := DataBaseOrgMapBak[orgNode.parent.NodeCode]; exist {
					orgNode.parent.DepId = father.DepId
					orgNode.FatherId = father.DepId
				}
			}

			// 创建部门
			orgNode.Action = 1
			taskNewQueue.Push(orgNode)
		}
	}

	// 删除队列需要特殊进行递归删除
	for _, value := range DataBaseOrgMapBak {
		// 需要进行删除的org
		if value.DiffCompare == false {
			value.Action = 1 << 3
			taskDelQueue.Push(value)
		}
	}

	return taskNewQueue, taskUpdateQueue, taskDelQueue
}

func CompareUpsteramAndCreateOrgTask(orgQueue *Queue) (*Queue, *Queue, *Queue) {
	// 需要新建org的任务队列
	taskNewQueue := new(Queue)
	// 需要更新org的任务队列
	taskUpdateQueue := new(Queue)
	// 需要删除org的任务队列
	taskDelQueue := new(Queue)

	var orgId string
	for {
		if orgQueue.Len() <= 0 {
			break
		}

		orgNode := orgQueue.Pop().(*DataOrgMemNode)
		if node, ok := UpstreamDataExtraKey[orgNode.NodeCode]; ok {
			// 根节点不需要做更新判断
			if orgNode.Root == true {
				orgId = node.OrgId
				node.Action = true
				continue
			}

			// 先填充原来的oneauth相关信息
			orgNode.OrgId = node.OrgId
			orgNode.DepId = node.DepId
			orgNode.FatherId = node.ParentId

			if orgNode.NodeName != node.Name || orgNode.parent.NodeCode != node.FatherCode {
				// 更新名字
				if orgNode.NodeName != node.Name {
					orgNode.Action = 1 << 1
				}

				// 移动部门
				if orgNode.parent.NodeCode != node.FatherCode {
					orgNode.Action |= 1 << 2
					// 查找新的父级id，如果没找到，就需要在新建的部门里面去查
					if father, exist := UpstreamDataExtraKey[node.FatherCode]; exist {
						orgNode.FatherId = father.DepId
					}
				}

				taskUpdateQueue.Push(orgNode)
			}

			node.Action = true
		} else {
			// 需要新建org，压入队列
			if orgNode.Root == false {
				orgNode.OrgId = orgId
				// 查找父级id
				if father, exist := UpstreamDataExtraKey[orgNode.parent.NodeCode]; exist {
					orgNode.parent.DepId = father.DepId
					orgNode.FatherId = father.DepId
				}
			}

			// 创建部门
			orgNode.Action = 1
			taskNewQueue.Push(orgNode)
		}
	}

	// 删除队列需要特殊进行递归删除
	for key, value := range UpstreamDataExtraKey {
		// 需要进行删除的org
		if value.Action == false {
			newDelNode := new(DataOrgMemNode)
			newDelNode.NodeCode = key
			newDelNode.NodeName = value.Name
			newDelNode.OrgId = value.OrgId
			newDelNode.DepId = value.DepId
			newDelNode.Action = 1 << 3
			taskDelQueue.Push(newDelNode)
		}
	}

	return taskNewQueue, taskUpdateQueue, taskDelQueue
}

// 创建组织架构任务队列数组，每个队列的第一个为根节点任务
func CreateOrgTaskQueue(realOrg *DataOrgMemNode) (*Queue, *Queue, *Queue) {
	// 做树的层序遍历，将数据按序压入队列中,为后面按序做数据比对
	orgQueue := new(Queue)
	levelQueue := new(Queue)

	orgQueue.Push(realOrg)
	levelQueue.Push(realOrg)

	for {
		size := levelQueue.Len()
		if size > 0 {
			node := levelQueue.Pop().(*DataOrgMemNode)
			for _, child := range node.Children {
				levelQueue.Push(child)
				orgQueue.Push(child)
			}

			size--
		} else {
			break
		}
	}

	/*
		// 需要新建org的任务队列
		taskNewQueue := new(Queue)
		// 需要更新org的任务队列
		taskUpdateQueue := new(Queue)
		// 需要删除org的任务队列
		taskDelQueue := new(Queue)

		var orgId string
		for {
			if orgQueue.Len() <= 0 {
				break
			}

			orgNode := orgQueue.Pop().(*DataOrgMemNode)
			if node, ok := UpstreamDataExtraKey[orgNode.NodeCode]; ok {
				// 根节点不需要做更新判断
				if orgNode.Root == true {
					orgId = node.OrgId
					node.Action = true
					continue
				}

				// 先填充原来的oneauth相关信息
				orgNode.OrgId = node.OrgId
				orgNode.DepId = node.DepId
				orgNode.FatherId = node.ParentId

				if orgNode.NodeName != node.Name || orgNode.parent.NodeCode != node.FatherCode {
					// 更新名字
					if orgNode.NodeName != node.Name {
						orgNode.Action = 1 << 1
					}

					// 移动部门
					if orgNode.parent.NodeCode != node.FatherCode {
						orgNode.Action |= 1 << 2
						// 查找新的父级id，如果没找到，就需要在新建的部门里面去查
						if father, exist := UpstreamDataExtraKey[node.FatherCode]; exist {
							orgNode.FatherId = father.DepId
						}
					}

					taskUpdateQueue.Push(orgNode)
				}

				node.Action = true
			} else {
				// 需要新建org，压入队列
				if orgNode.Root == false {
					orgNode.OrgId = orgId
					// 查找父级id
					if father, exist := UpstreamDataExtraKey[orgNode.parent.NodeCode]; exist {
						orgNode.parent.DepId = father.DepId
						orgNode.FatherId = father.DepId
					}
				}

				// 创建部门
				orgNode.Action = 1
				taskNewQueue.Push(orgNode)
			}
		}

		// 删除队列需要特殊进行递归删除
		for key, value := range UpstreamDataExtraKey {
			// 需要进行删除的org
			if value.Action == false {
				newDelNode := new(DataOrgMemNode)
				newDelNode.NodeCode = key
				newDelNode.NodeName = value.Name
				newDelNode.OrgId = value.OrgId
				newDelNode.DepId = value.DepId
				newDelNode.Action = 1 << 3
				taskDelQueue.Push(newDelNode)
			}
		}

		return taskNewQueue, taskUpdateQueue, taskDelQueue
	*/

	if UpstreamDataExtraKey == nil {
		return CompareDataBakAndCreateOrgTask(orgQueue)
	}

	return CompareUpsteramAndCreateOrgTask(orgQueue)
}

// 执行组织架构任务队列的任务，此处只执行创建和更新任务，删除任务需要最后执行
func ProcessOrgTaskQueue(taskNewQueue, taskUpdateQueue *Queue) {
	// 执行创建org任务队列

	log.Info("[oneauth] create queue count: ", taskNewQueue.Len())
	// 保存根节点id
	var orgId string
	for {
		if taskNewQueue.Len() <= 0 {
			break
		}

		task := taskNewQueue.Pop().(*DataOrgMemNode)

		if task.parent != nil {
			log.Debug("[oneauth] Create new org: [", task.NodeCode, ", ", task.NodeName,
				"], parent: [", task.parent.NodeCode, ", ", task.parent.NodeName, "]")
		} else {
			log.Debug("[oneauth] Create new org: [", task.NodeCode, ", ", task.NodeName, "], parent: [nil]")
		}

		if task.Root == true {
			// 创建根节点
			newOrgId, err := CreateRootOrg(task)
			if err != nil {
				// 根节点创建失败，直接跳出本组织的创建
				log.Error("[Oneauth] Create root org error, break.")
				break
			}

			task.OrgId = newOrgId
			orgId = newOrgId

			log.Info("[Oneauth] get orgid: ", newOrgId)
		} else {
			// 创建普通部门
			if len(orgId) > 0 {
				task.OrgId = orgId
			}

			depId, err := CreateNormalOrg(task)
			if err != nil {
				continue
			}

			task.DepId = depId
		}
	}

	log.Info("[oneauth] update org queue count: ", taskUpdateQueue.Len())

	// 执行更新task队列
	for {
		if taskUpdateQueue.Len() <= 0 {
			break
		}

		task := taskUpdateQueue.Pop().(*DataOrgMemNode)
		if task.Root == true {
			// TODO: 更新root节点信息
			continue
		}

		log.Debug("[oneauth] Update org: ", task.NodeCode, ", ", task.NodeName, ", ", task.Action)

		if task.Action&(1<<2) != 0 && len(task.FatherId) == 0 {
			if father, ok := DataBaseOrgMap[task.parent.NodeCode]; ok {
				task.FatherId = father.DepId
			} else {
				log.Error("[Oneauth] move can't find father, node: ", task.NodeCode, ", ", task.NodeName, ", father: ", task.parent.NodeCode)
				continue
			}
		}

		UpdateNormalOrg(task)
	}
}

func ProcessDelOrgTaskQueue(taskDelQueue *Queue) {
	for {
		if taskDelQueue.Len() <= 0 {
			break
		}

		task := taskDelQueue.Pop().(*DataOrgMemNode)
		if task.Action&(1<<3) != 0 {
			DelNormalOrg(task)
		}
	}
}

func CompareAndCreateUserTask(userMap *map[string]*DataApiEmpNode, compareUserMap *map[string]*DataApiEmpNode) *Queue {
	taskUsersQueue := new(Queue)
	for key, user := range *userMap {
		if node, ok := (*compareUserMap)[key]; ok {
			log.Debug(fmt.Sprintf("[task] database[%s,%s,%s,%s,%s,%s], oneauth[%s,%s,%s,%s,%s,%s]",
				user.UserCode, user.UserName, user.Email, user.OAID, user.OrgId, user.DepId,
				node.UserCode, node.UserName, node.Email, node.OAID, node.OrgId, user.DepId))

			if user.UserName != node.UserName || user.Email != node.Email || user.OAID != node.OAID {
				user.Action = 1 << 1
				user.Id = node.Id
			}

			if user.DepId != node.DepId || user.OrgId != node.OrgId {
				user.Action |= 1 << 2
			}

			node.DiffCompare = true
		} else {
			user.Action = 1
		}

		// 将需要做操作的user添加到任务队列里面
		if user.Action != 0 {
			taskUsersQueue.Push(user)
		}
	}

	for _, user := range *compareUserMap {
		if user.DiffCompare == false {
			user.Action = 1 << 3
			taskUsersQueue.Push(user)
		}
	}

	return taskUsersQueue
}

// 创建组织架构任务队列数组，每个队列的第一个为根节点任务
func CreateUserTaskQueue(userMap *map[string]*DataApiEmpNode) *Queue {
	if UpstreamUsersData == nil {
		return CompareAndCreateUserTask(userMap, &DataBaseAllMembersMapBak)
	}

	return CompareAndCreateUserTask(userMap, &UpstreamUsersData)
}

func ProcessFiberUserTaskQueue(taskUsersQueue *Queue) {
	for {
		if taskUsersQueue.Len() <= 0 {
			break
		}

		// 发起主数据同步client
		var ClientFiberUpstream = &http.Client{
			Transport: &http.Transport{
				Dial:                UpstreamConn,
				MaxIdleConns:        10,
				MaxIdleConnsPerHost: 30,
				IdleConnTimeout:     20 * time.Second,
				DisableKeepAlives:   false,
				TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
			},
			// 设置超时时间
			Timeout: time.Second * 60,
		}

		// 做并发任务分发
		task := taskUsersQueue.Pop().(*DataApiEmpNode)
		log.Debug(fmt.Sprintf("[oneauth] task process user: [%s, %s, %s, %s, %s, %d]", task.UserCode, task.UserName, task.OrgId, task.DepId, task.Id, task.Action))
		// 新建
		if task.Action&(1<<0) != 0 {
			log.Debug(fmt.Sprintf("[oneauth] Create user: [%s, %s, %s, %s]", task.UserCode, task.UserName, task.OrgId, task.DepId))
			id, err := CreateNewUser(ClientFiberUpstream, task)
			if err != nil {
				continue
			}

			task.Id = id
			task.Action &^= 1 << 0
		}

		// 更新
		if task.Action&(1<<1) != 0 {
			log.Debug(fmt.Sprintf("[oneauth] Update user: [%s, %s, %s, %s]", task.UserCode, task.UserName, task.OrgId, task.DepId))
			UpdateUserInfo(ClientFiberUpstream, task)
			task.Action &^= 1 << 1
		}

		// 移动
		if task.Action&(1<<2) != 0 {
			log.Debug(fmt.Sprintf("[oneauth] Move user: [%s, %s, %s, %s]", task.UserCode, task.UserName, task.OrgId, task.DepId))
			MoveUserByUserId(ClientFiberUpstream, task)
			task.Action &^= 1 << 2
		}

		// 删除
		if task.Action&(1<<3) != 0 {
			log.Debug(fmt.Sprintf("[oneauth] Delete user: [%s, %s, %s]", task.UserCode, task.UserName, task.Id))
			DeleteUserByUserId(ClientFiberUpstream, task)
			task.Action &^= 1 << 3
		}

	}
}

func ProcessUsersTaskQueue(taskUsersQueue *Queue) {
	if taskUsersQueue.Len() <= 0 {
		return
	}

	log.Info("[task] ProcessUsersTaskQueue user task queue size: ", taskUsersQueue.Len())

	// 先创建并发数量的任务队列
	fiberCount, _ := strconv.Atoi(GlobalConfig.System.Fiber)
	var FiberQueue = make([]*Queue, fiberCount)
	for i := 0; i < fiberCount; i++ {
		FiberQueue[i] = new(Queue)
	}

	// 任务分摊
	index := 0
	for {
		if taskUsersQueue.Len() <= 0 {
			break
		}

		// 做并发任务分发
		task := taskUsersQueue.Pop().(*DataApiEmpNode)
		FiberQueue[index].Push(task)

		index++
		if index >= fiberCount {
			index = 0
		}
	}

	// 任务执行
	var wg sync.WaitGroup
	for i := 0; i < fiberCount; i++ {
		wg.Add(1)
		go func(index int) {
			ProcessFiberUserTaskQueue(FiberQueue[index])
			wg.Done()
		}(i)
	}

	// 等待协程都执行完毕
	wg.Wait()
}

// 同步数据库内容数据，用于更新到ldap服务
func SyncDatainfoFromDatabase() {
	// 获取所有组织
	orgBody, err := GetDatabaseApi(GlobalConfig.Database.OrgInterface)
	if err != nil {
		log.Warn("[http] api get org some error: ", err)
		return
	}

	ProcessDataApiOrgRsp(orgBody)

	// 创建组织架构任务队列
	taskNewQueue, taskUpdateQueue, taskDelQueue := CreateOrgTaskQueue(DataBaseRealOrgMap)
	ProcessOrgTaskQueue(taskNewQueue, taskUpdateQueue)

	// 获取所有人员
	empBody, err := GetDatabaseApi(GlobalConfig.Database.MemberInterface)
	if err != nil {
		log.Warn("[http] api get members some error: ", err)
		return
	}

	ProcessDataApiEmpRsp(empBody)

	taskUserQueue := CreateUserTaskQueue(&DataBaseAllMembersMap)
	ProcessUsersTaskQueue(taskUserQueue)

	// 删除多余的org目录
	ProcessDelOrgTaskQueue(taskDelQueue)

	// 重启后，同步完成第一次数据后，清空从oneauth同步的数据，后续只做新老数据的比对
	UpstreamDataClear()
	// 拉取的数据做备份
	DataBaseRestore()
}

// 数据库相关服务初始化
func InitDatabase() {
	InitSign(GlobalConfig.Database.User.Appkey)

	log.Info(GlobalConfig)

	// 从oneauth同步数据到内存
	err := SyncDataFromOneAuth()
	if err != nil {
		os.Exit(-1)
	}

	InitTimer(SyncDatainfoFromDatabase, TimeToSec(GlobalConfig.Database.ReadTime))
}

func main() {
	var GConfig string
	flag.StringVar(&GConfig, "config", "OneAuth.yaml", "OneAuth的配置文件")
	flag.Parse()

	if InitConfig(GConfig) == false {
		return
	}

	InitDatabase()

	for {
		time.Sleep(time.Second)
	}
}
