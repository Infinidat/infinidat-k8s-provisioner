# infinidat-k8s-provisioner

The INFINIDAT Kubernetes Provisioner enables the user to create persistent volumes for their containerized applications. These persistent volumes are dynamically provisioned volumes/filesystem created on InfiniBox storage array.

you can use [installer](https://github.com/Infinidat/infinidat-k8s-installer) to deploy provisioner. After it is installed, it runs as a pod in any of the Kubernetes node and carries out provisioning when a Persistent Volume Claim (PVC) is raised. The administrator carries out the installation and creates Storage Class and the developer can create PVCs. 
The provisioner supports the following features:

•	Executes as a Kubernetes deployment and is available as a downloadable package 

•	Responds to Kubernetes PVC and creates file systems or volumes as required on InfiniBox

•	Supports iSCSI, FC and NFS protocols

•	Multipathing supported for iSCSI and FC protocols

•	Provides detailed logging of the operations done by the provisioner
