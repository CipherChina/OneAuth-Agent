package main

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

// 创建相关接口返回结构
type OrgStatus struct {
	OrgId   string `json:"orgId"`
	Success bool   `json:"success"`
}

type DepStatus struct {
	DepId   string `json:"depId"`
	Success bool   `json:"success"`
}

type UsersStatus struct {
	UserId  string `json:"userId"`
	Success bool   `json:"success"`
}

// 获取根节点接口返回相关结构
type RootInfo struct {
	OrgId    string `json:"orgId"` // 根节点id
	Name     string `json:"name"`
	OriginId string `json:"originId"` // 对应的外部id
}

type RootRspInfo struct {
	Count int        `json:"count"`
	Roots []RootInfo `json:"organizations"`
}

// 获取对应根节点下组织架构接口返回相关结构
type OrgInfo struct {
	ParentId string `json:"parentId"` // 上级部门id
	Name     string `json:"name"`
	DepId    string `json:"DepId"`    // 当前部门id
	OriginId string `json:"originId"` // 对应的外部id
}

type OrgRspInfo struct {
	LevelCount int       `json:"LevelCount"`
	TreeStruct []OrgInfo `json:"treeStruct"`
}

// 获取对应根节点下所有人员接口返回相关结构
type UserDepInfo struct {
	OrgId string   `json:"orgId,omitempty"`
	DepId []string `json:"depId,omitempty"`
}

type MemInfo struct {
	Email       string `json:"email"`
	Account     string `json:"account"`
	EmployeeId  string `json:"employeeId"`
	DisplayName string `json:"displayName"`
	UserId      string `json:"userId"`
	Status      int    `json:"status"`

	Department []UserDepInfo `json:"department"` // 人员所在部门id
}

type MemRspInfo struct {
	Count   int       `json:"count"`
	Members []MemInfo `json:"users"`
}

// 组织架构信息
type DataOrgNode struct {
	Name        string // 部门名字
	OrgUnitCode string // 外部部门id
	Status      string // 部门状态
	FatherCode  string // 父级部门id
	FatherName  string // 父级部门名字
	OrgId       string // oneauth根节点id
	DepId       string // oneauth部门id
	ParentId    string // oneauth父级id
	Action      bool   // 是否进行过操作
}

// oneauth内所有的组织架构数据
var UpstreamDataExtraKey map[string]*DataOrgNode  // key为外部id
var UpstreamDataInsideKey map[string]*DataOrgNode // key为oneauth depid
// oneauth内所有的人员信息
var UpstreamUsersData map[string]*DataApiEmpNode

// 获取所有根节点
var GetAllRoots = "/api/v1/account/org?page=1&limit=1000"

// 更新根节点信息
var UpdateOrgRoot = "/api/v1/account/org/%s?"

// 获取根节点下所有组织架构
var GetAllOrgs = "/api/v1/account/org/%s/tree"

// 获取根节点下所有人员
var GetAllMembers = "/api/v1/account/user?"

// 创建组织架构根节点
var CreateOrgRoot = "/api/v1/account/org?"

// 创建组织架构下部门
var CreateOrgDepartment = "/api/v1/account/org/%s/department?"

// 部门更新+移动
var UpdateOrgDepartment = "/api/v1/account/org/%s/department/%s"
var MoveOrgDepartment = "/api/v1/account/org/%s/department/%s/shift/%s"
var DeleteOrgDepartment = "/api/v1/account/org/%s/department/%s"

var CreateUser = "/api/v1/account/user"
var UpdateUser = "/api/v1/account/user/%s"
var MoveUser = "/api/v1/account/user/%s/org/%s/department/%s"
var DelUser = "/api/v1/account/user/%s/lifecycle/remove"

func InitUpstreamBaseUrl() {
	GlobalConfig.Oneauth.BaseUrl = "http://"
	if GlobalConfig.Oneauth.Upstream.Tls == true {
		GlobalConfig.Oneauth.BaseUrl = "https://"
	}

	GlobalConfig.Oneauth.BaseUrl += GlobalConfig.Oneauth.Upstream.Host + ":" + GlobalConfig.Oneauth.Upstream.Port
}

func UpstreamDataClear() {
	if UpstreamUsersData != nil {
		UpstreamUsersData = nil
	}

	if UpstreamDataExtraKey != nil {
		UpstreamDataExtraKey = nil
	}

	if UpstreamDataInsideKey != nil {
		UpstreamDataInsideKey = nil
	}
}

