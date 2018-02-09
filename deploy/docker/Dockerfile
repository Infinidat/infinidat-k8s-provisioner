# Dockerfile to create image for infinidat k8s provisioner
FROM fedora:24
RUN mkdir -p /export
COPY Infinidat-k8s-provisioner ./Infinidat-k8s-provisioner
#Adding user to avoid running of container in root mode
RUN useradd -m  infinidat
USER infinidat
ENTRYPOINT ["./Infinidat-k8s-provisioner"]

