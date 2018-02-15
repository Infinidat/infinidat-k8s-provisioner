# Dockerfile to create image for infinidat k8s provisioner
FROM fedora:24
RUN mkdir -p /export
COPY infinidat-k8s-provisioner ./infinidat-k8s-provisioner
#Adding user to avoid running of container in root mode
RUN useradd -m  infinidat
USER infinidat
ENTRYPOINT ["./infinidat-k8s-provisioner"]