func UpstreamConn(network, addr string) (net.Conn, error) {
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
var ClientUpstream = &http.Client{
	Transport: &http.Transport{
		Dial:                UpstreamConn,
		MaxIdleConns:        10,
		MaxIdleConnsPerHost: 2,
		IdleConnTimeout:     20 * time.Second,
		DisableKeepAlives:   false,
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
	},
	// 设置超时时间
	Timeout: time.Second * 60,
}

// 调用oneauth接口
func GetDataByOneauthApi(method, urlStr, reqBody string) ([]byte, error) {
	data := strings.NewReader(reqBody)
	req, err := http.NewRequest(method, urlStr, data)
	if err != nil {
		log.Info("[http] oneauth create new request [", urlStr, "]  error: ", err)
		return nil, err
	}

	req.Header.Set("accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", GlobalConfig.Oneauth.Token)

	resp, err := ClientUpstream.Do(req)
	if err != nil {
		log.Info("[http] oneauth recv [", urlStr, "] http response error: ", err)
		return nil, err
	}

	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Info("[http] oneauth read [", urlStr, "] http response body error: ", err)
		return nil, err
	}

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return nil, errors.New("oneauth [" + urlStr + "] response code: " + strconv.Itoa(resp.StatusCode) + ", rspbody: " + string(body))
	}

	//log.Info(string(body))
	return body, nil
}

func ProcessUpstreamRootsResponse(body []byte) (RootRspInfo, error) {
	var responseData RootRspInfo
	if err := json.Unmarshal(body, &responseData); err != nil {
		log.Info("[http] root response json unmarshal error: ", err)
		log.Info(body)
		return responseData, err
	}

	return responseData, nil
}

func GetAllOrgFromUpstream() (RootRspInfo, error) {
	var rootData RootRspInfo

	// 从oneauth同步根节点组织信息
	rootUrl := GlobalConfig.Oneauth.BaseUrl + GetAllRoots
	rootBody, err := GetDataByOneauthApi("GET", rootUrl, "")
	if err != nil {
		log.Error("[http] oneauth get all roots error: ", err)
		return rootData, err
	}

	rootData, err = ProcessUpstreamRootsResponse(rootBody)
	if err != nil {
		log.Error("[http] oneauth parse all roots error: ", err)
		return rootData, err
	}

	return rootData, nil
}

func ProcessUpstreamOrgResponse(body []byte) (OrgRspInfo, error) {
	var responseData OrgRspInfo
	if err := json.Unmarshal(body, &responseData); err != nil {
		log.Info("[http] org response json unmarshal error: ", err)
		log.Info(body)
		return responseData, err
	}

	return responseData, nil
}

func GetAllDepartmentByOrgId(orgId string) (OrgRspInfo, error) {
	var orgData OrgRspInfo
	orgUrl := fmt.Sprintf(GlobalConfig.Oneauth.BaseUrl+GetAllOrgs, orgId)
	orgBody, err := GetDataByOneauthApi("GET", orgUrl, "")
	if err != nil {
		log.Error("[http] oneauth get all org error: ", err)
		return orgData, err
	}

	orgData, err = ProcessUpstreamOrgResponse(orgBody)
	if err != nil {
		log.Error("[http] oneauth parse all org error: ", err)
		return orgData, err
	}

	return orgData, nil
}

func GetAllUsersByOrgId() error {
	page := 1
	for {
		userUrl := GlobalConfig.Oneauth.BaseUrl + GetAllMembers
		params := url.Values{}
		params.Add("page", strconv.Itoa(page))
		params.Add("limit", "100")
		userUrl += params.Encode()

		userBody, err := GetDataByOneauthApi("GET", userUrl, "")
		if err != nil {
			log.Error("[http] oneauth get all users error: ", err)
			return err
		}

		userData, err := ProcessUpstreamMemberResponse(userBody)
		if err != nil {
			log.Error("[http] oneauth parse all users error: ", err)
			return err
		}

		page++

		if len(userData.Members) == 0 {
			break
		}

		if UpstreamUsersData == nil {
			UpstreamUsersData = make(map[string]*DataApiEmpNode)
		}

		for _, user := range userData.Members {
			newUser := new(DataApiEmpNode)
			newUser.Status = strconv.Itoa(user.Status)
			newUser.UserName = user.DisplayName
			newUser.UserCode = user.EmployeeId
			newUser.OAID = user.Account
			newUser.Id = user.UserId
			newUser.Email = user.Email
			newUser.DiffCompare = false
			if len(user.Department) > 0 {
				newUser.OrgId = user.Department[0].OrgId
				if len(user.Department[0].DepId) > 0 {
					newUser.DepId = user.Department[0].DepId[0]
				}
			}

			UpstreamUsersData[newUser.UserCode] = newUser
			log.Trace(fmt.Sprintf("[onesuth] Get user: [%s, %s, %s, %s, %s, %s]",
				newUser.UserCode, newUser.UserName, newUser.OAID, newUser.Id, newUser.OrgId, newUser.DepId))
		}
	}

	return nil
}

