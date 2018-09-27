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
package nfs

import (
	"infinidat-k8s-provisioner/lib/commons"
	"infinidat-k8s-provisioner/lib/controller"
	"encoding/json"
	"errors"
	"fmt"

	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"github.com/golang/glog"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"k8s.io/client-go/kubernetes"
)

const (
	// are we allowed to set this? else make up our own
	annCreatedBy = "kubernetes.io/createdby"
	createdBy    = "infinidat-dynamic-provisioner"

	// A PV annotation for the /etc/exports
	// block, needed for deletion.
	annExportBlock = "EXPORT_block"
	// A PV annotation for the exportID of this PV used for
	// deleting/updating the entry in exportID
	annExportID = "Export_Id"

	// A PV annotation for the project id
	annProjectID = "Project_Id"

	// GidAnnotationKey is the key of the annotation on the PersistentVolume
	// object that specifies a supplemental GID.
	GidAnnotationKey = "pv.beta.kubernetes.io/gid"

	// MountOptionAnnotation is the annotation on a PV object that specifies a
	// comma separated list of mount options
	MountOptionAnnotation = "volume.beta.kubernetes.io/mount-options"

	// A PV annotation for the identity of the Provisioner that provisioned it
	annProvisionerID = "Provisioner_Id"

	annVolumeID = "Volume_Id"
)

// NewNFSProvisioner creates a Provisioner that provisions NFS PVs backed by
// the given directory.
func NewNFSProvisioner(exportDir string, client kubernetes.Interface, kubeVersion string,identity types.UID) controller.Provisioner {

	return newNFSProvisionerInternal(exportDir, client, kubeVersion,identity)
}

func newNFSProvisionerInternal(exportDir string, client kubernetes.Interface, kubeVersion string,identity types.UID) *nfsProvisioner {
	provisioner := &nfsProvisioner{
		exportDir:        exportDir,
		client:           client,
		identity:         identity,
		kubeVersion:      kubeVersion,
		networkspacesIps: make(map[string]int64),
	}

	return provisioner
}

type nfsProvisioner struct {
	// The directory to create PV-backing directories in
	exportDir string
	// Client, needed for getting a service cluster IP to put as the NFS server of
	// provisioned PVs
	client kubernetes.Interface

	// Whether the provisioner is running out of cluster and so cannot rely on
	// the existence of any of the pod, service, namespace, node env variables.
	outOfCluster bool

	kubeVersion string

	// Identity of this nfsProvisioner, generated & persisted to exportDir or
	// recovered from there. Used to mark provisioned PVs
	identity types.UID

	networkspacesIps map[string]int64
}

var _ controller.Provisioner = &nfsProvisioner{}

// Provision creates a volume i.e. the storage asset and returns a PV object for
// the volume.
func (p *nfsProvisioner) Provision(options controller.VolumeOptions, config map[string]string, nodelist []*v1.Node) (*v1.PersistentVolume, error) {

	//validate secret
	if val, found := os.LookupEnv(commons.Apiusername); !(found && len(val) > 0) {

		return nil, errors.New("management api username not specified in secret")
	}
	if val, found := os.LookupEnv(commons.Apiurl); !(found && len(val) > 0) {
		return nil, errors.New("management api url not specified in secret")
	}
	if val, found := os.LookupEnv(commons.Apipassword); !(found && len(val) > 0) {
		return nil, errors.New("management api password not specified in secret")
	}


	volume, err := p.createVolume(options, config, nodelist)
	if err != nil {
		return nil, err
	}


	annotations := make(map[string]string)
	annotations[annCreatedBy] = createdBy
	annotations[annExportBlock] = volume.exportBlock
	annotations[annExportID] = strconv.FormatUint(uint64(volume.exportID), 10)
	annotations[annProjectID] = strconv.FormatUint(uint64(volume.projectID), 10)
	annotations[annVolumeID] = strconv.Itoa(int(volume.dirId))
	if volume.supGroup != 0 {
		annotations[GidAnnotationKey] = strconv.FormatUint(volume.supGroup, 10)
	}
	if volume.mountOptions != "" {
		annotations[MountOptionAnnotation] = volume.mountOptions
	}

	//creating PersistentVolume object
	pv := &v1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name:        options.PVName,
			Labels:      map[string]string{},
			Annotations: annotations,
			Namespace:   options.PVC.Namespace,
		},
		Spec: v1.PersistentVolumeSpec{
			PersistentVolumeReclaimPolicy: options.PersistentVolumeReclaimPolicy,
			AccessModes:                   options.PVC.Spec.AccessModes,
			Capacity: v1.ResourceList{
				v1.ResourceName(v1.ResourceStorage): options.PVC.Spec.Resources.Requests[v1.ResourceName(v1.ResourceStorage)],
			},
			PersistentVolumeSource: v1.PersistentVolumeSource{
				NFS: &v1.NFSVolumeSource{
					Server:   volume.server,
					Path:     volume.path,
					ReadOnly: false,
				},
			},
		},
	}

	return pv, nil

}

