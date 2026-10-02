// Commit -> test -> build -> push -> config gate -> deploy -> verify.
// Every stage is a plain shell command you can run by hand to debug it; the
// deploy logic lives in scripts/ci/ rather than in Groovy for the same reason.
pipeline {
  agent any

  options {
    timestamps()
    disableConcurrentBuilds() // two deploys racing on one Deployment is never what you want
    buildDiscarder(logRotator(numToKeepStr: '30'))
    timeout(time: 20, unit: 'MINUTES')
  }

  environment {
    REGISTRY   = 'registry.lab:5000'
    IMAGE_NAME = 'delivery-promise'
    NAMESPACE  = 'logistics'
    // NodePort on worker1. kube-proxy routes it to a Ready pod on any node.
    SMOKE_URL  = 'http://192.168.60.11:30080'
    DOCKER_BUILDKIT = '1'
  }

  stages {
    stage('Resolve version') {
      steps {
        script {
          // Tag images with the commit, never "latest": the tag must say
          // exactly what is running, and rollback must be able to name it.
          env.IMAGE_TAG = sh(script: 'git rev-parse --short=12 HEAD', returnStdout: true).trim()
          env.IMAGE     = "${env.REGISTRY}/${env.IMAGE_NAME}:${env.IMAGE_TAG}"
          currentBuild.description = env.IMAGE_TAG
        }
        echo "Building ${env.IMAGE}"
      }
    }

    stage('Unit tests') {
      steps {
        sh 'docker build --target test app'
      }
    }

    stage('Build image') {
      steps {
        sh '''
          docker build \
            --build-arg VERSION="build-${BUILD_NUMBER}" \
            --build-arg COMMIT="${IMAGE_TAG}" \
            --label org.opencontainers.image.revision="${IMAGE_TAG}" \
            -t "${IMAGE}" app
        '''
      }
    }

    stage('Push to registry') {
      steps {
        sh 'docker push "${IMAGE}"'
      }
    }

    stage('Config gate') {
      steps {
        // Validate the fulfillment-network config with the SAME binary that
        // will load it in production, before any pod sees it. Piped on stdin
        // because a bind mount would resolve against the host, not this container.
        sh 'docker run --rm -i "${IMAGE}" -check-config - < deploy/k8s/app/network.json'
      }
    }

    stage('Deploy') {
      steps {
        withCredentials([file(credentialsId: 'deployer-kubeconfig', variable: 'KUBECONFIG')]) {
          sh 'bash scripts/ci/deploy.sh'
        }
      }
    }

    stage('Smoke test') {
      steps {
        sh 'bash scripts/ci/smoke.sh'
      }
      post {
        failure {
          withCredentials([file(credentialsId: 'deployer-kubeconfig', variable: 'KUBECONFIG')]) {
            sh 'bash scripts/ci/rollback.sh "smoke test failed"'
          }
        }
      }
    }
  }

  post {
    success {
      echo "Deployed ${env.IMAGE} - ${env.SMOKE_URL}/version"
    }
    always {
      // The host daemon keeps every image the pipeline builds; prune old layers
      // so a long-running lab does not fill the CI VM's disk.
      sh 'docker image prune -f --filter "until=72h" >/dev/null || true'
    }
  }
}
