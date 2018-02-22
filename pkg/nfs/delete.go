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
	"fmt"
	"github.com/golang/glog"
	"github.com/pkg/errors"
	"k8s.io/api/core/v1"
	"strconv"
	"strings"
)

// Delete removes the filesystem  that was created by Provision backing the given
// PV and removes its export.
func (p *nfsProvisioner) Delete(volume *v1.PersistentVolume, config map[string]string, nodeList []*v1.Node) error {
	// Ignore the call if this provisioner was not the one to provision the
	// volume. It doesn't even attempt to delete it, so it's neither a success
	// (nil error) nor failure (any other error)
	provisioned, err := p.provisioned(volume)
	if err != nil {
		return fmt.Errorf("error determining if this provisioner was the one to provision volume %q: %v", volume.Name, err)
	}
	if !provisioned {
		strerr := fmt.Sprintf("this provisioner id %s didn't provision volume %q and so can't delete it; id %s did & can",createdBy, volume.Name, volume.Annotations[annCreatedBy])
		return &controller.IgnoredError{Reason: strerr}
	}

	exportid, ok := volume.Annotations[annExportID]
	if !ok {
		err =  fmt.Errorf("PV doesn't have an annotation with key %s", annExportBlock)
	}
	exportPath, ok := volume.Annotations[annExportBlock]
	if !ok {
		err =  fmt.Errorf("PV doesn't have an annotation with key %s", annExportBlock)
	}
	err = p.deleteExport(exportid,exportPath, config)
	if err != nil {
		return fmt.Errorf("deleted the volume's backing path but error deleting export: %v", err)
	}

	idStr, ok := volume.Annotations[annVolumeID]
	if !ok {
		return  fmt.Errorf("PV doesn't have an annotation %s", annVolumeID)
	}
	id, _ := strconv.ParseInt(idStr, 10, 64)

	err = commons.DetachMetadata(id, volume.Name)

	if err != nil {
		return err
	}

	err = p.deleteDirectory(idStr, config)
	if err != nil {
		return fmt.Errorf("error deleting volume's backing path: %v", err)
	}
	return nil
}

func (p *nfsProvisioner) provisioned(volume *v1.PersistentVolume) (bool, error) {
	provisionerID, ok := volume.Annotations[annCreatedBy]
	if !ok {
		return false, fmt.Errorf("PV doesn't have an annotation %s", annCreatedBy)
	}

	return provisionerID == string(createdBy), nil
}

func (p *nfsProvisioner) deleteDirectory(idStr string, config map[string]string) (err error) {
	defer func() {
		if res := recover(); res != nil && err == nil {
			err = errors.New("error while deleting filesystem " + fmt.Sprint(res))
		}
	}()


	//URL to delete filesystem
	urldeld := "api/rest/filesystems/" + idStr

	//Resty request to delete filesystem
	resp, err := commons.GetRestClient().R().
		SetQueryString("approved=true").
		Delete(urldeld)
	result, err := commons.CheckResponse(resp, err)
	if err != nil {
		return err
	}

	resultmap := result.(map[string]interface{})
	glog.Infoln("Deleted file system: ", resultmap["name"])

	return nil
}

func (p *nfsProvisioner) deleteExport(exportid string,block string, config map[string]string) (err error) {
	defer func() {
		if res := recover(); res != nil && err == nil {
			err = errors.New(" ["+ block +" ]error while deleting export " + fmt.Sprint(res))
		}
	}()

	//URL to delete the export
	urldel := "api/rest/exports/" + fmt.Sprint(exportid)
	resp, err := commons.GetRestClient().R().
		SetQueryString("approved=true").
		Delete(urldel)

	result, err := commons.CheckResponse(resp, err)
	if err != nil {
		if strings.Contains(err.Error(),"EXPORT_NOT_FOUND") 		{ //ignore export not found
			return nil
		}else{
			return errors.New(block + err.Error())
		}
	}
	_ = result
	glog.Infoln("Export Deleted: ", block)

	return nil
}