type volume struct {
	server       string
	path         string
	exportBlock  string
	exportID     int64
	projectBlock string
	projectID    uint16
	supGroup     uint64
	mountOptions string
	dirId        int64
}

// createVolume creates a volume i.e. the storage asset. It creates a unique
// directory under /export and exports it. Returns the server IP, the path, a
// config or /etc/exports, and the exportID
func (p *nfsProvisioner) createVolume(options controller.VolumeOptions, config map[string]string, nodelist []*v1.Node) (v volume, err error) {
	defer func() {
		if res := recover(); res != nil && err == nil {
			err = errors.New("["+options.PVName+"] error while  creating filesystem  " + fmt.Sprint(res))
		}
	}()
	//override default config with storage class config
	for key, value := range options.Parameters {
		config[key] = value
	}

	validnwlist,err := commons.OneTimeValidation(config["pool_name"] ,config["nfs_networkspace"])
	if err !=nil{
		return v,err
	}
	config["nfs_networkspace"]= validnwlist
	
	v.path = path.Join(p.exportDir, options.PVName)

	v.dirId, err = p.createDirectory(options, config)
	if err != nil {
		err =  errors.New("["+options.PVName+"] error creating filesystem: " + fmt.Sprint(err))
		return v,err
	}
	defer func() {
		if res := recover(); res != nil{
			err = errors.New("error while createExport for directory " + fmt.Sprint(res))
		}
		if err!=nil && v.dirId != 0 {
				glog.Infoln("Seemes to be some problem reverting created directory id:", v.dirId)
				p.deleteDirectory(fmt.Sprint(v.dirId), config)
		}
	}()

	v.exportBlock, v.exportID, err = p.createExport(options.PVName, v.dirId, config, nodelist)
	if err != nil {
		err = errors.New("error creating export for filesystem: " + fmt.Sprint(err))
		return v, err
	}
	defer func() {
		if res := recover(); res != nil{
			err = errors.New("["+options.PVName+"] error while AttachMetadata directory " + fmt.Sprint(res))
		}
		if err!=nil && v.exportID != 0 {
			glog.Infoln("Seemes to be some problem reverting created export id:", v.exportID)
			p.deleteExport(fmt.Sprint(v.exportID),v.exportBlock, config)
		}
	}()

	err = commons.AttachMetadata(int(v.dirId), options, p.kubeVersion,"")
	if err != nil {
		err = errors.New("["+options.PVName+"] error attaching metadata: " + fmt.Sprint(err))
		return v, err
	}
	defer func() {
		if res := recover(); res != nil{
			err = errors.New("["+options.PVName+"] error while create directory " + fmt.Sprint(res))
		}
		if  err!=nil &&v.dirId != 0 {
			glog.Infoln("Seemes to be some problem reverting attached metadata:", v.dirId)
			commons.DetachMetadata(int64(v.dirId),options.PVName)
		}
	}()


	v.server, err = p.getServer(config)
	if err != nil {
		err = errors.New("["+options.PVName+"] error getting NFS server IP for volume: " + fmt.Sprint(err))
		return v, err
	}
	v.mountOptions = config["nfs_mount_options"]
	return
}

func (p *nfsProvisioner) validateOptions(options controller.VolumeOptions) (string, string, error) {
	networkSpace := ""

	mountOptions := ""
	for k, v := range options.Parameters {
		switch strings.ToLower(k) {
		case "networkspace":
			networkSpace = v
		case "nfs_mount_options":
			mountOptions = v
		}
	}
	return networkSpace, mountOptions, nil
}

