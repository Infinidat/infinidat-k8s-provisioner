# infinidat-k8s-provisioner

The INFINIDAT Kubernetes Provisioner enables the user to create persistent volumes for their containerized applications. These persistent volumes are dynamically provisioned volumes/filesystems created on the InfiniBox storage array.

The INFINIDAT Kubernetes Provisioner is a downloadable package that can be installed on Kubernetes master node.

Following the installation, it runs as a pod in any of the Kubernetes nodes and carries out provisioning when a Persistent Volume Claim (PVC) is raised. The administrator carries out the installation and creates Storage Classes and the developer can create PVCs.
The provisioner features the following:
* Executes as a Kubernetes deployment and is available as a downloadable package
* Responds to Kubernetes PVC and creates filesystems or volumes as required on InfiniBox
* Supports iSCSI, FC and NFS protocols
* Multipathing is supported for iSCSI and FC protocols
* Provides detailed logging of the operations done by the provisioner

INSTALLATION
Use [installer](https://github.com/Infinidat/infinidat-k8s-installer) to deploy provisioner.
