## Docker

This directory contains Docker-based flow to run Teleport Plugins locally, and is indended for manual QA with the [Test Plan](../testplan.md) / testing purposuses.

### Setup

This flow builds on top of [Teleport's own Docker flow](https://github.com/gravitational/teleport/tree/master/docker).Teleport's own Docker image and services are managed with that flow.

#### Getting started with Teleport's Docker flow

First prepare your teleport directory to work with the Docker flow: 

```bash
git clone git@gitnub.com:gravitational/teleport.git
cd teleport
make docker
```

After that, we'll build the teleport's image from teleport-plugin's directory.
We'll still use teleport's own Dockerfile, but the services in `docker-compose.yml` are different,
and teleport-plugins test flow uses it's own `data` directory, just so if you've been testing both
teleport and teleport-plugins, they shouldn't interfere with each other.

Please refer to Teleport's Docker documentation for more details about it's configuration.

### Building

```bash
make build-teleport # Builds Teleport image (teleport:latest) based on Teleport's docker/Dockerfile and build assets buildbox:latest image.
```

### Starting

```bash
make up # Starts a single node Teleport cluster and a Teleport Slack plugin alongside it.
```

### Stopping

```bash
make down
```

### Confugurationå

### TODO

Todo items for this document:
1. [x] Read the test plan thorougly
2. [x] Describe how to launch test scenarios (which teleport instances and plugin instances will we need?)
3. [x] Start implementing the instances in `docker-compose` and `Makefile`.
4. [ ] Implement Slack Plugin Dockerfile + service, write about how to set up it's config
5. [ ] Ben reviews if the Slack part is valid