ifeq ($(REGISTRY),)
        REGISTRY = infinidat/infinidat-k8s-provisioner
endif
ifeq ($(VERSION),)
        VERSION = latest
endif
IMAGE = $(REGISTRY):$(VERSION)
MUTABLE_IMAGE = $(REGISTRY):latest


all build:
	GOOS=linux go install -v .
	GOOS=linux go build .
.PHONY: all build

container: build quick-container
.PHONY: container

quick-container:
	cp infinidat-k8s-provisioner deploy/docker/infinidat-k8s-provisioner
	docker build -t $(MUTABLE_IMAGE) deploy/docker
	docker tag $(MUTABLE_IMAGE) $(IMAGE)
.PHONY: quick-container

push: container
	docker push $(IMAGE)
	docker push $(MUTABLE_IMAGE)
.PHONY: push


save: container
	docker save  $(IMAGE) > infinidat-k8s-provisioner$(VERSION).tar
.PHONY: save

clean:
	rm -f deploy/docker/infinidat-k8s-provisioner
.PHONY: clean