// getServer gets the server IP to put in a provisioned PV's spec.
//which will be used to acccess filesystem
func (p *nfsProvisioner) getServer(config map[string]string) (ip string, err error) {

	defer func() {
		if res := recover(); res != nil && err == nil {
			err = errors.New("Recovered in getServer" + fmt.Sprint(res))
		}
	}()
	var networkSpace string
	networkSpace = strings.Trim(strings.Split(config["nfs_networkspace"], ",")[0], " ")

	// logic to balance ip's under specified networkspace among filesystems to be mounted
	urlns := "api/rest/network/spaces"
	res, err := commons.GetRestClient().R().SetQueryString("name=" + networkSpace).
		Get(urlns)

	var resultmapip map[string]interface{}
	result, err := commons.CheckResponse(res, err)
	if err != nil {
		glog.Error(err)

	}

	arryofresult := result.([]interface{})
	for _, result := range arryofresult {
		resultmap := result.(map[string]interface{})
		if resultmap["name"] == networkSpace {
			list := resultmap["ips"].([]interface{})

			//Logic for the Ip balancing
			var count int64

			//check for the environment variable if found then increase it by 1 else set variable with value 0
			if _, found := p.networkspacesIps[networkSpace]; found {
				count = p.networkspacesIps[networkSpace]
				p.networkspacesIps[networkSpace] = count + 1
			} else {
				p.networkspacesIps[networkSpace] = 0
			}
			//returning ip in round robin fashion
			countindex := count % int64(len(list))
			resultindex := list[countindex]
			resultmapip = resultindex.(map[string]interface{})
		}
	}

	//Return the ip address
	if resultmapip == nil || resultmapip["ip_address"] == nil {
		err = errors.New("No such network space: " + networkSpace)
		return "", err
	}
	return resultmapip["ip_address"].(string), nil
}

// createDirectory creates the given directory in exportDir with appropriate
func (p *nfsProvisioner) createDirectory(options controller.VolumeOptions, config map[string]string) (id int64, err error) {

	defer func() {
		if res := recover(); res != nil && err == nil {
			err = errors.New("["+options.PVName+"] error while  creating filesystem  " + fmt.Sprint(res))
		}
	}()

	pathstr := options.PVName

	//api url to the inifinibox
	urlGet := "api/rest/filesystems"

	var poolID int64 = -1
	respo, err := commons.GetRestClient().R().
		Get(urlGet)

	if err != nil {
		return 0, err
	}

	var responseget interface{}
	if err := json.Unmarshal(respo.Body(), &responseget); err != nil {
		err = errors.New("error while decoding Json or casting jsondata to record object" + fmt.Sprint(err))
		return 0, err
	}

	var wholemap map[string]interface{}

	if responseget != nil {
		responseinmap := responseget.(map[string]interface{})
		if responseinmap != nil {
			//Check error
			if str, iserr := commons.ParseError(responseinmap["error"]); iserr {
				err = errors.New(str)
				return 0, err
			}
			result := responseinmap["metadata"]
			if result != nil {
				wholemap = result.(map[string]interface{})
			} else {
				err = errors.New(responseinmap["metadata"].(string))
				return 0, err
			}

		} else {
			err =  errors.New("empty response in Get filesystem ")
			return 0, err
		}
	} else {
		err = errors.New("empty response in Get filesystem ")
		return 0, err
	}

	var limit, _ = strconv.ParseInt(config["max_fs"], 10 , 64)

	//Checking the maximum no. of filesystems limit
	if int64(wholemap["number_of_objects"].(float64)) > limit {
		err =  errors.New("reached maximum no. of filesystems, allowed limit is" + fmt.Sprint(limit))
		return 0, err
	}

	var namepool = config["pool_name"]

	poolID, err = commons.GetPoolID(namepool)
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

	rescreate, err := commons.GetRestClient().R().
		SetBody(map[string]interface{}{"atime_mode": "NOATIME",
			"pool_id":     int(poolID),
			"name":        pathstr,
			"ssd_enabled": ssd,
			"provtype":    strings.ToUpper(config["provision_type"]),
			"size":        requestBytes}).
		Post(urlGet)

	var filesystemId int64
	var filesystemName string
	resultPostCreate, err := commons.CheckResponse(rescreate, err)
	if err != nil {
		return 0, err

	}

	if resultPostCreate != nil {
		wholemap := resultPostCreate.(map[string]interface{})
		filesystemId = int64(wholemap["id"].(float64))
		filesystemName = fmt.Sprint(wholemap["name"])
	}
	glog.Infoln("Created file system: ", filesystemName)
	return filesystemId, nil
}

