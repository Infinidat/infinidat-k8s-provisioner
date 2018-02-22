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
package iscsi

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
type iscsiProvisioner struct {
	networkspacesUsed int
	identity         types.UID
	targetdURL        string
	kubeVersion       string
}

// NewISCSIProvisioner creates new iscsi provisioner
func NewISCSIProvisioner(kubeVer string,identity types.UID) controller.Provisioner {
	return &iscsiProvisioner{
		networkspacesUsed: 1,
		identity:         identity,
		kubeVersion:       kubeVer}
}

// getAccessModes returns access modes iscsi volume supported.
func (p *iscsiProvisioner) getAccessModes() []v1.PersistentVolumeAccessMode {
	return []v1.PersistentVolumeAccessMode{
		 v1.ReadWriteOnce,
		}
}

//will map all pv's to nodes in cluster
func (p *iscsiProvisioner) UpdateMapping(pvList []*v1.PersistentVolume, nodeList []*v1.Node,deletedNodes ...*v1.Node) error {
	hostList, err := commons.GetHostList(nodeList) //all nodes of cluster, registered on infinibox as host.
	if err != nil {
		return err
	}


	for _, pv := range pvList {
		if pv.Spec.ISCSI!=nil {
			ann := pv.ObjectMeta.Annotations
			volumeid := ann["volumeId"]
			lun:= ann["lun"]
			lunNumber, _ := strconv.ParseFloat(lun, 64)
			if volumeid != "" && len(volumeid) > 0 {
				volIDInfloat, _ := strconv.ParseFloat(volumeid, 64)
				for _, hostName := range hostList {
					hostID, err := getHostId(hostName)
					if err != nil {
						glog.Error("["+hostName+"] "+err.Error())
					}

					url := "api/rest/hosts/" + fmt.Sprint(hostID) + "/luns"
					restPost, err := commons.GetRestClient().R().SetQueryString("approved=true").SetBody(map[string]interface{}{
						"volume_id": volIDInfloat,"lun":lunNumber}).Post(url)

					_, err = commons.CheckResponse(restPost, err)
					if err != nil {
						if strings.Contains(err.Error(), "MAPPING_ALREADY_EXISTS") {
							//ignore this error
							glog.Infoln("mapping alreaded exist for ",hostName)
						} else {
							glog.Error("error while mapping pv to host " + pv.Name + " to "+hostName+ " : " + err.Error())
						}
					}
				}
			}
		}
	}


	for _, node := range deletedNodes {
		hostID, err := getHostId(node.Name)
		if err != nil {
			glog.Error(err)
		}

		for _, pv := range pvList {
			if pv.Spec.ISCSI!=nil {
				ann := pv.ObjectMeta.Annotations
				volumeid := ann["volumeId"]

				if volumeid != "" && len(volumeid) > 0 {
					volIDInfloat, _ := strconv.ParseFloat(volumeid, 64)
					err :=commons.UnMap(hostID,volIDInfloat)
					if err!=nil{
						glog.Error("Error while unmapping pv from host" + pv.Name + " from "+node.Name+ " : " + err.Error())
					}
				}

			}
		}

	}
	return nil
}

