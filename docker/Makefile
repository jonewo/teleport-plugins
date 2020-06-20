# teleport-plugins docker flow makefile
# Based on Teleport's Docker Makefile
# https://github.com/gravitational/teleport/blob/master/docker/Makefile

# location of the test enterprise license file
ENTERPRISE_LICENSE ?= ../e/fixtures/license-enterprise.pem
# location of the test pro license file
PRO_LICENSE ?= ../e/fixtures/license-pro.pem
# whether to look for enterprise or pro license
# MODE ?= enterprise
# `open` mode skips the license check. 
MODE ?= open

# source directory in the build box
SRCDIR ?= /gopath/src/github.com/gravitational/teleport-plugins
MKFILE_PATH := $(abspath $(lastword $(MAKEFILE_LIST)))

# This is the Teleport version that will be used for the Teleport image.
# It can be any _released_ version, as the image will try to install the
# enterprise version of Teleport.
VERSION=4.2.10
GITTAG=v$(VERSION)
OS ?= $(shell go env GOOS)
ARCH ?= $(shell go env GOARCH)
RELEASE=teleport-$(GITTAG)-$(OS)-$(ARCH)-bin

RUNTIME ?= go1.13.2
BBOX = teleport-buildbox:$(RUNTIME)

#
# Default target starts a teleport cluster with a single node,
# provisions all the required configurations for users and roles, 
# and then starts all the plugins.
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
	docker-compose exec teleport /bin/bash

# `make enter-sshd` gives you shell inside sshd container
.PHONY:enter-teleport-sshd
enter-sshd:
	docker-compose exec one-sshd /bin/bash

.PHONY: export-certs
export-certs:
	docker-compose exec teleport /bin/bash -c "tctl auth export --type=user | sed s/cert-authority\ // > /mnt/shared/certs/teleport.pub"

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
	docker build -t teleport-ent:latest -f ./teleport/Dockerfile .

# `make configure` provisions configs for the `teleport cluster: 
# 1. Creates the role and user for tests
# 2. Creates the role and user for plugins to run with 
# 3. Exports the access-plugin certs so that we can later use them for plugins. 
#
.PHONY: config
config:
	docker-compose start teleport
	
	# ----> Creating dummy roles for requests and plugins to use.
	docker-compose exec teleport /bin/bash -c "tctl create -f \$$PLUGINSHOME/docker/teleport/foo-role.yaml"
	docker-compose exec teleport /bin/bash -c "tctl create -f \$$PLUGINSHOME/docker/teleport/access-plugin-user-role.yaml"
	
	# ----> Exporting certificates.
	docker-compose exec teleport /bin/bash -c "tctl auth sign --format=tls --user=access-plugin --out=plug"	
	docker-compose exec teleport /bin/bash -c "mkdir -p /mnt/shared/certs/access-plugin && mv plug.* /mnt/shared/certs/access-plugin/"

	# ----> Stoppig Teleport's VM.
	docker-compose stop teleport

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
