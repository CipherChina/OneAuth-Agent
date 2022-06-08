package main

import (
	"crypto/md5"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

var OrgUrl = "/api/service/datapub/rest/api/v1/org/queryDlpOrg?bsId="
var MemberUrl = "/api/service/datapub/rest/api/v1/emp/queryDlpEmp?bsId="

// 组织架构信息
type DataApiOrgNode struct {
	OrgUnitCode      string `json: "orgUnitCode"`
	OrgUnitName      string `json: "orgUnitName"`
	Status           string `json: "status"`
	UpperOrgUnitCode string `json: "upperOrgUnitCode"`
	UpperOrgUnitName string `json: "upperOrgUnitName"`
	LeaderCode       string `json: "leaderCode"`
	LeaderName       string `json: "leaderName"`
	UpdateDate       string `json: "updateDate"`
}

// 获取组织架构响应结构
type DataApiOrgResponse struct {
	Code        string           `json: "code"`
	Message     string           `json: "message"`
	Data        []DataApiOrgNode `json: "data"`
	Placeholder string           `json: "placeholder"`
	ErrorMsg    string           `json: "errorMsg"`
}

// 组织架构人员信息
type DataApiEmpNode struct {
	UserCode   string `json: "userCode"`
	UserName   string `json: "userName"`
	Email      string `json: "email"`
	Status     string `json: "status"`
	OAID       string `json: "OAID"`
	BsId       string `json: "bsId"`
	Version    string `json: "version"`
	UpdateDate string `json: "updateDate"`
	OrgCode    string `json: "orgCode"`
	OrgName    string `json: "orgName"`

	Id          string // Oneauth用户id
	DepId       string // Oneauth部门id
	OrgId       string // Oneauth组织id
	DiffCompare bool   // 是否进行过数据比对
	Action      int    // Oneauth操作类型, 0不操作， 1 << 0新建，1 << 1修改，1 << 2移动，1 << 3删除
}

// 获取组织架构人员响应结构
type DataApiEmpResponse struct {
	Code        string           `json: "code"`
	Message     string           `json: "message"`
	Data        []DataApiEmpNode `json: "data"`
	Placeholder string           `json: "placeholder"`
	ErrorMsg    string           `json: "errorMsg"`
}

// 所有组织架构信息节点集合
var DataBaseOrgMap map[string]*DataOrgMemNode

// 实际组织架构结构
var DataBaseRealOrgMap *DataOrgMemNode

// 所有人员
var DataBaseAllMembersMap map[string]*DataApiEmpNode

// 作为老数据备份
var DataBaseOrgMapBak map[string]*DataOrgMemNode
var DataBaseRealOrgMapBak *DataOrgMemNode
var DataBaseAllMembersMapBak map[string]*DataApiEmpNode

func DataBaseRestore() {
	// 备份数据
	DataBaseOrgMapBak = DataBaseOrgMap
	DataBaseRealOrgMapBak = DataBaseRealOrgMap
	DataBaseAllMembersMapBak = DataBaseAllMembersMap

	// 清理新数据变量
	DataBaseOrgMap = nil
	DataBaseRealOrgMap = nil
	DataBaseAllMembersMap = nil
}

func DatabaseConn(network, addr string) (net.Conn, error) {
	// netAddr := &net.TCPAddr{Port: apiConfig.localPort}
	dial := net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 60 * 5 * time.Second,
		// LocalAddr: netAddr,
	}

	conn, err := dial.Dial(network, addr)
	if err != nil {
		return conn, err
	}

	return conn, err
}

// 发起主数据同步client
var ClientDataBase = &http.Client{
	Transport: &http.Transport{
		Dial:                DatabaseConn,
		MaxIdleConns:        10,
		MaxIdleConnsPerHost: 2,
		IdleConnTimeout:     20 * time.Second,
		DisableKeepAlives:   false,
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
	},
	// 设置超时时间
	Timeout: time.Second * 60,
}

func GetSign(secret string, urlParams map[string]string, mapkey []string) string {
	if len(mapkey) == 0 || len(urlParams) == 0 {
		return ""
	}

	target := make([]string, 0, len(mapkey))
	for _, key := range mapkey {
		target = append(target, key+"="+urlParams[key])
	}

	targetStr := strings.Join(target, "&") + secret
	return fmt.Sprintf("%x", md5.Sum([]byte(targetStr)))
}

// 生成签名串
func InitSign(bsId string) {
	urlParams := make(map[string]string)
	urlParams["bsId"] = bsId
	urlParams["appKey"] = GlobalConfig.Database.User.Appkey

	var mapkey []string
	mapkey = append(mapkey, "appKey")
	mapkey = append(mapkey, "bsId")
	sort.Strings(mapkey)

	GlobalConfig.Database.User.Sign = GetSign(GlobalConfig.Database.User.Appsecret, urlParams, mapkey)
	log.Info("[http] sign: ", GlobalConfig.Database.User.Sign)
}