// Provision creates a storage asset and returns a PV object representing it.
func (p *iscsiProvisioner) Provision(options controller.VolumeOptions, config map[string]string, nodeList []*v1.Node) (pvobject *v1.PersistentVolume, err error) {

	defer func() {
		if res := recover(); res != nil && err == nil {
			err = errors.New("error while provision volume " + fmt.Sprint(res))
		}
	}()

	if !util.AccessModesContainedInAll(p.getAccessModes(), options.PVC.Spec.AccessModes) {
		return nil, fmt.Errorf("[%v] invalid AccessModes %v: only AccessModes %v are supported", options.PVName,options.PVC.Spec.AccessModes, p.getAccessModes())
	}
	glog.Info("New provision request received for pvc: ", options.PVName)

	//override default config with storage class config
	for key, value := range options.Parameters {
		config[key] = value
	}

	//Validating NetworkSpace and Pool
	validnwList,err  := commons.OneTimeValidation(config["pool_name"],config["iscsi_networkspaces"])
	if err !=nil{
		return nil, err
	}



	config["iscsi_networkspaces"] = validnwList

	iqn,IPList, err := p.getIPndIQNForNetworkSpace(config["iscsi_networkspaces"])
	if err != nil {
		return nil, err
	}



	vol, lun, volumeID, err := p.createVolume(options, config, nodeList)
	if err != nil {
		glog.Error(err)
		return nil, err
	}
	//url,err:=url.Parse(commons.GetRestClient().HostURL)
	//glog.Infoln("target portal from resty client ",url.Hostname())

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
				ISCSI: &v1.ISCSIPersistentVolumeSource{
					TargetPortal:   IPList[0],
					Portals:        IPList,
					IQN:            iqn,
					ISCSIInterface: "default",
					Lun:            int32(lun),
					ReadOnly:       getReadOnly(config["readonly"]),
					FSType:         config["fsType"],
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

func (p *iscsiProvisioner) createVolume(options controller.VolumeOptions, config map[string]string, nodeList []*v1.Node) (vol string, lun float64, volumeId float64, err error) {
	defer func() {
		if res := recover(); res != nil && err == nil {
			err = errors.New("["+options.PVName+"] Error while creating volume " + fmt.Sprint(res))
		}
	}()

	vol = p.getVolumeName(options)

	pool := config["pool_name"]

	hostList1, err := commons.GetHostList(nodeList)
	if err != nil {
		glog.Error(err)
		return "", 0, 0, err
	}

	volumeId, err = p.volCreate(vol, pool, config, options)
	if err != nil {
		glog.Error(err)
		return "", 0, 0, err
	}
	defer func() {
		if res := recover(); res != nil{
			err = errors.New("["+options.PVName+"] error while mapVolumeToHost volume " + fmt.Sprint(res))
		}
		if err!=nil && volumeId != 0 {
			glog.Infoln("["+options.PVName+"] Seemes to be some problem reverting created volume id: ",volumeId)
			p.volDestroy(int64(volumeId),options.PVName,nodeList)
		}
	}()

	glog.Info("Volume created: ", vol)

	lun , err = mapVolumeToHost(hostList1, volumeId)
	if err != nil {
		return "",0, 0, err
	}

	if lun == -1 {
		return "",0,0,errors.New("["+options.PVName+"] volume not mapped to any host")
	}

	defer func() {
		if res := recover(); res != nil{
			err = errors.New("error while AttachMetadata volume " + fmt.Sprint(res))
		}
		if err!=nil&&volumeId != 0 {
			glog.Infoln("["+options.PVName+"] Seemes to be some problem reverting mapping volume for id: ",volumeId)
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
		return "",0, 0, err
	}
	defer func() {
		if res := recover(); res != nil{
			err = errors.New("["+options.PVName+"] error while creating volume " + fmt.Sprint(res))
		}
		if err!=nil &&volumeId != 0 {
			glog.Infoln("Seemes to be some problem reverting created volume id:",volumeId)
			commons.DetachMetadata(int64(volumeId),options.PVName)
		}
	}()





	return vol, lun, volumeId, nil
}

func (p *iscsiProvisioner) getVolumeName(options controller.VolumeOptions) string {
	return options.PVName
}

// volCreate calls vol_create targetd API to create a volume.
func (p *iscsiProvisioner) volCreate(name string, pool string, config map[string]string, options controller.VolumeOptions) (volid float64, err error) {

	defer func() {
		if res := recover(); res != nil && err == nil {
			err = errors.New("error while volume create " + fmt.Sprint(res))
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
		return 0, errors.New("["+options.PVName+"] Limit exceeded for volume creation " + fmt.Sprint(noOfVolumes))
	}

	poolId, err := commons.GetPoolID(pool)
	if err != nil {
		return 0, err
	}

	capacity := options.PVC.Spec.Resources.Requests[v1.ResourceName(v1.ResourceStorage)]
	requestBytes := capacity.Value()

	urlToCreateVol := "api/rest/volumes"
	resCreate, err := commons.GetRestClient().R().SetBody(map[string]interface{}{
		"pool_id":  poolId,
		"name":     name,
		"provtype": provtype,
		"size":     requestBytes}).Post(urlToCreateVol)

	resultpostcreate, err := commons.CheckResponse(resCreate, err)
	if err != nil {
		glog.Error(err)
	}
	result := resultpostcreate.(map[string]interface{})

	volumeId = result["id"].(float64)

	return  volumeId, nil
}

//Map recently created volume with all host mentioned in export list
func mapVolumeToHost(arrayOfHosts []string, volumeId float64) (lunNo float64, err error) {

	defer func() {
		if res := recover(); res != nil && err == nil {
			err = errors.New("error while mapVolumeToHost " + fmt.Sprint(res))
		}
	}()


	lunNo = -1
	for _, hostName := range arrayOfHosts {
		id, _ := getHostId(hostName)
		newLunNo, err := mapping(id, volumeId,lunNo)
		if err != nil {
			return 0, err
		}
		if newLunNo > lunNo {
			//because we want same lun number to all host for single volume
			lunNo = newLunNo
		}
	}


	return lunNo, nil
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

	if lunNo > -1{//only first time it will be -1, henceforth it will be greater than -1
		body["lun"]=lunNo
	}

	url := "api/rest/hosts/" + fmt.Sprint(hostId) + "/luns"
	restPost, err := commons.GetRestClient().R().SetQueryString("approved=true").SetBody(body).Post(url)


	resultPost, err := commons.CheckResponse(restPost, err)
	if err != nil {
		if strings.Contains(err.Error(), "MAPPING_ALREADY_EXISTS") {
			//ignore
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
			err = errors.New("["+name+"] error while getting host id " + fmt.Sprint(res))
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


func getTargetIQN(networkSpaceName string) (iqn string,err error){
	defer func() {
		if res := recover(); res != nil && err == nil {
			err = errors.New("error while getting target iqn " + fmt.Sprint(res))
		}
	}()


	urlNwID := "api/rest/network/spaces"


	resFC, err := commons.GetRestClient().R().SetQueryString("name=" + networkSpaceName).
		Get(urlNwID)

	var iqnNo string

	resNw , err := commons.CheckResponse(resFC, err)
	if err != nil{
		return iqnNo ,err
	}
	arrayOfresult := resNw.([]interface{})
	for _,networkspace := range arrayOfresult {
		result := networkspace.(map[string]interface{})
		properties:=result["properties"].(map[string]interface{})
		iqnNo = fmt.Sprint(properties["iscsi_iqn"])


	}
	return iqnNo,nil
}


func (p *iscsiProvisioner) getIPndIQNForNetworkSpace(networkSpaces string) (iqnno string ,list []string, err error) {
	defer func() {
		if res := recover(); res != nil && err == nil {
			err = errors.New("error while getting ip list of networkspace" + fmt.Sprint(res))
		}
	}()
	netSpaces := strings.Split(networkSpaces, ",")
	name := strings.Trim(netSpaces[p.networkspacesUsed%len(netSpaces)], " ")
	var ipList []string
	var iqn string
	url := "api/rest/network/spaces"


	if name == "" {
		return "",ipList, errors.New("provided empty networkspace name ")
	}

	iqn , err = getTargetIQN(name)
	if err!=nil{
		return "",ipList,err
	}

	response, err := commons.GetRestClient().R().SetQueryString("name=" + name).Get(url)
	result, err := commons.CheckResponse(response, err)
	if err != nil {
		return "" , ipList, err
	}
	arrayOfResult := result.([]interface{})
	firstIndex := arrayOfResult[0].(map[string]interface{})
	arrayOfIps := firstIndex["ips"].([]interface{})

	for _, ip := range arrayOfIps {
		ipAddresses := ip.(map[string]interface{})
		ipList = append(ipList, fmt.Sprint(ipAddresses["ip_address"]))

	}
	p.networkspacesUsed++
	return iqn,ipList, nil
}