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
	"errors"
	"fmt"
	"github.com/golang/glog"
	"k8s.io/api/core/v1"
	"strconv"
)

// Delete removes the volume that was created by Provision
// PV and unmaps from k8s host.
func (p *FCProvisioner) Delete(volume *v1.PersistentVolume, config map[string]string, nodeList []*v1.Node) (err error) {
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
		err = errors.New("volumeid is empty")
		return err
	}
	volId, err := strconv.ParseFloat(volume.Annotations["volumeId"], 64)
	if err != nil {
		return err
	}
	err = p.volDestroy(volId, volume.Annotations["volume_name"], nodeList)
	if err != nil {
		glog.Error(err)
		return err

	}
	glog.Info("Volume deleted: ", volume.GetName())
	return nil
}

// deletes volume
func (p *FCProvisioner) volDestroy(volId float64, vol string, nodeList []*v1.Node) (err error) {

	defer func() {
		if res := recover(); res != nil && err == nil {
			err = errors.New("error while volume deleting " + fmt.Sprint(res))
		}
	}()
	//removes metatada about volume from infinibox
	err = commons.DetachMetadata(int64(volId), vol)
	if err != nil {
		return err
	}

	hostList1, err := commons.GetHostList(nodeList)
	if err != nil {
		glog.Error(err)
	}

	for _, name := range hostList1 {
		hostid, err := getHostId(name)
		if err != nil {
			//it should not return hence printing it.
			glog.Error(hostid)
		}
		err = commons.UnMap(hostid, volId)
		if err != nil {
			return err
		}
	}

	urldeleteVolume := "api/rest/volumes/" + fmt.Sprint(volId)
	resdel, err := commons.GetRestClient().R().SetQueryString("approved=true").Delete(urldeleteVolume)
	resultdelvolumes, err := commons.CheckResponse(resdel, err)
	if err != nil {
		glog.Errorln(err)
	}

	if resultdelvolumes == nil {
		err = errors.New("Result field empty in volDestroy ")
		return err
	}

	return nil
}


func (p *FCProvisioner) provisioned(volume *v1.PersistentVolume) (bool, error) {
	provisionerID, ok := volume.Annotations[annCreatedBy]
	if !ok {
		return false, fmt.Errorf("PV doesn't have an annotation %s", annCreatedBy)
	}

	return provisionerID == string(createdBy), nil
}