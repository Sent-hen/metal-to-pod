# Jenkins controller with the tools the pipeline shells out to.
# Builds run on the controller's own executors - acceptable for a single-user
# lab (see docs/adr/0002-jenkins-self-hosted-ci.md for what production changes).
FROM jenkins/jenkins:lts-jdk21

ARG K8S_MINOR=v1.36

USER root

# Docker CLI + buildx only. The daemon is the CI VM's; its socket is mounted in
# (docker-outside-of-docker), so images land in the host's cache and the
# registry trust configured on the host applies.
COPY --from=docker:cli /usr/local/bin/docker /usr/local/bin/docker
COPY --from=docker/buildx-bin /buildx /usr/libexec/docker/cli-plugins/docker-buildx

# kubectl at the newest patch of the cluster's minor version (client/server skew
# must stay within one minor).
RUN set -eux; \
    version="$(curl -fsSL "https://dl.k8s.io/release/stable-${K8S_MINOR#v}.txt")"; \
    curl -fsSLo /usr/local/bin/kubectl "https://dl.k8s.io/release/${version}/bin/linux/amd64/kubectl"; \
    chmod 755 /usr/local/bin/kubectl; \
    kubectl version --client

# The bare repo is bind-mounted from the host and owned by a different user.
RUN git config --system --add safe.directory '*'

USER jenkins

COPY plugins.txt /usr/share/jenkins/ref/plugins.txt
RUN jenkins-plugin-cli --plugin-file /usr/share/jenkins/ref/plugins.txt
