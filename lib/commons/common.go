/*Copyright 2018 Infinidat

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.*/
package commons

import (
	"infinidat-k8s-provisioner/lib/controller"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"github.com/go-resty/resty"
	"github.com/golang/glog"
	"net/http"
	"os"
	"k8s.io/api/core/v1"
)

const (
	Apiusername = "MGMT_USERNAME"
	Apipassword = "MGMT_PASSWORD"
	Apiurl      = "MGMT_URL"
)

//Method to check the response is valid or not
func CheckResponse(res *resty.Response, err error) (result interface{}, er error) {
	defer func() {
		if recovered := recover(); recovered != nil && er == nil {
			er = errors.New("error while parsing management api response " + fmt.Sprint(recovered) + "for request " + res.Request.URL)
		}
	}()

	if res.StatusCode() == http.StatusUnauthorized {
		return nil, errors.New("Request authentication failed for : " + res.Request.URL)
	}

	if res.StatusCode() == http.StatusServiceUnavailable{
		return nil, errors.New(res.Status())
	}

	if err != nil {
		return nil, errors.New("Error While Resty call for request " + res.Request.URL + err.Error())
	}
	var response interface{}
	if er := json.Unmarshal(res.Body(), &response); er != nil {
		return nil, er
	}

	if res != nil {
		responseinmap := response.(map[string]interface{})
		if responseinmap != nil {

			if str, iserr := ParseError(responseinmap["error"]); iserr {
				return nil, errors.New(str)
			}
			if result := responseinmap["result"]; result != nil {
				return responseinmap["result"], nil
			} else {
				return nil, errors.New("result part of response is nil for request " + res.Request.URL)
			}
		} else {
			return nil, errors.New("empty response for " + res.Request.URL)
		}
	} else {
		return nil, errors.New("empty response for " + res.Request.URL)
	}
}

//Method to check error response from management api
func ParseError(responseinmap interface{}) (str string, iserr bool) {
	defer func() {
		if res := recover(); res != nil {
			str = "recovered in parseError  " + fmt.Sprint(res)
			iserr = true
		}

	}()

	if responseinmap != nil {
		resultmap := responseinmap.(map[string]interface{})
		return resultmap["code"].(string) + " " + resultmap["message"].(string), true
	}
	return "", false
}

//To attach metadata to the resource
func AttachMetadata(resourceId int, options controller.VolumeOptions, kuberVersion string,filesystemType string) (err error) {

	defer func() {
		if res := recover(); res != nil && err == nil {
			err = errors.New("error while attaching mettadata on infinibox  " + fmt.Sprint(res))
		}
	}()

	//Attach metadata to Resource given by called function
	urlmd := "api/rest/metadata/" + fmt.Sprint(resourceId)

		var body = map[string]interface{}{
			"host.k8s.namespace ": options.PVC.Namespace,
			"host.k8s.pvcname ":   options.PVC.Name,
			"host.k8s.pvcid ":     options.PVC.UID,
			"host.k8s.pvname ":    options.PVName,
			"host.created_by":  "K8S " + kuberVersion,
			}
	//body for iscsi and fc types
	if filesystemType != "" {
		body["filesystem_type"] = filesystemType
	}
	resmd, err := GetRestClient().R().
		SetBody(body).
		Put(urlmd)
	resulput, err := CheckResponse(resmd, err)
	if err != nil {
		return errors.New("["+options.PVName+"] "+err.Error())

	}
	_ = resulput
	glog.Infoln("Metadata Attached: ", options.PVName)

	return nil
}

//To detach the metadata attached to resource
func DetachMetadata(resourceId int64, resourceName string) (err error) {
	defer func() {
		if res := recover(); res != nil && err == nil {
			err = errors.New("error while removing metatdata " + fmt.Sprint(res))
		}
	}()

	urlmd := "api/rest/metadata/" + fmt.Sprint(resourceId)
	resmd, err := GetRestClient().R().
		SetQueryString("approved=true").
		Delete(urlmd)

	_, err = CheckResponse(resmd, err)
	if err != nil {
		return errors.New("["+resourceName+"] "+err.Error())

	}
	glog.Infoln("Metadata  Detached: ", resourceName)

	return nil
}

//Returns poolId of provided pool name
func GetPoolID(name string) (id int64, err error) {

	defer func() {
		if res := recover(); res != nil && err == nil {
			err = errors.New("error while Get Pool ID  " + fmt.Sprint(res))
		}
	}()

	//To get the pool_id for corresponding poolname
	var poolId int64 = -1
	urlpool := "api/rest/pools"

	respool, err := GetRestClient().R().SetQueryString("name=" + name).
		Get(urlpool)

	resultpool, err := CheckResponse(respool, err)
	if err != nil {
		glog.Errorf(fmt.Sprint(err))
	}

	arryofresult := resultpool.([]interface{})
	for _, result := range arryofresult {
		resultmap := result.(map[string]interface{})
		if resultmap["name"] == name {
			poolId = int64(resultmap["id"].(float64))
		}
	}

	if poolId == -1 {
		return poolId, errors.New("No such pool: " + name)
	}

	return poolId, nil
}