func ProcessUpstreamMemberResponse(body []byte) (MemRspInfo, error) {
	var responseData MemRspInfo
	if err := json.Unmarshal(body, &responseData); err != nil {
		log.Info("[http] members response json unmarshal error: ", err)
		//log.Info(body)
		return responseData, err
	}

	return responseData, nil
}

// 创建根节点
func CreateRootOrg(node *DataOrgMemNode) (string, error) {
	urlStr := GlobalConfig.Oneauth.BaseUrl + CreateOrgRoot
	params := url.Values{}
	params.Add("orgName", node.NodeName)
	params.Add("originId", node.NodeCode)
	urlStr += params.Encode()

	rootBody, err := GetDataByOneauthApi("POST", urlStr, "")
	if err != nil {
		log.Error("[http] oneauth create roots [", node.NodeName, "] error: ", err)
		return "", err
	}

	var responseData OrgStatus
	if err := json.Unmarshal(rootBody, &responseData); err != nil {
		log.Info("[http] members response json unmarshal error: ", err)
		return "", err
	}

	return responseData.OrgId, nil
}

func CreateNormalOrg(node *DataOrgMemNode) (string, error) {
	urlStr := fmt.Sprintf(GlobalConfig.Oneauth.BaseUrl+CreateOrgDepartment, node.OrgId)
	params := url.Values{}
	params.Add("department", node.NodeName)
	params.Add("originId", node.NodeCode)
	if len(node.parent.DepId) > 0 {
		params.Add("parentId", node.parent.DepId)
	}
	urlStr += params.Encode()

	rootBody, err := GetDataByOneauthApi("POST", urlStr, "")
	if err != nil {
		log.Error("[http] oneauth create department error: ", err)
		return "", err
	}

	var responseData DepStatus
	if err := json.Unmarshal(rootBody, &responseData); err != nil {
		log.Info("[http] members response json unmarshal error: ", err)
		return "", err
	}

	return responseData.DepId, nil
}

// 更新根节点
func UpdateRootOrg(node *DataOrgMemNode) error {
	urlStr := fmt.Sprintf(GlobalConfig.Oneauth.BaseUrl+UpdateOrgRoot, node.OrgId)
	params := url.Values{}
	params.Add("name", node.NodeName)
	urlStr += params.Encode()

	_, err := GetDataByOneauthApi("PUT", urlStr, "")
	if err != nil {
		log.Error("[http] oneauth create roots [", node.NodeName, "] error: ", err)
		return err
	}

	return nil
}

// 更新普通部门节点
func UpdateNormalOrg(node *DataOrgMemNode) error {
	if node.Action&(1<<1) != 0 {
		urlStr := fmt.Sprintf(GlobalConfig.Oneauth.BaseUrl+UpdateOrgDepartment, node.OrgId, node.DepId)
		body := fmt.Sprintf("{\"name\": \"%s\"}", node.NodeName)

		_, err := GetDataByOneauthApi("PUT", urlStr, body)
		if err != nil {
			log.Error("[http] oneauth update department error: ", err)
			return err
		}

		// 清除位标记
		node.Action &^= 1 << 1
	}

	if node.Action&(1<<2) != 0 {
		urlStr := fmt.Sprintf(GlobalConfig.Oneauth.BaseUrl+MoveOrgDepartment, node.OrgId, node.DepId, node.FatherId)
		_, err := GetDataByOneauthApi("PUT", urlStr, "")
		if err != nil {
			log.Error("[http] oneauth move department error: ", err)
			return err
		}

		// 清除位标记
		node.Action &^= 1 << 2
	}

	return nil
}

func DelNormalOrg(node *DataOrgMemNode) {
	urlStr := fmt.Sprintf(GlobalConfig.Oneauth.BaseUrl+DeleteOrgDepartment, node.OrgId, node.DepId)
	_, err := GetDataByOneauthApi("DELETE", urlStr, "")
	if err != nil {
		log.Error("[http] oneauth delete org [", node.NodeCode, ", ", node.NodeName, "], [", urlStr, "] error: ", err)
	}
}

