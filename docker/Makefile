# teleport-plugins docker flow makefile
# Based on Teleport's Docker Makefile
# https://github.com/gravitational/teleport/blob/master/docker/Makefile

# location of the test enterprise license file
ENTERPRISE_LICENSE ?= ../e/fixtures/license-enterprise.pem
# location of the test pro license file
PRO_LICENSE ?= ../e/fixtures/license-pro.pem
# whether to look for enterprise or pro license
# MODE ?= enterprise
MODE ?= open

# source directory in the build box
SRCDIR ?= /gopath/src/github.com/gravitational/teleport-plugins

MKFILE_PATH := $(abspath $(lastword $(MAKEFILE_LIST)))

RUNTIME ?= go1.13.2
BBOX = teleport-buildbox:$(RUNTIME)

#
# Default target starts two Teleport clusters
#
.PHONY: up
up: check-license
	docker-compose up

# 'make down' stops all Teleport containers, deletes them
# and their network
#
.PHONY:down
down:
	docker-compose down

# `make enter-teleport` gives you shell inside auth server
# of cluster "one"
#
.PHONY:enter-teleport
enter-teleport:
	docker exec -ti teleport /bin/bash

# `make enter-sshd` gives you shell inside sshd container
.PHONY:enter-teleport-sshd
enter-sshd:
	docker exec -ti one-sshd /bin/bash

.PHONY: export-certs
export-certs:
	docker exec -i teleport /bin/bash -c "tctl auth export --type=user | sed s/cert-authority\ // > /mnt/shared/certs/teleport.pub"

.PHONY: setup-tc
setup-tc:
	docker exec -i teleport /bin/bash -c "tctl -c /root/go/src/github.com/gravitational/teleport/docker/teleport/config.yaml create -f /root/go/src/github.com/gravitational/teleport/docker/teleport/cluster.yaml"

.PHONY: delete-tc
delete-tc:
	docker exec -i two-auth /bin/bash -c "tctl -c /root/go/src/github.com/gravitational/teleport/docker/teleport/config.yaml rm tc/one"

.PHONY: check-license
check-license:
	@if [ $(MODE) = "enterprise" ] && [ -f $(ENTERPRISE_LICENSE) ]; then \
		mkdir -p ./data/one && cp $(ENTERPRISE_LICENSE) ./data/one/license.pem; \
		mkdir -p ./data/two/auth && cp $(ENTERPRISE_LICENSE) ./data/two/auth/license.pem; \
	fi
	@if [ $(MODE) = "pro" ] && [ -f $(PRO_LICENSE) ]; then \
		mkdir -p ./data/one && cp $(PRO_LICENSE) ./data/one/license.pem; \
		mkdir -p ./data/two/auth && cp $(PRO_LICENSE) ./data/two/auth/license.pem; \
	fi

# builds teleport:latest docker image
# Using the ../../teleport's docker instructions
# Note: This doesn't build Teleport itself. If you need to build teleport,
# go to teleport repo root, and run make -c build.assets
#
.PHONY: build-teleport-image
build-teleport-image:
	docker build -t teleport:latest ../../teleport/docker/


# Builds teleport-slack:latest docker image
#
.PHONY: build-slack-image
build-slack-image:
	docker build -t teleport-slack:latest . -f ./slack/Dockerfile

# Builds all of the extensions inside the build box docker container.
#
.PHONY: build-plugins
build-plugins:
	docker run -v $(MKFILE_PATH)/../../:/gopath/src/github.com/gravitational/teleport-plugins $(BBOX) \
		make -C $(SRCDIR) build-all

.PHONY: clean
clean:
	rm -rf data