var client *resty.Client

func GetRestClient() *resty.Client {
	if client == nil {
		client = resty.New()
		client.SetHeader("Content-Type", "application/json")
		client.SetBasicAuth(os.Getenv(Apiusername), os.Getenv(Apipassword)) // or SetResult(AuthSuccess{})
		client.SetTLSClientConfig(&tls.Config{InsecureSkipVerify: true})
		client.SetHostURL(os.Getenv(Apiurl))
		client.SetDisableWarn(true)

		//validation for the URL
		_ , err := client.R().Get(os.Getenv(Apiurl))
		if err != nil{
			glog.Error("error in validating URL ",err)
		}

	}

	return client
}

func OneTimeValidation(poolname string, networkspace string) ( list string , err error){
	defer func() {
		if res := recover(); res != nil && err == nil {
			err = errors.New("error while One Time Validation   " + fmt.Sprint(res))
		}
	}()
	//validating pool
	var validList = ""
	_,err = GetPoolID(poolname)
	if err!=nil{
		return validList , err
	}


	arrayofNetworkSpaces := strings.Split(networkspace, ",")
	var arrayOfValidnetspaces []string

	for _,name := range arrayofNetworkSpaces {
		flag,err:= NetworkspaceValidation(name)
		if err !=nil{
			glog.Error(err)
		}

		if flag{
			arrayOfValidnetspaces = append(arrayOfValidnetspaces, name)

		}
	}
	if len(arrayOfValidnetspaces)>0{
		validList = strings.Join(arrayOfValidnetspaces,",")
		return validList,nil
	}

	return validList,errors.New("provide valid network spaces")
}

func NetworkspaceValidation(networkspace string)(flag bool, err error){

	defer func() {
		if res := recover(); res != nil && err == nil {
			err = errors.New("error while Networkspace Validation   " + fmt.Sprint(res))
		}
	}()

	//validating networkspace
	urlpool := "api/rest/network/spaces"

	respool, err := GetRestClient().R().SetQueryString("name=" + networkspace).
		Get(urlpool)

	nspace , err := CheckResponse(respool, err)
	if err != nil {
		return false,err
	}

	if fmt.Sprint(nspace) == "[]"{

		return false,errors.New("No such network space: " + networkspace)
	}

	return true,nil
}


// getHostList returns []string ie host registered on infinibox by administrator which is present in k8s cluster
func GetHostList(k8sNodeList []*v1.Node) (list []string, err error) {

	defer func() {
		if res := recover(); res != nil && err == nil {
			err = errors.New("error while getting host list " + fmt.Sprint(res))
		}
	}()

	var hostList []string

	for _, node := range k8sNodeList {
		urlGet := "api/rest/hosts"

		resGet, err := GetRestClient().R().SetQueryString("name="+node.Name).Get(urlGet)
		if err != nil {
			return hostList,err
		}
		var response interface{}
		if err := json.Unmarshal(resGet.Body(), &response); err != nil {
			glog.Error("Error while decoding Json or casting jsondata to record object", err)
		}
		var wholeMap map[string]interface{}
		if response != nil {
			responseInMap := response.(map[string]interface{})
			if responseInMap != nil {
				if str, iserr := ParseError(responseInMap["error"]); iserr {
					return hostList,errors.New(str)
				}

				//If name of the node not found then number_of_objects will be 0
				result := responseInMap["metadata"]
				if result != nil {
					wholeMap = result.(map[string]interface{})
					if int64(wholeMap["number_of_objects"].(float64)) > 0{
						hostList=append(hostList,node.Name)
					}else{
						glog.Infoln("cluster node is not added on infinibox: ",node.Name)
					}
				}
			}
		}

	}
	return hostList, nil
}

func UnMap(hostId int64, volId int64) (err error) {
	defer func() {
		if res := recover(); res != nil && err == nil {
			err = errors.New("error while unmap volume " + fmt.Sprint(res))
		}
	}()

	urlUnmap := "api/rest/hosts/" + fmt.Sprint(hostId) + "/luns/volume_id/" + fmt.Sprint(volId)
	unmapResponse, err := GetRestClient().R().SetQueryString("approved=true").Delete(urlUnmap)

	_ , err = CheckResponse(unmapResponse, err)
	if err != nil {
		if strings.Contains(err.Error(), "LUN_NOT_FOUND"){ //ignore if lun not found
			return nil
		}
		return err
	}
	return nil
}















