// 获取主数据接口
func GetDatabaseApi(urlStr string) ([]byte, error) {
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		log.Info("[http] create new request error: ", err)
		return nil, err
	}

	req.Header.Set("appKey", GlobalConfig.Database.User.Appkey)
	req.Header.Set("sign", GlobalConfig.Database.User.Sign)

	resp, err := ClientDataBase.Do(req)
	if err != nil {
		log.Info("[http] recv http response error: ", err)
		return nil, err
	}

	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Info("[http] read http response body error: ", err)
		return nil, err
	}

	if resp.StatusCode != 200 {
		log.Info(string(body))
		return nil, errors.New("response code: " + strconv.Itoa(resp.StatusCode))
	}

	//log.Info(string(body))
	return body, nil
}

// 过滤目录
func ProcessOrgFilter(unitcode, unitname string) bool {
	if _, ok := GlobalConfig.Database.Filter.Filter[unitcode]; ok {
		return true
	}

	if _, ok := GlobalConfig.Database.Filter.Filter[unitname]; ok {
		return true
	}

	return false
}

// 删除不需要的org节点
func DeleteOrgNode(orgmap *DataOrgMemNode) {
	for _, node := range orgmap.Children {
		DeleteOrgNode(node)
		delete(DataBaseOrgMap, node.NodeCode)
	}

	delete(DataBaseOrgMap, orgmap.NodeCode)
}

func FilterOrgMap(orgmap *DataOrgMemNode) {
	queNode := new(Queue)
	queNode.Push(orgmap)
	for queNode.Len() > 0 {
		size := queNode.Len()
		//log.Info("queue length: ", queNode.length)
		for i := 0; i < size; i++ {
			tmpNode := queNode.Pop().(*DataOrgMemNode)
			if len(tmpNode.NodeName) == 0 || tmpNode.Value.Status != "1" {
				DeleteOrgNode(tmpNode)
				//delete(orgMap, tmpNode.NodeCode)

				if tmpNode.parent != nil {
					delete(tmpNode.parent.Children, tmpNode.NodeCode)
					tmpNode.parent = nil
				}
				continue
			}

			/*
				if tmpNode.parent != nil {
					log.Info("avalible org: ", tmpNode.NodeCode, ", ", tmpNode.NodeName, ", parent: ", tmpNode.parent.NodeCode, ", ", tmpNode.parent.NodeName)
				} else {
					log.Info("avalible org: ", tmpNode.NodeCode, ", ", tmpNode.NodeName, ", parent: nil")
				}
			*/

			for _, node := range tmpNode.Children {
				queNode.Push(node)
				//log.Error("error log: ", node.NodeCode, node.NodeName)
			}
		}
	}
}

// 过滤出指定目录数据
func FiterSyncOu(topOrg *DataOrgMemNode) {
	if len(GlobalConfig.Database.SyncOu) == 0 {
		return
	}

	basenode := DataBaseOrgMap[GlobalConfig.Database.SyncOu]
	if basenode == nil {
		return
	}

	// 如果指定的目录直接是顶层目录，则删除其他顶层目录即可
	if basenode.parent != topOrg {
		// 将basenode从父节点中删除
		delete(basenode.parent.Children, basenode.NodeCode)
		// 变更basenode父节点为根节点
		basenode.parent = topOrg
		basenode.Root = false
		topOrg.Children[GlobalConfig.Database.SyncOu] = basenode
	}

	// 递归删除其他顶层结构
	for code, node := range topOrg.Children {
		if code != GlobalConfig.Database.SyncOu {
			DeleteOrgNode(node)
			delete(topOrg.Children, code)
			node.parent = nil
		}
	}
}

