# Image to publish, without a tag.
#
# Override with the REGISTRY environment variable so a fork builds into its own
# namespace rather than upstream's - `REGISTRY=ghcr.io/you/unifi-port-forward
# just build`, or set it once in a local .env. CI sets it from the repository
# the workflow is running in, so it needs no per-fork edit either.
variable "REGISTRY" {
  default = "ghcr.io/fiskhest/unifi-port-forward"
}

# Tag applied alongside :latest. CI sets a YYYY-MM-DD-sha tag.
variable "IMAGE_TAG" {
  default = "dev"
}

group "default" {
  targets = ["controller"]
}

target "controller" {
  context    = "."
  dockerfile = "Dockerfile"
  platforms  = ["linux/amd64"]
  tags       = ["${REGISTRY}:latest", "${REGISTRY}:${IMAGE_TAG}"]

  # No cache config here on purpose: type=gha only works inside GitHub Actions
  # and fails a local build. The workflow adds it with `set:`.
}
