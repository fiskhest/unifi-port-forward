variable "REGISTRY" {
  default = "johrad/unifi-port-forward"
}

group "default" {
  targets = ["controller"]
}

target "controller" {
  context = "."
  dockerfile = "Dockerfile"
  platforms = ["linux/amd64"]
  tags = ["${REGISTRY}:latest"]
  cache-from = ["type=gha"]
  cache-to = ["type=gha,mode=max"]
}