// createExport creates the export by adding a block to the appropriate config
// file and exporting it
func (p *nfsProvisioner) createExport(directory string, FilesystemID int64, config map[string]string, nodelist []*v1.Node) (name string, id int64, err error) {

	defer func() {
		if res := recover(); res != nil && err == nil {
			err = errors.New("error while creating export " + fmt.Sprint(res))
		}
	}()

	var block string
	var exportID int64

	//This will create the export for provided filesystem
	exportpath := path.Join(p.exportDir, directory)

	acess := config["nfs_export_permissions"]

	rootsquash := config["no_root_squash"]

	if rootsquash == "" {
		rootsquash = fmt.Sprint(true)
	}
	rootsq, _ := strconv.ParseBool(rootsquash)

	if len(nodelist) == 0 {
		return "", 0, nil
	}

	var permissionsput []map[string]interface{}

	for _, node := range nodelist {
		for _, naddress := range node.Status.Addresses {
			addr := net.ParseIP(naddress.Address)
			if addr != nil  && naddress.Type=="InternalIP" {
				permissionsput = append(permissionsput, map[string]interface{}{"access": acess, "no_root_squash": rootsq, "client": addr})
			}
		}
	}

	urlPost := "api/rest/exports"

	respex, err := commons.GetRestClient().R().
		SetBody(map[string]interface{}{
			"transport_protocols": "TCP",
			"filesystem_id":       int(FilesystemID),
			"privileged_port":     true,
			"export_path":         exportpath,
			"permissions":         permissionsput}).
		Post(urlPost)

	resultpost, err := commons.CheckResponse(respex, err)
	if err != nil {
		return "", 0, err

	}

	if resultpost != nil {
		wholemap := resultpost.(map[string]interface{})
		block = fmt.Sprint(wholemap["export_path"])
		exportID = int64(wholemap["id"].(float64))
	} else {
		err = errors.New("Empty result after post ")
		return "", 0, err
	}

	glog.Infoln("Export Created: ", block)
	return block, exportID, nil

}

/**
Export should be updated in case of node addition and deletion in k8s cluster
*/
func (p *nfsProvisioner) UpdateMapping(pvList []*v1.PersistentVolume, nodeList []*v1.Node , nodeaddedflag bool , nodeDeletedFlag bool , addednode ...*v1.Node) (err error ){
	defer func() {
		if res := recover(); res != nil && err == nil {
			err = errors.New("error while updating export " + fmt.Sprint(res))
		}
	}()
	
	//Check the list of pv whether empty or not
	if len(pvList) == 0 {
		return nil
	}
	//Check the length of the nodelist for empty or not
	if len(nodeList) == 0 {
		return nil
	}

	//If there is pv then rotate
	for _, pv := range pvList {
		//Check pv provisioned by us or not
		ours, _ := p.provisioned(pv)
		if ours {
			//If yes then get exportId of that PV
			exportid := pv.Annotations[annExportID] 
			if exportid != "" {
				urlGet := "api/rest/exports/" + exportid
				resp, err := commons.GetRestClient().R().
					Get(urlGet)

				resultpost, err := commons.CheckResponse(resp, err)
				if err != nil {
					return err

				}

				var permissionsput []interface{}
				var permissions []interface{}
				if resultpost != nil {
					resultmap := resultpost.(map[string]interface{})
					var acesstype string
					var rootsquash bool
					if resultmap["permissions"] != nil {
						permissions = resultmap["permissions"].([]interface{})
						oldpermisiion := permissions[0].(map[string]interface{})
						acesstype = fmt.Sprint(oldpermisiion["access"])
						rootsquash, _ = strconv.ParseBool(fmt.Sprint(oldpermisiion["no_root_squash"]))
					}
					// if node addition activity happened then find it whether it is present already
					if nodeaddedflag {
						for _, node := range addednode {
							for _, naddress := range node.Status.Addresses {
								addr := net.ParseIP(naddress.Address)
								if addr != nil {
									_, ok := keyExists(permissions, fmt.Sprint(addr))
									if ok {
										glog.Info("node is already added into export permission hence skipping ", addr)
										continue
									}
									permissions = append(permissions, map[string]interface{}{"access": acesstype, "no_root_squash": rootsquash, "client": addr})
									err = callUpdateExportRules(permissions, pv.Name, urlGet)
									if err != nil {
										return err
									}
								}
							}
						}
					}else{
						for _, node := range nodeList {
							for _, naddress := range node.Status.Addresses {
								addr := net.ParseIP(naddress.Address)
								if addr != nil  {
									permissionsput = append(permissionsput, map[string]interface{}{"access": acesstype, "no_root_squash": rootsquash, "client": addr})
								}
							}
					  	}
						err  = callUpdateExportRules(permissionsput,pv.Name,urlGet)
						if err != nil {
							return err
						}
					}

				}

			}
		}
	}
	return nil
}

func callUpdateExportRules(permissions []interface{} , pvName string ,url string) error{
	if permissions != nil {
		respex, err := commons.GetRestClient().R().
			SetBody(map[string]interface{}{
			"permissions": permissions}).
			Put(url)

		_, err = commons.CheckResponse(respex, err)
		if err != nil {
			return err

		}
		glog.Info("Modified export permissions for: ", pvName)
	}
	return nil
}


func keyExists(decoded []interface{}, value string) (key string, ok bool) {
	for i := range decoded {
		_ , ok = keyExistsInMap(decoded[i].(map[string]interface{}),value)
		if  ok {
			return
		}
	}
	return
}

func keyExistsInMap(decoded map[string]interface{}, value string) (key string, ok bool) {
	for k, v := range decoded {
		if v == value {
			key = k
			ok = true
			return
		}
	}
	return
}
