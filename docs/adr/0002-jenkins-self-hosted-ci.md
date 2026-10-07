# 0002: Jenkins, configured as code, as the self-hosted CI

**Status:** Accepted

## Context
The README requires a self-hosted pipeline (Jenkins or GitLab CI). A GitLab CE omnibus install needs about 4-8 GB of RAM before running a single job; Jenkins runs comfortably in about 1 GB. Jenkins configured through the UI is also the classic example of an unreproducible "snowflake" server.

## Decision
- Jenkins LTS in Docker on the `ci` VM, with plugins listed in `ci/jenkins/plugins.txt`.
- **All** configuration comes from Configuration as Code (`casc.yaml`): the admin user, the credential, and the pipeline job (Job DSL). The setup wizard is disabled.
- Pipeline stages live in the repo `Jenkinsfile`; non-trivial logic lives in `scripts/ci/*.sh`, which can be run and debugged without Jenkins.
- Builds run on the controller's executors and use the host Docker daemon through its socket.

## Consequences
- The controller is disposable: `docker volume rm ci_jenkins-home` plus a restart rebuilds it the same way.
- The pipeline is portable: the shell scripts would run unchanged in GitLab CI or GitHub Actions.
- **Security trade-off:** mounting `docker.sock` gives any build root-equivalent access to the ci VM, and running builds on the controller lets them read `JENKINS_HOME`. That is acceptable for a single-user lab and not for shared CI. Production fixes: ephemeral agents (the Kubernetes plugin or VM agents), and rootless image builds (BuildKit rootless, Kaniko, or Buildah).
- Plugins are unpinned and resolved at image build time against the current LTS. For reproducible rebuilds, pin versions in `plugins.txt`.
