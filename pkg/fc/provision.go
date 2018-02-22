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
package fc
import (
	"infinidat-k8s-provisioner/lib/commons"
	"infinidat-k8s-provisioner/lib/controller"
	"infinidat-k8s-provisioner/lib/util"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/golang/glog"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strconv"
	"strings"
	"k8s.io/apimachinery/pkg/types"
)


const (
	// are we allowed to set this? else make up our own
	annCreatedBy = "kubernetes.io/createdby"
	createdBy    = "infinidat-dynamic-provisioner"
)
type FCProvisioner struct {
	identity     types.UID
	kubeVersion string
}

// NewFCProvisioner creates new FC provisioner
func NewFCProvisioner(kubeVer string,identity types.UID) controller.Provisioner {
	return &FCProvisioner{
		identity:         identity,
		kubeVersion: kubeVer}
}

// getAccessModes returns access modes FC volume supported.
func (p *FCProvisioner) getAccessModes() []v1.PersistentVolumeAccessMode {
	return []v1.PersistentVolumeAccessMode{
		v1.ReadWriteOnce,
	}
}

//will map all pv's to nodes in cluster
func (p *FCProvisioner) UpdateMapping(pvList []*v1.PersistentVolume, nodeList []*v1.Node,deletedNodes ...*v1.Node) error {
	hostList, err := commons.GetHostList(nodeList) //all nodes of cluster, registered on infinibox as host.
	if err != nil {
		return err
	}
	for _, pv := range pvList {
		if pv.Spec.FC!=nil {
			ann := pv.ObjectMeta.Annotations
			volumeid := ann["volumeId"]
			lun:= ann["lun"]
			if volumeid != "" && len(volumeid) > 0 {
				volIDInfloat, _ := strconv.ParseFloat(volumeid, 64)
				lunNumber, _ := strconv.ParseFloat(lun, 64)
				for _, hostName := range hostList {
					hostID, err := getHostId(hostName)
					if err != nil {
						glog.Error(hostName + err.Error() )
					}
					url := "api/rest/hosts/" + fmt.Sprint(hostID) + "/luns"
					restPost, err := commons.GetRestClient().R().SetQueryString("approved=true").SetBody(map[string]interface{}{
						"volume_id": volIDInfloat,"lun":lunNumber}).Post(url)

					_, err = commons.CheckResponse(restPost, err)
					if err != nil {
						if strings.Contains(err.Error(), "MAPPING_ALREADY_EXISTS") {
							//ignore this error
						} else {
							glog.Error("error while mapping pv to hostlist: " + pv.Name + " " + err.Error())
						}
					}
				}
			}
		}
	}
	for _, node := range deletedNodes {

		hostID, err := getHostId(node.Name)
		if err != nil {
			glog.Error(node.Name + err.Error())
		}

		for _, pv := range pvList {
			if pv.Spec.FC!=nil {
				ann := pv.ObjectMeta.Annotations
				volumeid := ann["volumeId"]
				if volumeid != "" && len(volumeid) > 0 {
					volIDInfloat, _ := strconv.ParseFloat(volumeid, 64)
					commons.UnMap(hostID,volIDInfloat)
				}

			}
		}

	}
	return nil
}

// Provision creates a storage asset and returns a PV object representing it.
func (p *FCProvisioner) Provision(options controller.VolumeOptions, config map[string]string, nodeList []*v1.Node) (pvobject *v1.PersistentVolume, err error) {

	defer func() {
		if res := recover(); res != nil && err == nil {
			err = errors.New(" ["+options.PVName+"]  error while provision volume " + fmt.Sprint(res))
		}
	}()

	if !util.AccessModesContainedInAll(p.getAccessModes(), options.PVC.Spec.AccessModes) {
		return nil, fmt.Errorf("invalid AccessModes %v: only AccessModes %v are supported", options.PVC.Spec.AccessModes, p.getAccessModes())
	}
	glog.Info("new provision request received for pvc: ", options.PVName)

	//override default config with storage class config
	for key, value := range options.Parameters {
		config[key] = value
	}

	wwpnlist, err := wwpnList()
	if err != nil {
		return nil, err
	}


	vol, lun, volumeID, err := p.createVolume(options, config, nodeList)
	if err != nil {
		glog.Error(options.PVName +" "+ err.Error() )
		return nil, err
	}
	annotations := make(map[string]string)
	annotations[annCreatedBy] = createdBy
	annotations["volume_name"] = vol
	annotations["volumeId"] = fmt.Sprint(volumeID)
	annotations["lun"]=fmt.Sprint(lun)

	pv := &v1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name:        options.PVName,
			Labels:      map[string]string{},
			Annotations: annotations,
		},
		Spec: v1.PersistentVolumeSpec{
			PersistentVolumeReclaimPolicy: options.PersistentVolumeReclaimPolicy,
			AccessModes:                   options.PVC.Spec.AccessModes,
			Capacity: v1.ResourceList{
				v1.ResourceName(v1.ResourceStorage): options.PVC.Spec.Resources.Requests[v1.ResourceName(v1.ResourceStorage)],
			},
			PersistentVolumeSource: v1.PersistentVolumeSource{
				FC: &v1.FCVolumeSource{
					TargetWWNs: wwpnlist,
					Lun:        &lun,
					ReadOnly:   getReadOnly(config["readonly"]),
					FSType:     config["fsType"],
				},
			},
		},
	}
	return pv, nil
}

