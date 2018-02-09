ifeq ($(REGISTRY),)
        REGISTRY = quay.io/nileshdsalunkhe/nfsprovi
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
	cp Infinidat-k8s-provisioner deploy/docker/Infinidat-k8s-provisioner
#       cp main deploy/docker/main
	docker build -t $(MUTABLE_IMAGE) deploy/docker
	docker tag $(MUTABLE_IMAGE) $(IMAGE)
.PHONY: quick-container

push: container
	docker login quay.io --username=nileshdsalunkhe --password=20101992
	docker push $(IMAGE) 
	docker push $(MUTABLE_IMAGE)
.PHONY: push


save: container
	docker save  $(IMAGE) > Infinidat-k8s-provisioner$(VERSION).tar 
.PHONY: save

clean:
	rm -f deploy/docker/Infinidat-k8s-provisioner
.PHONY: clean