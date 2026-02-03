variable "REGISTRY" {
  default = "ghcr.io/fiskhest/unifi-port-forward"
}

variable "TAGS" {
  default = ["${REGISTRY}:latest"]
}

group "default" {
  targets = ["controller"]
}

target "controller" {
  context = "."
  dockerfile = "Dockerfile"
  platforms = ["linux/amd64"]
  tags = TAGS
  cache-from = ["type=gha"]
  cache-to = ["type=gha,mode=max"]
}