func getReadOnly(readonly string) bool {
	isReadOnly, err := strconv.ParseBool(readonly)
	if err != nil {
		return false
	}
	return isReadOnly
}

func (p *FCProvisioner) createVolume(options controller.VolumeOptions, config map[string]string, nodeList []*v1.Node) (vol string, lun int32, volumeId float64, err error) {
	defer func() {
		if res := recover(); res != nil && err == nil {
			err = errors.New("["+options.PVName+"] error while creating volume " + fmt.Sprint(res))
		}
	}()

	vol = p.getVolumeName(options)

	pool := config["pool_name"]

	hostList1, err := commons.GetHostList(nodeList)
	if err != nil {
		glog.Error(err)
		return "", 0, 0, err
	}

	volumeId, err = p.volCreate(vol, pool, config,options)
	if err != nil {
		glog.Error(err)
		return "", 0, 0, errors.New(vol+" "+err.Error())
	}
	glog.Info("Volume created: ", vol)

	defer func() {
		if res := recover(); res != nil{
			err = errors.New("["+options.PVName+"]  error while mapVolumeToHost volume " + fmt.Sprint(res))
		}
		if err!=nil && volumeId != 0 {
			glog.Infoln("Seemes to be some problem reverting created volume id: ",volumeId)
			p.volDestroy(volumeId,options.PVName,nodeList)
		}
	}()


	lun, err = mapVolumeToHost(hostList1, volumeId)
	if err != nil {
		return "", 0, 0, err
	}
	if lun == -1 {
		return "",0,0,errors.New("["+options.PVName+"]  volume not mapped to any host")
	}

	defer func() {
		if res := recover(); res != nil{
			err = errors.New("["+options.PVName+"]  error while AttachMetadata volume " + fmt.Sprint(res))
		}
		if err!=nil&&volumeId != 0 {
			glog.Infoln("Seemes to be some problem reverting mapping volume for id: ",volumeId)
			for _,hostname := range hostList1{
				hostid,err:=getHostId(hostname)
				if err!=nil{
					glog.Error(err)
				}
				commons.UnMap(hostid,volumeId)
			}

		}
	}()


	err = commons.AttachMetadata(int(volumeId), options, p.kubeVersion,config["fsType"])
	if err != nil {
		return "", 0, 0, err
	}

	defer func() {
		if res := recover(); res != nil{
			err = errors.New("["+options.PVName+"]  error while creating volume " + fmt.Sprint(res))
		}
		if err!=nil &&volumeId != 0 {
			glog.Infoln("Seemes to be some problem reverting created volume id:",volumeId)
			commons.DetachMetadata(int64(volumeId),options.PVName)
		}
	}()

	return vol, lun, volumeId, nil
}

func (p *FCProvisioner) getVolumeName(options controller.VolumeOptions) string {
	return options.PVName
}

// volCreate calls vol_create targetd API to create a volume.
func (p *FCProvisioner) volCreate(name string, pool string, config map[string]string, options controller.VolumeOptions) ( volid float64, err error) {

	defer func() {
		if res := recover(); res != nil && err == nil {
			err = errors.New("["+options.PVName+"]  error while volume create " + fmt.Sprint(res))
		}
	}()

	//To Create volume provided by above parameters
	provtype := config["provision_type"]
	var volumeId float64
	noOfVolumes, err := getNumberOfVolumes()
	if err != nil {
		glog.Errorf(fmt.Sprint(err))
	}


	limit, _ := strconv.ParseFloat(config["max_volume"], 64)

	if noOfVolumes >= limit {
		return 0, errors.New("Limit exceeded for volume creation " + fmt.Sprint(noOfVolumes))
	}

	poolId, err := commons.GetPoolID(pool)
	if err != nil {
		return 0, err
	}

	capacity := options.PVC.Spec.Resources.Requests[v1.ResourceName(v1.ResourceStorage)]
	requestBytes := capacity.Value()

	ssdEnabled := config["ssd_enabled"]
	if ssdEnabled == "" {
		ssdEnabled = fmt.Sprint(true)
	}

	ssd, _ := strconv.ParseBool(ssdEnabled)

	urlToCreateVol := "api/rest/volumes"
	resCreate, err := commons.GetRestClient().R().SetBody(map[string]interface{}{
		"pool_id":  poolId,
		"name":     name,
		"provtype": provtype,
		"ssd_enabled": ssd,
		"size":     requestBytes}).Post(urlToCreateVol)

	resultpostcreate, err := commons.CheckResponse(resCreate, err)
	if err != nil {
		glog.Error(name +" "+ err.Error())
	}
	result := resultpostcreate.(map[string]interface{})

	volumeId = result["id"].(float64)



	return  volumeId, nil
}

