variable "REGISTRY" {
  default = "ghcr.io/fiskhest/unifi-port-forward"
}

group "default" {
  targets = ["controller"]
}

target "controller" {
  context    = "."
  dockerfile = "Dockerfile"
  platforms  = ["linux/amd64"]
  tags       = []
  cache-from = ["type=gha"]
  cache-to   = ["type=gha,mode=max"]
}