// 处理组织架构数据
func ProcessDataApiOrgRsp(body []byte) {
	// 清理原有的数据
	DataBaseOrgMap = nil
	DataBaseRealOrgMap = nil

	var responseData DataApiOrgResponse
	if err := json.Unmarshal(body, &responseData); err != nil {
		log.Info("[http] org response json unmarshal error: ", err)
		log.Info(body)
		return
	}

	log.Info("[http] org response json org unmarshal success, get orgs count: ", len(responseData.Data))

	DataBaseOrgMap = make(map[string]*DataOrgMemNode)
	for _, node := range responseData.Data {
		// 替换名字中的逗号为空格
		node.OrgUnitName = strings.Replace(node.OrgUnitName, ",", " ", -1)
		node.UpperOrgUnitName = strings.Replace(node.UpperOrgUnitName, ",", " ", -1)

		newnode := new(DataApiOrgNode)
		*newnode = node

		// 组织编码如果没有，则设置组织名
		if len(node.UpperOrgUnitCode) == 0 && len(node.UpperOrgUnitName) > 0 {
			node.UpperOrgUnitCode = node.UpperOrgUnitName
		}

		// 设置目录过滤
		if ProcessOrgFilter(newnode.OrgUnitCode, newnode.OrgUnitName) == true {
			newnode.Status = "2"
		}

		if midnode, ok := DataBaseOrgMap[node.OrgUnitCode]; ok {
			// key存在，创建实际中继节点，同时创建虚拟父节点
			// log.Info("创建中继节点: ", midnode.NodeCode)
			midnode.Value = newnode

			// 父节点若存在则直接指针指过去，若不存在则建立虚拟父节点
			if father, fatherok := DataBaseOrgMap[node.UpperOrgUnitCode]; fatherok {
				midnode.parent = father
				if father.Children == nil {
					father.Children = make(map[string]*DataOrgMemNode)
				}
				father.Children[midnode.NodeCode] = midnode
			} else {
				// 创建虚拟父节点，并将本节点插入到父节点的子集中
				var fatherOrg = new(DataOrgMemNode)
				fatherOrg.NodeCode = node.UpperOrgUnitCode
				fatherOrg.NodeName = node.UpperOrgUnitName
				fatherOrg.Root = false
				fatherOrg.DiffCompare = false
				fatherOrg.OuName = fatherOrg.NodeName + "(" + fatherOrg.NodeCode + ")"

				fatherOrg.Children = make(map[string]*DataOrgMemNode)
				fatherOrg.Children[midnode.NodeCode] = midnode
				DataBaseOrgMap[fatherOrg.NodeCode] = fatherOrg
				midnode.parent = DataBaseOrgMap[node.UpperOrgUnitCode]
				// log.Info("创建父节点: ", fatherOrg.NodeCode)
			}
		} else {
			var newOrg = new(DataOrgMemNode)
			newOrg.NodeCode = node.OrgUnitCode
			newOrg.NodeName = node.OrgUnitName
			newOrg.Root = false
			newOrg.DiffCompare = false
			newOrg.OuName = newOrg.NodeName + "(" + newOrg.NodeCode + ")"
			newOrg.Value = newnode
			DataBaseOrgMap[newOrg.NodeCode] = newOrg

			// 顶层节点
			if len(node.UpperOrgUnitCode) == 0 {
				continue
			}

			// 父节点若存在则直接指针指过去，若不存在则建立虚拟父节点
			father := DataBaseOrgMap[node.UpperOrgUnitCode]
			if father != nil {
				if father.Children == nil {
					father.Children = make(map[string]*DataOrgMemNode)
				}
			} else {
				// 创建虚拟父节点，并将本节点插入到父节点的子集中
				father = new(DataOrgMemNode)
				father.NodeCode = node.UpperOrgUnitCode
				father.NodeName = node.UpperOrgUnitName
				father.Root = false
				father.DiffCompare = false
				father.OuName = father.NodeName + "(" + father.NodeCode + ")"
				father.Children = make(map[string]*DataOrgMemNode)

				DataBaseOrgMap[father.NodeCode] = father
			}

			father.Children[newOrg.NodeCode] = newOrg
			newOrg.parent = father
		}
	}

	// 添加顶层主目录
	topOrg := new(DataOrgMemNode)
	// 设置name和code是为了防止后面过滤目录时，被删除掉
	topOrg.NodeName = GlobalConfig.Oneauth.RootName
	topOrg.NodeCode = GlobalConfig.Oneauth.RootName
	topOrg.OuName = GlobalConfig.Oneauth.RootName
	value := new(DataApiOrgNode)
	value.Status = "1"
	topOrg.Value = value
	topOrg.Root = true
	topOrg.DiffCompare = false
	topOrg.Children = make(map[string]*DataOrgMemNode)

	// 添加默认目录
	if len(GlobalConfig.Database.DefaultTree) > 0 {
		DefaultOrg := new(DataOrgMemNode)
		DefaultOrg.NodeName = GlobalConfig.Database.DefaultTree
		DefaultOrg.NodeCode = GlobalConfig.Database.DefaultTree
		DefaultOrg.OuName = GlobalConfig.Database.DefaultTree
		DefaultOrg.Root = false
		DefaultOrg.DiffCompare = false
		// 添加到orgmap集合
		DataBaseOrgMap[DefaultOrg.NodeCode] = DefaultOrg
	}

	for key, node := range DataBaseOrgMap {
		if node.Value == nil || node.parent == nil {
			if node.Value == nil {
				value := new(DataApiOrgNode)
				node.Value = value
			}

			// 设置目录过滤, 2无效，1有效
			if ProcessOrgFilter(node.NodeCode, node.NodeName) == true {
				node.Value.Status = "2"
			} else {
				node.Value.Status = "1"
			}

			node.parent = topOrg
			topOrg.Children[key] = node
			log.Info("id: " + node.NodeCode + ", name: " + node.NodeName)
		}
	}

	log.Info("获取总组织数量: ", len(DataBaseOrgMap))

	// 过滤掉name为空和无效的组织架构
	FilterOrgMap(topOrg)
	// 过滤出指定目录数据
	FiterSyncOu(topOrg)

	DataBaseRealOrgMap = topOrg
	log.Info("有效总组织数量: ", len(DataBaseOrgMap), ", 总公司数量: ", len(DataBaseRealOrgMap.Children))
}

