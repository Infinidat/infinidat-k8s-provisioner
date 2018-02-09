package main

import (
	"Infinidat-k8s-provisioner/lib/controller"
	"Infinidat-k8s-provisioner/pkg/iscsi"
	"Infinidat-k8s-provisioner/pkg/nfs"
	"flag"
	"github.com/golang/glog"
	//"k8s.io/apimachinery/pkg/util/validation"
	//"k8s.io/apimachinery/pkg/util/validation/field"
	"Infinidat-k8s-provisioner/pkg/fc"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
)

var (
	master         = flag.String("master", "", "Master URL to build a client config from. Either this or kubeconfig needs to be set if the provisioner is being run out of cluster.")
	kubeconfig     = flag.String("kubeconfig", "", "Absolute path to the kubeconfig file. Either this or master needs to be set if the provisioner is being run out of cluster.")
	serverHostname = flag.String("server-hostname", "", "The hostname for the NFS server to export from. Only applicable when running out-of-cluster i.e. it can only be set if either master or kubeconfig are set. If unset, the first IP output by `hostname -i` is used.")
)

const (
	exportDir = "/export"
)

func main() {
	flag.Set("logtostderr", "true")
	flag.Parse()
	provisionerName := "infinidat.com"

	// Create the client according to whether we are running in or out-of-cluster
	outOfCluster := *master != "" || *kubeconfig != ""

	if !outOfCluster && *serverHostname != "" {
		glog.Fatalf("Invalid flags specified: if server-hostname is set, either master or kube-config must also be set.")
	}
	//glog.Info("outOfCluster ",outOfCluster," serverHostname",serverHostname)

	var config *rest.Config
	var err error
	if outOfCluster {
		config, err = clientcmd.BuildConfigFromFlags(*master, *kubeconfig)
	} else {
		config, err = rest.InClusterConfig()
	}

	if err != nil {
		glog.Fatalf("Failed to create config: %v", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		glog.Fatalf("Failed to create client: %v", err)
	}

	// The controller needs to know what the server version is because out-of-tree
	// provisioners aren't officially supported until 1.5

	serverVersion, err := clientset.Discovery().ServerVersion()
	if err != nil {
		glog.Fatalf("Error getting server version: %v", err)
	}
	var identity types.UID
	identity = uuid.NewUUID()
	// Create the provisioner: it implements the Provisioner interface expected by
	// the controller
	nfsProvisioner := nfs.NewNFSProvisioner(exportDir, clientset, serverVersion.GitVersion,identity)
	iscsiProvisioner := iscsi.NewISCSIProvisioner(serverVersion.GitVersion,identity)
	fcProvisioner := fc.NewFCProvisioner(serverVersion.GitVersion,identity)
	var supportedProvisioners = make(map[string]controller.Provisioner)
	supportedProvisioners[provisionerName] = nfsProvisioner
	supportedProvisioners[provisionerName+"/nfs"] = nfsProvisioner
	supportedProvisioners[provisionerName+"/iscsi"] = iscsiProvisioner
	supportedProvisioners[provisionerName+"/fc"] = fcProvisioner
	// Start the provision controller which will dynamically provision NFS/iscsi/FC PVs
	for k := range supportedProvisioners {
		glog.Infof("Registering  %s Provisioner", k)
	}
	pc := controller.NewProvisionController(
		clientset,
		provisionerName,
		supportedProvisioners,
		serverVersion.GitVersion,
	)
	var neverStop <-chan struct{} = make(chan struct{})
	pc.Run(neverStop)
}

// validateProvisioner tests if provisioner is a valid qualified name.
// https://github.com/kubernetes/kubernetes/blob/release-1.4/pkg/apis/storage/validation/validation.go
/*func validateProvisioner(provisioner string, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if len(provisioner) == 0 {
		allErrs = append(allErrs, field.Required(fldPath, provisioner))
	}
	if len(provisioner) > 0 {
		for _, msg := range validation.IsQualifiedName(strings.ToLower(provisioner)) {
			allErrs = append(allErrs, field.Invalid(fldPath, provisioner, msg))
		}
	}
	return allErrs
}*/
