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
	"errors"
	"fmt"
	"github.com/golang/glog"
	"k8s.io/api/core/v1"
	"strconv"
	"strings"

)

// Delete removes the volume that was created by Provision
// PV and unmaps from k8s host.
func (p *iscsiProvisioner) Delete(volume *v1.PersistentVolume, config map[string]string, nodeList []*v1.Node) (err error) {
	defer func() {
		if res := recover(); res != nil && err == nil {
			err = errors.New("error while deleting volume " + fmt.Sprint(res))
		}
	}()

	provisioned, err := p.provisioned(volume)
	if err != nil {
		return fmt.Errorf("error determining if this provisioner was the one to provision volume %q: %v", volume.Name, err)
	}
	if !provisioned {
		strerr := fmt.Sprintf("this provisioner id %s didn't provision volume %q and so can't delete it; id %s did & can", createdBy, volume.Name, volume.Annotations[annCreatedBy])
		return &controller.IgnoredError{Reason: strerr}
	}


	glog.Info("volume deletion request received: ", volume.GetName())

	if volume.Annotations["volumeId"] == "" {
		return errors.New("volumeid is empty")
	}
	volId, err := strconv.ParseInt(volume.Annotations["volumeId"],10, 64)
	if err != nil {
		return err
	}


	//removes metatada about volume from infinibox
	err = commons.DetachMetadata(volId,volume.GetName())
	if err != nil {
		return err
	}


	//Unmap volume
	hostList1, err := p.getHostList(nodeList)
	if err != nil {
		glog.Error(err)
	}

	for _, name := range hostList1 {
		hostid, err := getHostId(name)
		if err != nil {
			//it should not return hence printing it.
			glog.Error(hostid)
		}
		err = p.unMap(hostid, volId)
		if err != nil {
				return err
		}
	}

	//Destroy Volume
	err = p.volDestroy(volId, volume.Annotations["volume_name"], nodeList)
	if err != nil {
		glog.Error(err)
		return err

	}
	glog.Info("Volume deleted: ", volume.GetName())
	return nil
}

// deletes volume
func (p *iscsiProvisioner) volDestroy(volId int64, vol string, nodeList []*v1.Node) (err error) {

	defer func() {
		if res := recover(); res != nil && err == nil {
			err = errors.New("error while volume deleting " + fmt.Sprint(res))
		}
	}()


	urldeleteVolume := "api/rest/volumes/" + fmt.Sprint(volId)
	resdel, err := commons.GetRestClient().R().SetQueryString("approved=true").Delete(urldeleteVolume)
	resultdelvolumes, err := commons.CheckResponse(resdel, err)
	if err != nil {
		glog.Errorln(err)
	}

	if resultdelvolumes == nil {
		return errors.New("Result field empty in volDestroy ")
	}

	return nil
}

func (p *iscsiProvisioner)unMap(hostId float64, volId int64) (err error) {
	defer func() {
		if res := recover(); res != nil && err == nil {
			err = errors.New("error while unmap volume " + fmt.Sprint(res))
		}
	}()

	urlUnmap := "api/rest/hosts/" + fmt.Sprint(hostId) + "/luns/volume_id/" + fmt.Sprint(volId)
	unmapResponse, err := commons.GetRestClient().R().SetQueryString("approved=true").Delete(urlUnmap)

	_ , err = commons.CheckResponse(unmapResponse, err)
	if err != nil {
		if strings.Contains(err.Error(), "LUN_NOT_FOUND"){ //ignore if lun not found
			return nil
		}
		return err
	}
	return nil
}


func (p *iscsiProvisioner) provisioned(volume *v1.PersistentVolume) (bool, error) {
	provisionerID, ok := volume.Annotations[annCreatedBy]
	if !ok {
		return false, fmt.Errorf("PV doesn't have an annotation %s", annCreatedBy)
	}

	return provisionerID == string(createdBy), nil
}