func SyncDataFromOneAuth() error {
	// 同步根节点数据
	rootData, err := GetAllOrgFromUpstream()
	if err != nil {
		return err
	}

	log.Debug(rootData)

	// 创建upstream org的组织架构
	UpstreamDataExtraKey = make(map[string]*DataOrgNode)
	UpstreamDataInsideKey = make(map[string]*DataOrgNode)

	// 将数据存在内存中，不做维护
	for _, node := range rootData.Roots {
		newOrg := new(DataOrgNode)
		newOrg.OrgId = node.OrgId
		newOrg.DepId = node.OrgId
		newOrg.OrgUnitCode = node.OriginId
		newOrg.Name = node.Name
		newOrg.Action = false

		UpstreamDataExtraKey[newOrg.OrgUnitCode] = newOrg
		UpstreamDataInsideKey[newOrg.DepId] = newOrg

		// 从oneauth同步对应根节点组织架构信息
		depData, err := GetAllDepartmentByOrgId(node.OrgId)
		if err != nil {
			continue
		}

		// 直接粗暴解决判断是否存在，做调整
		for _, depNode := range depData.TreeStruct {
			newDep := new(DataOrgNode)
			newDep.OrgId = node.OrgId
			newDep.DepId = depNode.DepId
			newDep.OrgUnitCode = depNode.OriginId
			newDep.Name = depNode.Name
			newDep.ParentId = depNode.ParentId
			newDep.Action = false

			UpstreamDataExtraKey[newDep.OrgUnitCode] = newDep
			UpstreamDataInsideKey[newDep.DepId] = newDep
		}

		// 调整外部fatherid的对应关系
		for _, value := range UpstreamDataExtraKey {
			if len(value.ParentId) > 0 && (len(value.FatherCode) == 0 || len(value.FatherName) == 0) {
				if father, ok := UpstreamDataInsideKey[value.ParentId]; ok {
					value.FatherCode = father.OrgUnitCode
					value.FatherName = father.Name
				}
			}
		}

	}

	// 从oneauth同步人员信息
	return GetAllUsersByOrgId()
}

// 更新根节点
func CreateNewUser(node *DataApiEmpNode) (string, error) {
	urlStr := GlobalConfig.Oneauth.BaseUrl + CreateUser
	body := fmt.Sprintf(`{"account":"%s","displayName":"%s","gender":"","idCardNumber":"","address":"","mobilePhone":"","nickName":"","groupId":["1"],"jobTitle":"","firstName":"%s","lastName":"","birthday":"2022-06-06","email":"%s","employeeId":"%s","orgId":"%s","departmentId":["%s"]}`,
		node.OAID, node.UserName, node.UserName, node.Email, node.UserCode, node.OrgId, node.DepId)

	log.Trace(body)

	rootBody, err := GetDataByOneauthApi("POST", urlStr, body)
	if err != nil {
		log.Error("[http] oneauth create users [", node.UserCode, ", ", node.UserName, "] error: ", err)
		return "", err
	}

	var responseData UsersStatus
	if err := json.Unmarshal(rootBody, &responseData); err != nil {
		log.Error("[http] oneauth create users response json unmarshal [", node.UserCode, ", ", node.UserName, "] error: ", err)
		return "", err
	}

	return responseData.UserId, nil
}

func UpdateUserInfo(node *DataApiEmpNode) error {
	urlStr := fmt.Sprintf(GlobalConfig.Oneauth.BaseUrl+UpdateUser, node.Id)
	body := fmt.Sprintf(`{"propval":{"account":"%s","displayName":"%s","gender":"","idCardNumber":"","address":"","mobilePhone":"","nickName":"","groupId":["1"],"jobTitle":"","firstName":"%s","lastName":"","birthday":"2022-06-06","email":"%s","employeeId":"%s","orgId":"%s","departmentId":["%s"]}}`,
		node.OAID, node.UserName, node.UserName, node.Email, node.UserCode, node.OrgId, node.DepId)

	log.Trace(body)

	_, err := GetDataByOneauthApi("PUT", urlStr, body)
	if err != nil {
		log.Error("[http] oneauth update user [", node.UserCode, ", ", node.UserName, ", ", node.Id, "] error: ", err)
		return err
	}

	return nil
}

func MoveUserByUserId(node *DataApiEmpNode) error {
	urlStr := fmt.Sprintf(GlobalConfig.Oneauth.BaseUrl+MoveUser, node.Id, node.OrgId, node.DepId)
	_, err := GetDataByOneauthApi("PUT", urlStr, "")
	if err != nil {
		log.Error("[http] oneauth move user [", node.UserCode, ", ", node.UserName, ", ", node.Id, "] error: ", err)
		return err
	}

	return nil
}

func DeleteUserByUserId(node *DataApiEmpNode) error {
	urlStr := fmt.Sprintf(GlobalConfig.Oneauth.BaseUrl+DelUser, node.Id)
	_, err := GetDataByOneauthApi("PUT", urlStr, "")
	if err != nil {
		log.Error("[http] oneauth delete user [", node.UserCode, ", ", node.UserName, ", ", node.Id, "] error: ", err)
		return err
	}

	return nil
}