//Map recently created volume with all host mentioned in export list
func mapVolumeToHost(arrayOfHosts []string, volumeId float64) (lunNo int32, err error) {

	defer func() {
		if res := recover(); res != nil && err == nil {
			err = errors.New("["+ fmt.Sprint(volumeId) +"]  error while mapVolumeToHost " + fmt.Sprint(res))
		}
	}()

	lunNo = -1
	for _, hostName := range arrayOfHosts {
		id, _ := getHostId(hostName)
		lun, err := mapping(id, volumeId,float64(lunNo))
		if err != nil {
			return 0, err
		}
		if int32(lun) > -1 {
			lunNo = int32(lun)
		}
	}


	return int32(lunNo) , nil
}

//Maps the volume to the host passed by calling function
func mapping(hostId float64, volumeId float64,lunNo float64) (lunno float64, err error) {
	defer func() {
		if res := recover(); res != nil && err == nil {
			err = errors.New("error while mapping volume to host " + fmt.Sprint(res))
		}
	}()


	body:=map[string]interface{}{
		"volume_id": volumeId}

	if lunNo > -1{
		body["lun"]=lunNo
	}

	url := "api/rest/hosts/" + fmt.Sprint(hostId) + "/luns"
	restPost, err := commons.GetRestClient().R().SetQueryString("approved=true").SetBody(body).Post(url)

	resultPost, err := commons.CheckResponse(restPost, err)
	if err != nil {
		if strings.Contains(err.Error(), "MAPPING_ALREADY_EXISTS") {
			//ignore this error
		} else {
			return 0, err
		}
	}
	lunofVolume := resultPost.(map[string]interface{})


	return lunofVolume["lun"].(float64), nil
}

func getHostId(name string) (id float64, err error) {
	defer func() {
		if res := recover(); res != nil && err == nil {
			err = errors.New("["+name+" ] error while getting host id " + fmt.Sprint(res))
		}
	}()

	urlGetHostId := "api/rest/hosts"
	resGet, err := commons.GetRestClient().R().SetQueryString("name=" + name).Get(urlGetHostId)
	resultGet, err := commons.CheckResponse(resGet, err)
	if err != nil {
		return 0, err
	}

	arrayOfResult := resultGet.([]interface{})
	resultMap := arrayOfResult[0].(map[string]interface{})

	return resultMap["id"].(float64), nil
}

func getNumberOfVolumes() (no float64, err error) {
	defer func() {
		if res := recover(); res != nil && err == nil {
			err = errors.New("error while getting number of volumes " + fmt.Sprint(res))
		}
	}()

	urlGet := "api/rest/volumes"

	resGet, err := commons.GetRestClient().R().Get(urlGet)
	if err != nil {
		return 0, err
	}

	var response interface{}
	if err := json.Unmarshal(resGet.Body(), &response); err != nil {
		glog.Info("Error while decoding Json or casting jsondata to record object", err)
	}

	var wholeMap map[string]interface{}
	if response != nil {
		responseInMap := response.(map[string]interface{})
		if responseInMap != nil {

			if str, iserr := commons.ParseError(responseInMap["error"]); iserr {
				return 0, errors.New(str)
			}
			result := responseInMap["metadata"]
			if result != nil {
				wholeMap = result.(map[string]interface{})
			} else {
				return 0, errors.New(responseInMap["metadata"].(string))
			}
		} else {
			return 0, errors.New("Empty response in Get NumberofVolumes ")
		}
	} else {
		return 0, errors.New("empty response while getting numberofvolumes ")
	}
	return wholeMap["number_of_objects"].(float64), nil
}


func wwpnList()(list []string,err error){


	defer func() {
		if res := recover(); res != nil && err == nil {
			err = errors.New("error while getting wwpn list " + fmt.Sprint(res))
		}
	}()

	urlFC := "api/rest/components/nodes"
	var wwpno string


	resFC, err := commons.GetRestClient().R().SetQueryString("fields=fc_ports").
		Get(urlFC)

	var wwpnList []string

	resssFc , err := commons.CheckResponse(resFC, err)
	if err != nil {
		return wwpnList,err
	}

	arrayOfresult := resssFc.([]interface{})
	for _,fcPort := range arrayOfresult {
		result := fcPort.(map[string]interface{})
		arrayofFCports := result["fc_ports"].([]interface{})
		for _, fcport := range arrayofFCports{
			fcfields := fcport.(map[string]interface{})
			if fcfields["link_state"] == "UP" {
				wwpno = fmt.Sprint(fcfields["wwpn"])
				wwpno = strings.Replace(wwpno,":","",-1)
				wwpnList = append(wwpnList, fmt.Sprint(wwpno))
			}
		}
	}

	return wwpnList,nil
}