func FilterUnrelatedUsers(usersMap *map[string]*DataApiEmpNode) {
	DataBaseAllMembersMap = nil
	DataBaseAllMembersMap = make(map[string]*DataApiEmpNode)

	for key, value := range *usersMap {
		if father, ok := DataBaseOrgMap[value.OrgCode]; ok {
			// 更新人员的orgid和depid
			value.DepId = father.DepId
			value.OrgId = father.OrgId
			DataBaseAllMembersMap[key] = value
		}
	}
}

func ProcessDataApiEmpRsp(body []byte) {

	var responseData DataApiEmpResponse
	if err := json.Unmarshal(body, &responseData); err != nil {
		log.Info("[http] org response json unmarshal error: ", err)
		return
	}

	log.Info("总人员数量: ", len(responseData.Data))

	if len(responseData.Data) > 0 {
		var usersMap = make(map[string]*DataApiEmpNode)
		for _, person := range responseData.Data {
			// 无效用户直接过滤
			if len(person.UserName) == 0 || len(person.OAID) == 0 || person.Status != "1" {
				continue
			}

			newUser := new(DataApiEmpNode)
			*newUser = person

			// 更新orgId和depId
			if father, ok := DataBaseOrgMap[newUser.OrgCode]; ok {
				newUser.OrgId = father.OrgId
				newUser.DepId = father.DepId
			}

			usersMap[person.UserCode] = newUser

			/*
						newPerson := new(PersonNode)
						newPerson.CurrentFlag = false
						newPerson.Person.DisplayName = person.UserName
						newPerson.Person.Name = person.UserName
						newPerson.Person.Email = person.Email
						newPerson.Person.Department = person.OrgName
						newPerson.Person.SAMAccountName = person.OAID
						newPerson.Person.UserPrincipalName = person.OAID + "@greentown.com"
						newPerson.Person.UserCode = person.UserCode
						newPerson.Person.Status = 512

						newPerson.Person.Password = base64.StdEncoding.EncodeToString([]byte(person.OAID + "@" + "1234567"))
						utf16 := unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM)
						newPerson.Person.Password, _ = utf16.NewEncoder().String("\"" + newPerson.Person.Password + "\"")



					// 创建DN + 负责人CN
					orgNode := orgMap[person.OrgCode]
					if orgNode == nil {
						// 部门不存在，放到临时管理集合中
						ExtraMembersMap[person.UserCode] = newPerson
						continue
					}

					if orgNode.Members == nil {
						orgNode.Members = make(map[string]*PersonNode)
					}

					// 设置ou下的basedn
					baseTree := "OU=" + orgNode.OuName
					for node := orgNode; node.parent != nil; node = node.parent {
						baseTree += ",OU=" + node.parent.OuName
					}
					baseTree += "," + systemConfig.RootDN

					// 查询同部门是否有同名的存在
					if same_name, ok := orgNode.Members[person.UserName]; ok {
						// 重建同名者dn
						same_name.Person.Name += "(" + same_name.Person.UserCode + ")"
						same_name.DN = "CN=" + same_name.Person.Name + "," + baseTree

						newPerson.Person.Name += "(" + newPerson.Person.UserCode + ")"
						orgNode.Members[newPerson.Person.Name] = newPerson
					} else {
						orgNode.Members[newPerson.Person.Name] = newPerson
					}

					newPerson.DN = "CN=" + newPerson.Person.Name + "," + baseTree

					// 此处仅保存manager的员工id
					if orgNode.Value != nil && len(orgNode.Value.LeaderCode) > 0 && orgNode.Value.LeaderCode != person.UserCode {
						// manager需要在后面另外生成
						newPerson.Person.LeaderCode = orgNode.Value.LeaderCode
					}



				AllMembersMap[person.UserCode] = newPerson

			*/
		}

		// 过滤掉找不到组织的人员
		FilterUnrelatedUsers(&usersMap)
	}

	log.Info("有效人员数量: ", len(DataBaseAllMembersMap))
}
